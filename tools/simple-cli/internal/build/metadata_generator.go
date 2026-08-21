package build

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"simple-cli/internal/fsx"
	"simple-cli/internal/home"
)

//go:embed scripts/extract-action-metadata.js
var generatorScript string

//go:embed scripts/extract_godoc.go
var goExtractorSource string

// The Rust companion, carried as the crate it is rather than as one file.
//
// Named file by file rather than as a directory, because a directory pattern
// embeds whatever is in it: the crate builds into a `target/` beside its
// manifest, and a copy that had ever been built in place would carry a few
// hundred megabytes of object files into this binary. The three patterns below
// are the crate's whole source, and `src` is a directory so a file added to it
// upstream arrives with the sync rather than being left behind.
//
//go:embed scripts/extract_rustdoc/Cargo.toml scripts/extract_rustdoc/Cargo.lock scripts/extract_rustdoc/src
var rustCompanionCrate embed.FS

const (
	// generatorScriptName and the two names below are the names the generator
	// knows. It looks for both companions BESIDE ITSELF, by name, so these are
	// not this package's choice to make: they are the platform's file names,
	// carried here with the files.
	generatorScriptName = "extract-action-metadata.js"
	goExtractorName     = "extract_godoc.go"
	rustCompanionName   = "extract_rustdoc"

	// Where the embedded crate lives inside this binary.
	rustCompanionEmbedDir = "scripts/" + rustCompanionName
)

// describeActionFromSource describes an action from its own source by running
// the generator the platform runs, rather than having this package reimplement
// what it does.
//
// ONE GENERATOR, EVERY LANGUAGE. It reads the action's source itself and
// dispatches on what it finds there: TypeScript through ts-morph and
// ts-json-schema-generator, Go through the extractor it builds from the file
// carried beside it, Rust through the crate carried the same way. Which is why
// all three are written out together — the generator looks for its companions
// NEXT TO ITSELF, and this tool used to leave them in different directories, so
// the Go half of the one generator could not be found by the half that runs it.
//
// THE GENERATOR'S OUTPUT IS THE FILE. It writes action.json itself and what it
// wrote is left exactly as it wrote it — read back only far enough to prove it
// is there and parses.
//
// It used to be re-rendered through this package's own structs on the way past.
// Those structs model a SUBSET of JSON Schema, so a shape they cannot hold was
// silently flattened, dropped, or refused: a member typed as a union stopped
// the CLI dead on a source the platform's own build accepts, and everything
// that survived came back out with its keys reordered. Either way the file this
// tool left behind was not the file the other tool writes from the same source,
// which is the one thing a generated contract may not do.
//
// Returns error if:
//   - Node.js is not available
//   - Required npm packages are not installed
//   - A Rust action is described with no cargo to build the companion with
//   - Extraction script is not found
//   - Script execution fails, including refusing a malformed exposure statement
//   - No readable action.json was produced
func describeActionFromSource(fs fsx.FileSystem, actionDir string, lang ActionLanguage) error {
	// Check if Node.js is available
	if err := checkNodeJS(); err != nil {
		return fmt.Errorf("node.js is required for action metadata extraction: %w", err)
	}

	// Ensure required npm packages are installed
	if err := ensureNPMPackages(); err != nil {
		return fmt.Errorf("failed to install required npm packages: %w", err)
	}

	// THE TOOLCHAIN IS ASKED FOR HERE RATHER THAN INSIDE NODE.
	//
	// The generator builds the companion itself and says so plainly when it
	// cannot, but what it can say is what a failed `cargo` looked like from a
	// child process. A machine with no Rust on it is not a broken generator,
	// and it is told the same sentence here that compiling the action would
	// tell it — one fix, named once, wherever a developer meets it first.
	if lang == LanguageRust {
		if _, err := EnsureCargoFunc(); err != nil {
			return err
		}
	}

	// Execute the generator
	if err := executeScript(actionDir, lang); err != nil {
		// A refusal is handed back as the generator wrote it. Wrapping it would
		// put this layer's account of how a child process ended in front of the
		// one sentence the author has to read to fix their source.
		var refusal *AnnotationRefusal
		if errors.As(err, &refusal) {
			return err
		}

		return fmt.Errorf("failed to execute extraction script: %w", err)
	}

	data, err := fs.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		return fmt.Errorf("failed to read generated action.json: %w", err)
	}

	if !json.Valid(data) {
		return fmt.Errorf("the generator produced an action.json that is not valid JSON")
	}

	return nil
}

// checkNodeJS verifies that Node.js is available on the system
func checkNodeJS() error {
	cmd := exec.Command("node", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("node.js is required for action metadata extraction: %w", err)
	}

	// Check Node.js version (should be >= 18 for ESM support)
	version := strings.TrimSpace(string(output))
	if !strings.HasPrefix(version, "v") {
		return fmt.Errorf("unexpected node version format: %s", version)
	}

	return nil
}

// generatorPackages are the npm packages the generator imports. They are named
// once because the same list decides both what is looked for and what is
// installed, and a list that says one thing in the check and another in the
// install would install something the check keeps asking for.
var generatorPackages = []string{
	"ts-json-schema-generator",
	"ts-morph",
}

// ensureNPMPackages makes sure the generator's packages can be found before the
// generator is asked to run, and installs them when they cannot.
//
// IT ASKS THE QUESTION NODE ASKS. Node finds a package by looking in a
// `node_modules` beside the importing file, then beside each directory above it,
// so one copy at the top of a repository serves every directory beneath it.
// Asking instead whether one particular directory has its own `node_modules` is
// a narrower question, and answering "missing" to it while Node answers "found"
// buys an install the run cannot use. That is what happened for as long as the
// workspace root came back as a directory partway up the tree: the packages were
// there the whole time, one level higher, and every run paid to install them
// again.
//
// The walk starts at the workspace root because that is where the generator is
// written and run from. The directory it actually sits in is created empty under
// that root, so it has no `node_modules` of its own to contribute.
func ensureNPMPackages() error {
	workspaceRoot, err := findWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("failed to find workspace root: %w", err)
	}

	if generatorPackagesResolvable(workspaceRoot) {
		return nil
	}

	// Output is captured rather than inherited: builds run under a progress UI
	// that repaints in place, and a concurrent write from a child process
	// corrupts the frame. The output is surfaced only if the install fails.
	cmd := exec.Command("pnpm", append([]string{"add", "-w", "-D"}, generatorPackages...)...)
	cmd.Dir = workspaceRoot

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install packages: %w\nOutput: %s", err, out)
	}

	return nil
}

// generatorPackagesResolvable reports whether every package the generator
// imports can be found from a directory, and so whether a run starting there
// would have anything to install.
func generatorPackagesResolvable(dir string) bool {
	for _, pkg := range generatorPackages {
		if !nodeCanResolve(dir, pkg) {
			return false
		}
	}

	return true
}

// nodeCanResolve reports whether a Node module living in dir would find the
// named package when it imports it, by taking the same walk Node takes.
//
// The walk itself lives in fsx because two commands need it and for different
// things — this one asks for a package, the test runner also asks for an
// executable a package installed. Written twice they answer the same question
// in two places, and the pair drift.
func nodeCanResolve(dir, pkg string) bool {
	_, found := fsx.ResolveUpward(fsx.OSFileSystem{}, dir, "node_modules", pkg)

	return found
}

// findWorkspaceRoot answers with the root of the workspace whose `node_modules`
// the generator runs against, by walking up from the current directory.
//
// A WORKSPACE ROOT IS NOT SIMPLY THE FIRST package.json ABOVE US. Every member
// of a workspace carries a package.json of its own, so a walk that stops at the
// first one it meets stops inside a member and calls that the root. This tool's
// own tests are where that showed: they run from a directory inside the CLI's
// package, whose package.json is a member manifest, so the root came back as the
// CLI's package — a directory with no `node_modules` in it, in a repository that
// keeps its packages at the top. Nothing broke, because Node walks past it and
// finds them anyway, but the check that stopped where this walk stopped
// concluded on every run that they were missing.
//
// So a file that MARKS a workspace root beats a manifest that merely SITS in
// one: pnpm-workspace.yaml marks a pnpm workspace, and a package.json carrying a
// `workspaces` field marks an npm or yarn one. A plain manifest is remembered as
// the answer for a project that belongs to no workspace at all, but the walk
// carries on past it in case a workspace encloses it.
func findWorkspaceRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// The nearest plain manifest, which is the answer only if the walk reaches
	// the top without meeting a workspace.
	standalone := ""

	for {
		if _, err := os.Stat(filepath.Join(dir, "pnpm-workspace.yaml")); err == nil {
			return dir, nil
		}

		packageJSON := filepath.Join(dir, "package.json")
		if _, err := os.Stat(packageJSON); err == nil {
			if declaresWorkspaces(packageJSON) {
				return dir, nil
			}

			if standalone == "" {
				standalone = dir
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	if standalone != "" {
		return standalone, nil
	}

	return "", fmt.Errorf("workspace root not found (no pnpm-workspace.yaml or package.json)")
}

// declaresWorkspaces reports whether a package.json names workspace members,
// which is how npm and yarn mark the root of a workspace.
//
// Only that one field is read, because the rest of a manifest is somebody else's
// business, and it is accepted in either shape it is written in: the array of
// patterns npm documents, or the object holding one that yarn also accepts.
// A manifest that cannot be read or parsed is simply not a workspace root as far
// as this walk is concerned — the walk carries on, and whatever is really wrong
// with the file will be said by the tool that has to use it.
func declaresWorkspaces(packageJSON string) bool {
	data, err := os.ReadFile(packageJSON)
	if err != nil {
		return false
	}

	var manifest struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return false
	}

	return len(manifest.Workspaces) > 0 && !bytes.Equal(manifest.Workspaces, []byte("null"))
}

// unpackGenerator writes the generator into a directory of this invocation's
// own, under the workspace root, and hands back the script to run and the way
// to discard them.
//
// UNDER THE WORKSPACE ROOT because ESM resolves `node_modules` from the
// IMPORTING FILE's directory upwards, so a generator run from anywhere else
// cannot find ts-morph. EVERY HALF because the generator looks for its
// companions beside itself, and writing them to different directories left a Go
// action describable by neither tool.
//
// OF THIS INVOCATION'S OWN because one fixed path was shared by every concurrent
// extraction in a build. Each wrote the same bytes there, so nothing was
// corrupted — but each also deleted the file when it finished, and a build runs
// these in parallel. One action's cleanup removed the script another action's
// node process had not finished starting up with, and that action failed to
// build with `Cannot find module` naming a path nothing in the source mentions.
// Measured at 1 failure in 48 actions at 16-way concurrency, which is a build
// that fails on which actions happened to finish first.
//
// THE RUST COMPANION IS LINKED IN RATHER THAN COPIED, and only when the action
// needs it. It is a crate, so the generator has to compile it before it can ask
// it anything, and cargo keeps what it compiled in a directory beside the
// manifest. Copied here, that directory would be thrown away with the rest of
// this invocation and the next action in the same build would compile the
// companion again — measured at 4.0s per action against 0.03s for a link into
// the one crate directory this tool keeps. Cargo resolves both the manifest and
// the build directory to where the link points, so every action reaches the
// same warm cache no matter which invocation's link it arrives through.
func unpackGenerator(workspaceRoot string, lang ActionLanguage) (string, func(), error) {
	dir, err := os.MkdirTemp(workspaceRoot, ".simple-extract-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create a directory for the generator: %w", err)
	}

	discard := func() { _ = os.RemoveAll(dir) }

	scriptPath := filepath.Join(dir, generatorScriptName)
	if err := os.WriteFile(scriptPath, []byte(generatorScript), fsx.FilePerm); err != nil {
		discard()
		return "", nil, fmt.Errorf("failed to write the generator: %w", err)
	}

	goDocPath := filepath.Join(dir, goExtractorName)
	if err := os.WriteFile(goDocPath, []byte(goExtractorSource), fsx.FilePerm); err != nil {
		discard()
		return "", nil, fmt.Errorf("failed to write the Go extractor: %w", err)
	}

	if lang == LanguageRust {
		crateDir, err := rustCompanionCrateDir()
		if err != nil {
			discard()
			return "", nil, err
		}

		if err := os.Symlink(crateDir, filepath.Join(dir, rustCompanionName)); err != nil {
			discard()
			return "", nil, fmt.Errorf("failed to put the Rust extractor beside the generator: %w", err)
		}
	}

	return scriptPath, discard, nil
}

// The crate directory, written out at most once per run of this CLI.
//
// A build describes its actions in parallel, so without this every Rust action
// would race the others to write the same files. They would be the same bytes,
// but the run that costs nothing is the one that does not write at all.
var rustCompanion struct {
	sync.Mutex
	dir  string
	err  error
	done bool
}

// rustCompanionCrateDir answers with the directory holding the Rust companion's
// crate, writing it out if what is there is not what this binary carries.
//
// IT IS KEPT OUTSIDE THE WORKSPACE, beside the other build tooling this CLI
// installs, because the generator compiles it and cargo writes what it compiled
// into a `target/` beside the manifest. Inside the workspace that directory
// would be a few hundred megabytes of build output appearing untracked in a
// developer's own repository, in a folder they did not create.
//
// UNCHANGED FILES ARE LEFT ALONE rather than rewritten, and the answer is read
// from the files themselves rather than from a version stamped beside them: a
// stamp is a claim about the files, and the files are right there. Cargo decides
// what to recompile from what it finds on disk, so rewriting identical bytes
// would throw away the build the last run paid for.
//
// A file that does have to change is renamed into place rather than written
// through, so a cargo already reading it sees either the old file or the new one
// and never half of each.
//
// A file the crate no longer has is left where it is rather than swept up. What
// gets compiled is what the crate's own `main.rs` declares, so a module that has
// left the crate is inert the moment the declaration naming it does — and a
// sweep would have to be told which of the directory's contents are the crate
// and which are the build cargo keeps beside it, which is a rule about a
// directory this package does not own.
func rustCompanionCrateDir() (string, error) {
	rustCompanion.Lock()
	defer rustCompanion.Unlock()

	if rustCompanion.done {
		return rustCompanion.dir, rustCompanion.err
	}

	rustCompanion.done = true
	rustCompanion.dir, rustCompanion.err = writeRustCompanionCrate()

	return rustCompanion.dir, rustCompanion.err
}

func writeRustCompanionCrate() (string, error) {
	home, err := home.Dir()
	if err != nil {
		return "", fmt.Errorf("failed to find the home directory to write the Rust extractor into: %w", err)
	}

	crateDir := filepath.Join(home, ".simple", "scripts", rustCompanionName)

	err = fs.WalkDir(rustCompanionCrate, rustCompanionEmbedDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		content, err := rustCompanionCrate.ReadFile(path)
		if err != nil {
			return err
		}

		target := filepath.Join(crateDir, filepath.FromSlash(strings.TrimPrefix(path, rustCompanionEmbedDir+"/")))

		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, content) {
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(target), fsx.DirPerm); err != nil {
			return err
		}

		staged, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(staged.Name()) }()

		if _, err := staged.Write(content); err != nil {
			_ = staged.Close()
			return err
		}
		if err := staged.Close(); err != nil {
			return err
		}
		if err := os.Chmod(staged.Name(), fsx.FilePerm); err != nil {
			return err
		}

		return os.Rename(staged.Name(), target)
	})
	if err != nil {
		return "", fmt.Errorf("failed to write the Rust extractor: %w", err)
	}

	return crateDir, nil
}

// executeScript runs the embedded generator over one action
func executeScript(actionDir string, lang ActionLanguage) error {
	// Find workspace root to run the script from there (so Node.js can find packages)
	workspaceRoot, err := findWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("failed to find workspace root: %w", err)
	}

	scriptPath, discard, err := unpackGenerator(workspaceRoot, lang)
	if err != nil {
		return err
	}

	defer discard()

	// Captured, not inherited: this runs once per action while the progress UI
	// is repainting, and interleaved child output corrupts the frame.
	cmd := exec.Command("node", scriptPath, actionDir)
	cmd.Dir = workspaceRoot

	if out, err := cmd.CombinedOutput(); err != nil {
		// A refusal is the generator working, and it is the one failure whose
		// text an author is meant to act on — so it is handed back as the
		// sentence the generator wrote rather than buried under this layer's
		// account of how the process ended.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == AnnotationRefusalExitCode {
			return &AnnotationRefusal{Refusal: strings.TrimSpace(string(out))}
		}

		return fmt.Errorf("script execution failed: %w\nOutput: %s", err, out)
	}

	return nil
}

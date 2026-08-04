package build

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"simple-cli/internal/fsx"
)

//go:embed scripts/extract-ts-metadata.js
var extractScriptContent string

//go:embed scripts/extract_godoc.go
var extractGoDocContent string

// describeActionFromSource describes an action from its own source by running
// the generator the platform runs, rather than having this package reimplement
// what it does.
//
// ONE GENERATOR, BOTH LANGUAGES. It reads the action's source itself and
// dispatches on what it finds there: TypeScript through ts-morph and
// ts-json-schema-generator, Go through the extractor it builds from the file
// carried beside it. Which is why both files are written out together — the
// generator looks for the Go extractor NEXT TO ITSELF, and this tool used to
// leave the two in different directories, so the Go half of the one generator
// could not be found by the half that runs it.
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
//   - Extraction script is not found
//   - Script execution fails, including refusing a malformed exposure statement
//   - No readable action.json was produced
func describeActionFromSource(fs fsx.FileSystem, actionDir string) error {
	// Check if Node.js is available
	if err := checkNodeJS(); err != nil {
		return fmt.Errorf("node.js is required for action metadata extraction: %w", err)
	}

	// Ensure required npm packages are installed
	if err := ensureNPMPackages(); err != nil {
		return fmt.Errorf("failed to install required npm packages: %w", err)
	}

	// Execute the generator
	if err := executeScript(actionDir); err != nil {
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
		return fmt.Errorf("node.js is required for TypeScript metadata extraction: %w", err)
	}

	// Check Node.js version (should be >= 18 for ESM support)
	version := strings.TrimSpace(string(output))
	if !strings.HasPrefix(version, "v") {
		return fmt.Errorf("unexpected node version format: %s", version)
	}

	return nil
}

// ensureNPMPackages checks if required packages are installed and installs them if needed
func ensureNPMPackages() error {
	// Check if packages are already installed by trying to resolve them
	// We check in the workspace root (where pnpm workspace is configured)
	workspaceRoot, err := findWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("failed to find workspace root: %w", err)
	}

	// Check if packages exist in node_modules
	packagesToCheck := []string{
		"ts-json-schema-generator",
		"ts-morph",
	}

	allInstalled := true
	for _, pkg := range packagesToCheck {
		pkgPath := filepath.Join(workspaceRoot, "node_modules", pkg)
		if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
			allInstalled = false
			break
		}
	}

	// If packages are not installed, install them.
	// Output is captured rather than inherited: builds run under a progress UI
	// that repaints in place, and a concurrent write from a child process
	// corrupts the frame. The output is surfaced only if the install fails.
	if !allInstalled {
		cmd := exec.Command("pnpm", "add", "-w", "-D", "ts-json-schema-generator", "ts-morph")
		cmd.Dir = workspaceRoot

		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to install packages: %w\nOutput: %s", err, out)
		}
	}

	return nil
}

// findWorkspaceRoot finds the pnpm workspace root by looking for pnpm-workspace.yaml
func findWorkspaceRoot() (string, error) {
	// Start from current directory and walk up
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		// Check for pnpm-workspace.yaml
		workspaceFile := filepath.Join(dir, "pnpm-workspace.yaml")
		if _, err := os.Stat(workspaceFile); err == nil {
			return dir, nil
		}

		// Check for package.json with workspaces field (fallback)
		packageJSON := filepath.Join(dir, "package.json")
		if _, err := os.Stat(packageJSON); err == nil {
			// This might be the root, return it
			return dir, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", fmt.Errorf("workspace root not found (no pnpm-workspace.yaml or package.json)")
		}
		dir = parent
	}
}

// unpackGenerator writes both halves of the embedded generator into a directory
// of this invocation's own, under the workspace root, and hands back the script
// to run and the way to discard them.
//
// UNDER THE WORKSPACE ROOT because ESM resolves `node_modules` from the
// IMPORTING FILE's directory upwards, so a generator run from anywhere else
// cannot find ts-morph. BOTH HALVES because the generator looks for the Go
// extractor beside itself, and writing the two to different directories left a
// Go action describable by neither tool.
//
// OF THIS INVOCATION'S OWN because one fixed path was shared by every concurrent
// extraction in a build. Each wrote the same bytes there, so nothing was
// corrupted — but each also deleted the file when it finished, and a build runs
// these in parallel. One action's cleanup removed the script another action's
// node process had not finished starting up with, and that action failed to
// build with `Cannot find module` naming a path nothing in the source mentions.
// Measured at 1 failure in 48 actions at 16-way concurrency, which is a build
// that fails on which actions happened to finish first.
func unpackGenerator(workspaceRoot string) (string, func(), error) {
	dir, err := os.MkdirTemp(workspaceRoot, ".simple-extract-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create a directory for the generator: %w", err)
	}

	discard := func() { _ = os.RemoveAll(dir) }

	scriptPath := filepath.Join(dir, "extract-ts-metadata.js")
	if err := os.WriteFile(scriptPath, []byte(extractScriptContent), 0644); err != nil {
		discard()
		return "", nil, fmt.Errorf("failed to write the generator: %w", err)
	}

	goDocPath := filepath.Join(dir, "extract_godoc.go")
	if err := os.WriteFile(goDocPath, []byte(extractGoDocContent), 0644); err != nil {
		discard()
		return "", nil, fmt.Errorf("failed to write the Go extractor: %w", err)
	}

	return scriptPath, discard, nil
}

// executeScript runs the embedded generator over one action
func executeScript(actionDir string) error {
	// Find workspace root to run the script from there (so Node.js can find packages)
	workspaceRoot, err := findWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("failed to find workspace root: %w", err)
	}

	scriptPath, discard, err := unpackGenerator(workspaceRoot)
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

package scaffold

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
)

type workspaceDevbox struct {
	Packages map[string]string `json:"packages"`
	Env      map[string]string `json:"env"`
	Shell    struct {
		InitHook []string `json:"init_hook"`
	} `json:"shell"`
}

type workspaceManifest struct {
	Private         bool              `json:"private"`
	PackageManager  string            `json:"packageManager"`
	Engines         map[string]string `json:"engines"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// A new workspace runs its tools through devbox and pins pnpm for corepack.
// Without the pin, corepack fetches the newest pnpm, and pnpm 12.15.0 ships no
// bin/pnpm.cjs for it to start, so `devbox run -- pnpm` fails at once.
//
// It gets Rust the same way. Without rustup, the release it installs and the
// WebAssembly target, `simple build` of a Rust action in a workspace that has
// just been created stops at "cargo was not found on PATH".
func TestNewWorkspaceGetsDevboxAndPackageManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme")
	if err := CreateMonorepoStructure(fsx.OSFileSystem{}, TemplatesFS, root, MonorepoConfig{ProjectName: "acme", TenantName: "acme"}); err != nil {
		t.Fatalf("CreateMonorepoStructure() error = %v", err)
	}

	var devbox workspaceDevbox
	readWorkspaceJSON(t, filepath.Join(root, "devbox.json"), &devbox)
	if devbox.Packages["nodejs"] == "" || devbox.Env["DEVBOX_COREPACK_ENABLED"] != "true" || devbox.Env["COREPACK_INTEGRITY_KEYS"] != "0" {
		t.Fatalf("devbox.json = %+v, want node through devbox with corepack enabled", devbox)
	}
	installsRustTarget := slices.ContainsFunc(devbox.Shell.InitHook, func(line string) bool {
		return strings.Contains(line, "toolchain install") && strings.Contains(line, build.RustWasmTarget)
	})
	if devbox.Packages["rustup"] == "" || devbox.Env["RUSTUP_TOOLCHAIN"] == "" || !installsRustTarget {
		t.Fatalf("devbox.json = %+v, want rustup, a Rust release and a start-up line that installs %s, so a new workspace can build Rust actions", devbox, build.RustWasmTarget)
	}
	var manifest workspaceManifest
	readWorkspaceJSON(t, filepath.Join(root, "package.json"), &manifest)
	if !manifest.Private || !strings.HasPrefix(manifest.PackageManager, "pnpm@") || manifest.Engines["node"] == "" {
		t.Fatalf("package.json = %+v, want a private manifest pinning pnpm", manifest)
	}
	// The root has packages of its own to install, so it carries pnpm's
	// install protections. It lists no members: `simple build` and
	// `simple test` install each action's and space's packages themselves, and
	// a member would be installed a second time by a root install.
	workspacePath := filepath.Join(root, "pnpm-workspace.yaml")
	workspace, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read pnpm-workspace.yaml: %v", err)
	}
	if !strings.Contains(string(workspace), "trustPolicy: no-downgrade") {
		t.Errorf("pnpm-workspace.yaml = %q, want trustPolicy: no-downgrade", workspace)
	}
	for _, line := range strings.Split(string(workspace), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "packages:") || strings.Contains(trimmed, "apps/") {
			t.Errorf("pnpm-workspace.yaml must not list package members, found %q", line)
		}
	}
}

// The versions are the ones Simple's own app repositories pin in their
// devbox.json and package.json, so an instance workspace builds with the same
// node, pnpm and Rust. Change them together with those repositories.
//
// The Rust release is one exact version, never a channel such as "stable": a
// channel means whatever was newest on the day the environment first started,
// so two checkouts would compile the same action with different compilers.
func TestWorkspaceToolVersionsArePinned(t *testing.T) {
	var devbox workspaceDevbox
	readTemplateJSON(t, "templates/devbox.json", &devbox)
	if devbox.Packages["nodejs"] != "25.9.0" {
		t.Errorf("devbox.json nodejs = %q, want 25.9.0", devbox.Packages["nodejs"])
	}
	if devbox.Packages["rustup"] != "1.29.3" {
		t.Errorf("devbox.json rustup = %q, want 1.29.3", devbox.Packages["rustup"])
	}
	toolchain := devbox.Env["RUSTUP_TOOLCHAIN"]
	if toolchain != "1.100.0" {
		t.Errorf("devbox.json RUSTUP_TOOLCHAIN = %q, want 1.100.0", toolchain)
	}
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(toolchain) {
		t.Errorf("devbox.json RUSTUP_TOOLCHAIN = %q, want one exact release such as 1.100.0, not a channel", toolchain)
	}
	var manifest workspaceManifest
	readTemplateJSON(t, "templates/package.json", &manifest)
	if manifest.PackageManager != "pnpm@10.34.9" || manifest.Engines["node"] != ">=25" {
		t.Errorf("package.json packageManager = %q, engines.node = %q, want pnpm@10.34.9 and >=25", manifest.PackageManager, manifest.Engines["node"])
	}
	for _, dependency := range []struct{ name, version string }{
		{name: "@commitlint/cli", version: "20.5.0"},
		{name: "@commitlint/config-conventional", version: "20.5.0"},
		{name: "lint-staged", version: "16.2.7"},
	} {
		if got := manifest.DevDependencies[dependency.name]; got != dependency.version {
			t.Errorf("package.json devDependency %s = %q, want %q", dependency.name, got, dependency.version)
		}
	}
}

// The start-up line has three parts, `question || install || notice`, and each
// one keeps a promise.
//
// The question keeps a start quiet and quick once Rust is complete: rustup is
// asked what is installed, and nothing more runs when all three pieces are
// there: the formatter, the linter, and the standard library for the
// WebAssembly target. A workspace whose Rust was installed before the
// formatter and the linter were part of it is two pieces short, so its next
// start adds them. The standard library's name is matched to its end, so a
// second WebAssembly target whose name begins the same way is not counted in
// its place. grep counts
// through the whole list rather than stopping at a match: in a shell that
// counts a failure anywhere in a pipe (pipefail), stopping early can break
// rustup's last write and turn a yes into an install on every start.
//
// The install takes the minimal profile, which is the compiler, cargo and the
// standard library, plus the formatter and the linter: 583 MB. What it leaves
// out is the documentation, most of what the default profile adds.
//
// RUSTUP_AUTO_INSTALL=0 makes the install the only thing that ever downloads
// Rust. Without it rustup answers a question about a release it does not have,
// and any cargo or rustc call, by downloading that release itself,
// documentation included (1.5 GB against 583 MB), saying nothing while it does.
//
// The notice keeps the rest of the environment usable when the install fails,
// with no network for one. devbox runs nothing after a start-up line that
// fails, so without the notice `devbox run -- pnpm install` would stop at the
// Rust download in a workspace that has no Rust action in it.
//
// The line is compared whole because Simple's own app repositories carry the
// same line: change it here and there together.
func TestWorkspaceRustInstallAsksFirstAndNeverBlocks(t *testing.T) {
	var devbox workspaceDevbox
	readTemplateJSON(t, "templates/devbox.json", &devbox)
	if devbox.Env["RUSTUP_AUTO_INSTALL"] != "0" {
		t.Errorf("devbox.json RUSTUP_AUTO_INSTALL = %q, want 0 so only the start-up line downloads Rust", devbox.Env["RUSTUP_AUTO_INSTALL"])
	}
	question := `[ "$(rustup component list --installed --toolchain "$RUSTUP_TOOLCHAIN" 2>/dev/null | grep -c -E '^(rustfmt-|clippy-|rust-std-` + build.RustWasmTarget + `$)')" = 3 ]`
	install := `rustup toolchain install "$RUSTUP_TOOLCHAIN" --profile minimal --component rustfmt,clippy --target ` + build.RustWasmTarget
	notice := `echo "Rust $RUSTUP_TOOLCHAIN is not completely installed: see the error above. Everything else here works, but Rust actions may not build, format or lint until a later start completes it." >&2`
	hooksPath := `root="$(git rev-parse --show-toplevel 2>/dev/null)" && [ "$(cd "$root" 2>/dev/null && pwd -P)" = "$(pwd -P)" ] && git config core.hooksPath .githooks >/dev/null 2>&1 || true`
	want := []string{hooksPath, question + " || " + install + " || " + notice}
	if !slices.Equal(devbox.Shell.InitHook, want) {
		t.Errorf("devbox.json init_hook =\n%q\nwant\n%q", devbox.Shell.InitHook, want)
	}
}

func TestGeneratedWorkspaceContainsCommitChecks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme")
	fsys := &permissionRecordingFileSystem{FileSystem: fsx.OSFileSystem{}, written: make(map[string]os.FileMode)}
	if err := CreateMonorepoStructure(fsys, TemplatesFS, root, MonorepoConfig{ProjectName: "acme", TenantName: "acme"}); err != nil {
		t.Fatalf("CreateMonorepoStructure() error = %v", err)
	}

	files := []struct {
		name       string
		executable bool
	}{
		{name: ".gitattributes"},
		{name: ".githooks/pre-commit", executable: true},
		{name: ".githooks/commit-msg", executable: true},
		{name: ".githooks/pre-push", executable: true},
		{name: ".githooks/post-checkout", executable: true},
		{name: ".githooks/post-merge", executable: true},
		{name: ".githooks/post-rewrite", executable: true},
		{name: "scripts/supply-chain-guard.sh", executable: true},
		{name: "commitlint.config.js"},
	}
	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(file.name))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("generated file is missing: %v", err)
			}
			wantPerm := fsx.FilePerm
			if file.executable {
				wantPerm = fsx.ExecPerm
			}
			if got := fsys.written[path]; got != wantPerm {
				t.Errorf("write permission = %04o, want %04o", got, wantPerm)
			}
			if file.executable && runtime.GOOS != "windows" && info.Mode().Perm()&0o111 != 0o111 {
				t.Errorf("generated file mode = %04o, want executable bits", info.Mode().Perm())
			}
		})
	}
}

func TestGeneratedSupplyChainGuardTree(t *testing.T) {
	requireSupplyChainGuardTools(t)

	tests := []struct {
		name        string
		mode        string
		prepare     func(t *testing.T, root string)
		wantSuccess bool
		wantText    string
	}{
		{name: "fresh workspace passes in tree mode", mode: "--tree", wantSuccess: true},
		{name: "fresh workspace passes in staged mode", mode: "--staged", wantSuccess: true},
		{
			name: "hidden code is refused",
			mode: "--tree",
			prepare: func(t *testing.T, root string) {
				content := "const visible = true;\n" + strings.Repeat(" ", 2000) + "require('x')\n"
				writeGuardFixture(t, root, "apps/com.acme.demo/hidden.ts", content)
			},
			wantText: "hidden code found",
		},
		{
			name: "install script is refused",
			mode: "--tree",
			prepare: func(t *testing.T, root string) {
				writeGuardFixture(t, root, "apps/com.acme.demo/package.json", `{"name":"com.acme.demo","scripts":{"postinstall":"node install.js"}}`)
			},
			wantText: "postinstall",
		},
		{
			name:        "root node_modules file is excluded",
			mode:        "--tree",
			wantSuccess: true,
			prepare: func(t *testing.T, root string) {
				content := strings.Repeat(" ", 2000) + "require('x')\n"
				writeGuardFixture(t, root, "node_modules/hidden.ts", content)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "acme")
			if err := CreateMonorepoStructure(fsx.OSFileSystem{}, TemplatesFS, root, MonorepoConfig{ProjectName: "acme", TenantName: "acme"}); err != nil {
				t.Fatalf("CreateMonorepoStructure() error = %v", err)
			}
			runGuardGit(t, root, "init")
			if tt.prepare != nil {
				tt.prepare(t, root)
			}
			runGuardGit(t, root, "add", "--all")

			output, err := runSupplyChainGuard(t, root, tt.mode)
			if (err == nil) != tt.wantSuccess {
				t.Fatalf("guard error = %v, wantSuccess %v\n%s", err, tt.wantSuccess, output)
			}
			if tt.wantText != "" && !strings.Contains(string(output), tt.wantText) {
				t.Errorf("guard output = %q, want it to contain %q", output, tt.wantText)
			}
		})
	}
}

func TestGeneratedSupplyChainGuardStaged(t *testing.T) {
	requireSupplyChainGuardTools(t)

	tests := []struct {
		name            string
		prepare         func(t *testing.T, root string) string
		afterStage      func(t *testing.T, root, name string)
		cleanAfterStage bool
		wantSuccess     bool
		wantText        string
	}{
		{
			name: "reads staged payload after working copy is cleaned",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/hidden.ts"
				writeGuardFixture(t, root, name, "const visible = true;\n"+strings.Repeat(" ", 2000)+"require('x')\n")
				return name
			},
			cleanAfterStage: true,
			wantText:        "hidden code found",
		},
		{
			name: "unrelated staged file larger than one MiB does not fail",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/large.bin"
				writeGuardFixture(t, root, name, strings.Repeat("x", (1<<20)+1))
				return name
			},
			wantSuccess: true,
		},
		{
			name: "staged submodule entry is ignored",
			prepare: func(t *testing.T, root string) string {
				return "apps/com.acme.demo/vendor.ts"
			},
			afterStage: func(t *testing.T, root, _ string) {
				runGuardGit(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-m", "baseline")
				command := exec.Command("git", "-C", root, "rev-parse", "HEAD")
				oid, err := command.Output()
				if err != nil {
					t.Fatalf("read baseline commit for submodule entry: %v", err)
				}
				runGuardGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+strings.TrimSpace(string(oid))+",apps/com.acme.demo/vendor.ts")
			},
			wantSuccess: true,
		},
		{
			name: "staged rename reads the renamed file",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/old.ts"
				writeGuardFixture(t, root, name, strings.Repeat(" ", 2000)+"require('x')\n")
				return name
			},
			afterStage: func(t *testing.T, root, name string) {
				runGuardGit(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-m", "baseline")
				runGuardGit(t, root, "mv", name, "apps/com.acme.demo/renamed.ts")
			},
			wantText: "hidden code found",
		},
		{
			name: "staged delete does not read removed content",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/deleted.ts"
				writeGuardFixture(t, root, name, strings.Repeat(" ", 2000)+"require('x')\n")
				return name
			},
			afterStage: func(t *testing.T, root, name string) {
				runGuardGit(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-m", "baseline")
				runGuardGit(t, root, "rm", name)
			},
			wantSuccess: true,
		},
		{
			name: "preinstall script is refused",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/package.json"
				writeGuardFixture(t, root, name, `{"scripts":{"preinstall":"node setup.js"}}`)
				return name
			},
			wantText: "preinstall",
		},
		{
			name: "install script is refused",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/package.json"
				writeGuardFixture(t, root, name, `{"scripts":{"install":"node setup.js"}}`)
				return name
			},
			wantText: "install runs",
		},
		{
			name: "postinstall script is refused",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/package.json"
				writeGuardFixture(t, root, name, `{"scripts":{"postinstall":"node setup.js"}}`)
				return name
			},
			wantText: "postinstall",
		},
		{
			name: "prepare script is refused",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/package.json"
				writeGuardFixture(t, root, name, `{"scripts":{"prepare":"node setup.js"}}`)
				return name
			},
			wantText: "prepare",
		},
		{
			name: ".npmrc script setting is refused",
			prepare: func(t *testing.T, root string) string {
				name := ".npmrc"
				writeGuardFixture(t, root, name, "pnpmfile=./setup.cjs\n")
				return name
			},
			wantText: ".npmrc",
		},
		{
			name: ".pnpmfile.cjs is refused",
			prepare: func(t *testing.T, root string) string {
				name := ".pnpmfile.cjs"
				writeGuardFixture(t, root, name, "module.exports = {};\n")
				return name
			},
			wantText: ".pnpmfile.cjs",
		},
		{
			name: "binding.gyp is refused",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/binding.gyp"
				writeGuardFixture(t, root, name, "{}\n")
				return name
			},
			wantText: "binding.gyp",
		},
		{
			name: "package.yaml is refused",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/package.yaml"
				writeGuardFixture(t, root, name, "scripts: {}\n")
				return name
			},
			wantText: "package.yaml",
		},
		{
			name: "package.json5 is refused",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/package.json5"
				writeGuardFixture(t, root, name, "{ scripts: {} }\n")
				return name
			},
			wantText: "package.json5",
		},
		{
			name: "space in file name is preserved",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/has space.ts"
				writeGuardFixture(t, root, name, strings.Repeat(" ", 2000)+"require('x')\n")
				return name
			},
			wantText: "apps/com.acme.demo/has space.ts",
		},
		{
			name: "non-ASCII letter in file name is preserved",
			prepare: func(t *testing.T, root string) string {
				name := "apps/com.acme.demo/café.ts"
				writeGuardFixture(t, root, name, strings.Repeat(" ", 2000)+"require('x')\n")
				return name
			},
			wantText: "apps/com.acme.demo/café.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "acme")
			if err := CreateMonorepoStructure(fsx.OSFileSystem{}, TemplatesFS, root, MonorepoConfig{ProjectName: "acme", TenantName: "acme"}); err != nil {
				t.Fatalf("CreateMonorepoStructure() error = %v", err)
			}
			runGuardGit(t, root, "init")
			name := tt.prepare(t, root)
			runGuardGit(t, root, "add", "--all")
			if tt.afterStage != nil {
				tt.afterStage(t, root, name)
			}
			if tt.cleanAfterStage {
				writeGuardFixture(t, root, name, "const visible = true;\n")
			}

			output, err := runSupplyChainGuard(t, root, "--staged")
			if (err == nil) != tt.wantSuccess {
				t.Fatalf("guard error = %v, wantSuccess %v\n%s", err, tt.wantSuccess, output)
			}
			if !tt.wantSuccess && err == nil {
				t.Fatalf("guard passed, want refusal containing %q\n%s", tt.wantText, output)
			}
			if tt.wantText != "" && !strings.Contains(string(output), tt.wantText) {
				t.Errorf("guard output = %q, want it to contain %q", output, tt.wantText)
			}
		})
	}
}

func TestGeneratedPreCommitRefusesMissingNpx(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is missing; cannot run the generated pre-commit hook: %v", err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is missing; cannot provide the generated hook's repository root: %v", err)
	}

	tests := []struct {
		name     string
		missing  string
		wantText string
	}{
		{name: "npx", missing: "npx", wantText: "npx is unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "acme")
			if err := CreateMonorepoStructure(fsx.OSFileSystem{}, TemplatesFS, root, MonorepoConfig{ProjectName: "acme", TenantName: "acme"}); err != nil {
				t.Fatalf("CreateMonorepoStructure() error = %v", err)
			}
			runGuardGit(t, root, "init")
			envFile := filepath.Join(t.TempDir(), "bash_env")
			if err := os.WriteFile(envFile, []byte("git() { \"$HOOK_TEST_GIT\" \"$@\"; }\nnode() { :; }\n"), fsx.FilePerm); err != nil {
				t.Fatalf("write Bash environment: %v", err)
			}
			command := exec.Command(bashPath, filepath.Join(root, ".githooks", "pre-commit"))
			command.Dir = root
			command.Env = []string{"PATH=", "BASH_ENV=" + envFile, "HOOK_TEST_GIT=" + gitPath}
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("pre-commit passed, want refusal because %s is missing\n%s", tt.missing, output)
			}
			if !strings.Contains(string(output), tt.wantText) || !strings.Contains(string(output), "devbox shell") {
				t.Errorf("pre-commit output = %q, want %q and the dev-environment message", output, tt.wantText)
			}
		})
	}
}

func TestCopyWorkspaceHooksNormalizesLineEndings(t *testing.T) {
	tests := []string{
		".githooks/pre-commit",
		".githooks/commit-msg",
		".githooks/pre-push",
		".githooks/post-checkout",
		".githooks/post-merge",
		".githooks/post-rewrite",
		"scripts/supply-chain-guard.sh",
	}
	root := t.TempDir()
	if err := copyWorkspaceHooks(fsx.OSFileSystem{}, crlfTemplateFS{TemplateFS: TemplatesFS}, root); err != nil {
		t.Fatalf("copyWorkspaceHooks() error = %v", err)
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil {
				t.Fatalf("read generated file: %v", err)
			}
			if strings.ContainsRune(string(content), '\r') {
				t.Errorf("generated file contains CR line endings")
			}
			if !strings.ContainsRune(string(content), '\n') {
				t.Errorf("generated file contains no LF line endings")
			}
		})
	}
}

type crlfTemplateFS struct{ fsx.TemplateFS }

func (f crlfTemplateFS) ReadFile(name string) ([]byte, error) {
	content, err := f.TemplateFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(name, "templates/githooks/") || name == "templates/scripts/supply-chain-guard.sh" {
		content = []byte(strings.ReplaceAll(string(content), "\n", "\r\n"))
	}
	return content, nil
}

func requireSupplyChainGuardTools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash is missing; cannot run the generated supply-chain guard: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is missing; cannot provide the generated guard's file list: %v", err)
	}
}

func runSupplyChainGuard(t *testing.T, root, mode string) ([]byte, error) {
	t.Helper()
	command := exec.Command("bash", filepath.Join(root, "scripts", "supply-chain-guard.sh"), mode)
	command.Dir = root
	return command.CombinedOutput()
}

func writeGuardFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), fsx.DirPerm); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), fsx.FilePerm); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func runGuardGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

type permissionRecordingFileSystem struct {
	fsx.FileSystem
	written map[string]os.FileMode
}

func (f *permissionRecordingFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	f.written[name] = perm
	return f.FileSystem.WriteFile(name, data, perm)
}

func readWorkspaceJSON(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(contents, value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func readTemplateJSON(t *testing.T, name string, value any) {
	t.Helper()
	contents, err := TemplatesFS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := json.Unmarshal(contents, value); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

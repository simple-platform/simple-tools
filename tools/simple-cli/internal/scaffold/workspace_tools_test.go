package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	Private        bool              `json:"private"`
	PackageManager string            `json:"packageManager"`
	Engines        map[string]string `json:"engines"`
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
	// The workspace has no member packages to list.
	if _, err := os.Stat(filepath.Join(root, "pnpm-workspace.yaml")); !os.IsNotExist(err) {
		t.Fatalf("pnpm-workspace.yaml was written: %v", err)
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
}

// The start-up line has three parts, `question || install || notice`, and each
// one keeps a promise.
//
// The question keeps a start quiet and quick once Rust is there: rustup is
// asked what is installed, and nothing more runs when the answer is yes. grep
// reads the whole answer rather than stopping at the first match (-q): in a
// shell that counts a failure anywhere in a pipe (pipefail), stopping early
// can break rustup's last write and turn a yes into an install on every start.
//
// The profile is minimal: the compiler, cargo and the standard library, which
// is all a build or a test needs.
//
// RUSTUP_AUTO_INSTALL=0 makes the install the only thing that ever downloads
// Rust. Without it rustup answers a question about a release it does not have,
// and any cargo or rustc call, by downloading that release itself,
// documentation included (1.5 GB against 564 MB), saying nothing while it does.
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
	question := `rustup target list --installed --toolchain "$RUSTUP_TOOLCHAIN" 2>/dev/null | grep -x ` + build.RustWasmTarget + ` >/dev/null`
	install := `rustup toolchain install "$RUSTUP_TOOLCHAIN" --profile minimal --target ` + build.RustWasmTarget
	notice := `echo "Rust $RUSTUP_TOOLCHAIN could not be installed: see the error above. Everything else here works, but Rust actions will not build until a later start installs it." >&2`
	want := []string{question + " || " + install + " || " + notice}
	if !slices.Equal(devbox.Shell.InitHook, want) {
		t.Errorf("devbox.json init_hook =\n%q\nwant\n%q", devbox.Shell.InitHook, want)
	}
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

package scaffold

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
)

// A Rust action or tool must pass Rust's own formatter and linter as
// generated: `cargo fmt --check` and `cargo clippy -- -D warnings`, for the
// computer it is written on, tests included, and for the WebAssembly target
// it is built for, where its tests are never built.
// Whoever runs either in a new workspace should find nothing wrong in code
// they have not touched yet.
//
// It runs with the release the workspace template names, not with whatever
// Rust is newest on the machine. A newer linter knows lints this release does
// not, so judging with it would fail a template over something a new
// workspace's own tools never say.
func TestScaffoldedRustPassesFmtAndClippy(t *testing.T) {
	var devbox workspaceDevbox
	readTemplateJSON(t, "templates/devbox.json", &devbox)
	release := devbox.Env["RUSTUP_TOOLCHAIN"]
	if release == "" {
		t.Fatal("templates/devbox.json has no RUSTUP_TOOLCHAIN release")
	}
	requireRustQualityToolchain(t, release)

	targetDir := filepath.Join(t.TempDir(), "cargo-target")
	tests := []struct {
		name       string
		actionName string
		tool       bool
		execution  string
		commands   [][]string
	}{
		{
			name:       "plain Rust action",
			actionName: "plain-action",
			execution:  "server",
			commands: [][]string{
				{"fmt", "--check"},
				{"clippy", "--all-targets", "--", "-D", "warnings"},
				{"clippy", "--target", build.RustWasmTarget, "--", "-D", "warnings"},
			},
		},
		{
			name:       "Rust action for server and browser",
			actionName: "shared-action",
			execution:  "both",
			commands: [][]string{
				{"fmt", "--check"},
				{"clippy", "--all-targets", "--", "-D", "warnings"},
				{"clippy", "--target", build.RustWasmTarget, "--", "-D", "warnings"},
				{"clippy", "--target", build.RustWasmTarget, "--features", "async", "--", "-D", "warnings"},
			},
		},
		{
			name:       "Rust tool",
			actionName: "report-status",
			tool:       true,
			execution:  "server",
			commands: [][]string{
				{"fmt", "--check"},
				{"clippy", "--all-targets", "--", "-D", "warnings"},
				{"clippy", "--target", build.RustWasmTarget, "--", "-D", "warnings"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "acme")
			if err := CreateMonorepoStructure(fsx.OSFileSystem{}, TemplatesFS, root, MonorepoConfig{
				ProjectName: "acme",
				TenantName:  "acme",
			}); err != nil {
				t.Fatalf("create workspace: %v", err)
			}
			if err := CreateAppStructure(fsx.OSFileSystem{}, TemplatesFS, root, "com.acme.demo", "Acme Demo", ""); err != nil {
				t.Fatalf("create app: %v", err)
			}

			actionDir := filepath.Join(root, "apps", "com.acme.demo", "actions", tt.actionName)
			if tt.tool {
				if _, err := CreateTool(fsx.OSFileSystem{}, TemplatesFS, root, ToolConfig{
					AppID:        "com.acme.demo",
					Name:         tt.actionName,
					DisplayName:  "Report Status",
					Language:     LanguageRust,
					ExecutionEnv: tt.execution,
				}); err != nil {
					t.Fatalf("create Rust tool: %v", err)
				}
			} else if err := CreateActionStructure(fsx.OSFileSystem{}, TemplatesFS, root, ActionConfig{
				AppID:        "com.acme.demo",
				ActionName:   tt.actionName,
				DisplayName:  "Plain Action",
				Language:     LanguageRust,
				ExecutionEnv: tt.execution,
			}); err != nil {
				t.Fatalf("create Rust action: %v", err)
			}

			for _, args := range tt.commands {
				cmd := exec.Command("cargo", args...)
				cmd.Dir = actionDir
				cmd.Env = rustCommandEnvironment(os.Environ(), release, targetDir)
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("cargo %s failed: %v\n%s", strings.Join(args, " "), err, output)
				}
			}
		})
	}
}

// requireRustQualityToolchain lets the test run only where the template's
// release is installed with the formatter, the linter and the WebAssembly
// target. This repository's own dev environment has no Rust, so on a
// contributor's computer the test is skipped, with the command that installs
// what it needs. In CI it fails instead: a skipped check there would let a
// template ship with a finding and nobody would see that nothing was checked.
func requireRustQualityToolchain(t *testing.T, release string) {
	t.Helper()
	install := fmt.Sprintf("rustup toolchain install %s --profile minimal --component rustfmt,clippy --target %s", release, build.RustWasmTarget)
	if _, err := exec.LookPath("cargo"); err != nil {
		reportRustToolchainUnavailable(t, install, "cargo is not on PATH")
		return
	}
	rustup, err := exec.LookPath("rustup")
	if err != nil {
		reportRustToolchainUnavailable(t, install, "rustup is not on PATH")
		return
	}
	cmd := exec.Command(rustup, "component", "list", "--installed", "--toolchain", release)
	// Asked about a release it does not have, rustup downloads it, complete
	// with documentation, before it answers. This is a question, not an
	// install.
	cmd.Env = append(os.Environ(), "RUSTUP_AUTO_INSTALL=0")
	output, err := cmd.CombinedOutput()
	if err != nil || !hasRustQualityComponents(string(output)) {
		reportRustToolchainUnavailable(t, install, "the pinned release is missing rustfmt, clippy, or the WebAssembly standard library")
	}
}

func reportRustToolchainUnavailable(t *testing.T, install, reason string) {
	t.Helper()
	message := fmt.Sprintf("%s; install it with `%s`", reason, install)
	// The same reading of CI as the CLI's own (ciEnabled in internal/cli):
	// set, and neither "0" nor "false".
	if ci := strings.TrimSpace(os.Getenv("CI")); ci != "" && ci != "0" && !strings.EqualFold(ci, "false") {
		t.Fatalf("%s", message)
	}
	t.Skip(message)
}

func hasRustQualityComponents(installed string) bool {
	var rustfmt, clippy, wasmStd bool
	for _, component := range strings.Split(installed, "\n") {
		component = strings.TrimSpace(component)
		switch {
		case strings.HasPrefix(component, "rustfmt-"):
			rustfmt = true
		case strings.HasPrefix(component, "clippy-"):
			clippy = true
		case component == "rust-std-"+build.RustWasmTarget:
			wasmStd = true
		}
	}
	return rustfmt && clippy && wasmStd
}

func rustCommandEnvironment(environment []string, release, targetDir string) []string {
	result := make([]string, 0, len(environment)+3)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "RUSTUP_TOOLCHAIN=") || strings.HasPrefix(entry, "CARGO_TARGET_DIR=") || strings.HasPrefix(entry, "RUSTUP_AUTO_INSTALL=") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "RUSTUP_TOOLCHAIN="+release, "CARGO_TARGET_DIR="+targetDir, "RUSTUP_AUTO_INSTALL=0")
}

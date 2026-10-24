package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildJavyDownloadURL(t *testing.T) {
	url := buildJavyDownloadURL("1.0.0")
	// Expected format depends on runtime.GOOS/GOARCH which we can't easily mock here without modifying buildJavyDownloadURL to accept them.
	// But we can check basic structure.
	// "https://github.com/bytecodealliance/javy/releases/download/v1.0.0/javy-..."

	if !strings.Contains(url, "v1.0.0/javy") {
		t.Errorf("URL %s does not contain expected version/path", url)
	}
}

func TestMapJavyArch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"aarch64", "arm"},
		{"x86_64", "x86_64"},
		{"other", "other"},
	}

	for _, tt := range tests {
		if got := mapJavyArch(tt.input); got != tt.want {
			t.Errorf("mapJavyArch(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestMapJavyOS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"macos", "macos"},
		{"linux", "linux"},
		{"windows", "windows"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		if got := mapJavyOS(tt.input); got != tt.want {
			t.Errorf("mapJavyOS(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestBuildWasmOptDownloadURL(t *testing.T) {
	url := buildWasmOptDownloadURL("100")
	if !strings.Contains(url, "version_100/binaryen-version_100") {
		t.Errorf("URL %s does not match expected format", url)
	}
}

func TestMapWasmOptArchOS(t *testing.T) {
	tests := []struct {
		arch     string
		platform string
		wantOS   string
		wantArch string
	}{
		{"x86_64", "macos", "macos", "x86_64"},
		{"aarch64", "linux", "linux", "aarch64"},
		{"aarch64", "macos", "macos", "arm64"},
		{"x86_64", "windows", "windows", "x86_64"},
	}

	for _, tt := range tests {
		inputArch := tt.arch
		inputPlatform := tt.platform
		// We are testing mapWasmOptArchOS which logic is embedded in buildWasmOptDownloadURL?
		// No, it's a helper I wrote in wasm.go: mapWasmOptArchOS(arch, platform).

		got := mapWasmOptArchOS(inputArch, inputPlatform)
		if got.OS != tt.wantOS || got.Arch != tt.wantArch {
			t.Errorf("mapWasmOptArchOS(%s, %s) = {%s, %s}, want {%s, %s}",
				inputArch, inputPlatform, got.Arch, got.OS, tt.wantArch, tt.wantOS)
		}
	}
}

// stubRustc puts a shell script named rustc on an otherwise empty PATH, so the
// toolchain checks can be exercised on a machine with no Rust installed at all.
func stubRustc(t *testing.T, script string) string {
	t.Helper()
	binDir := t.TempDir()
	path := filepath.Join(binDir, "rustc")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	return binDir
}

// TestEnsureCargo_Missing pins that a machine with no Rust toolchain is told it
// has no Rust toolchain, and where to get one. Left to cargo, the same
// situation arrives as "executable file not found in $PATH", which reads like
// the action is at fault.
func TestEnsureCargo_Missing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := EnsureCargo()
	if err == nil {
		t.Fatal("Expected an error when cargo is not on PATH, got nil")
	}
	if !strings.Contains(err.Error(), "cargo") || !strings.Contains(err.Error(), InstallRustURL) {
		t.Errorf("Error should name cargo and where to get it, got: %v", err)
	}
}

func TestEnsureCargo_Found(t *testing.T) {
	binDir := t.TempDir()
	cargoPath := filepath.Join(binDir, "cargo")
	if err := os.WriteFile(cargoPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	got, err := EnsureCargo()
	if err != nil {
		t.Fatalf("EnsureCargo() error = %v", err)
	}
	if got != cargoPath {
		t.Errorf("EnsureCargo() = %s, want %s", got, cargoPath)
	}
}

// TestEnsureRustWasmTarget covers the three answers rustc can give. The target
// being absent is a different failure from Rust being absent, and it has a
// different one-line fix, so the two must not collapse into one message.
func TestEnsureRustWasmTarget(t *testing.T) {
	installed := t.TempDir()

	tests := []struct {
		name    string
		script  string
		wantErr string
	}{
		{
			name:    "target installed",
			script:  fmt.Sprintf("echo %s", installed),
			wantErr: "",
		},
		{
			// rustc prints the directory whether or not anyone installed the
			// component, so a path that does not exist is the only evidence
			// that the target is missing.
			name:    "target known but not installed",
			script:  fmt.Sprintf("echo %s", filepath.Join(installed, "never-added")),
			wantErr: "rustup target add",
		},
		{
			name:    "rustc has never heard of the target",
			script:  "echo 'error: could not find specification' >&2; exit 1",
			wantErr: "does not know the",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubRustc(t, tt.script)

			err := EnsureRustWasmTarget()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("EnsureRustWasmTarget() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Expected an error mentioning %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error should contain %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestEnsureRustWasmTarget_NoRustc(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := EnsureRustWasmTarget()
	if err == nil {
		t.Fatal("Expected an error when rustc is not on PATH, got nil")
	}
	if !strings.Contains(err.Error(), "rustc") || !strings.Contains(err.Error(), InstallRustURL) {
		t.Errorf("Error should name rustc and where to get it, got: %v", err)
	}
}

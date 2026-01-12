package build

import (
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

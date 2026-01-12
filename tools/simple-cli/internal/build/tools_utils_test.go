package build

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMapPlatform(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"darwin", "macos"},
		{"linux", "linux"},
		{"windows", "windows"},
		{"other", "other"},
	}

	for _, tt := range tests {
		if got := mapPlatform(tt.input); got != tt.want {
			t.Errorf("mapPlatform(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestMapArch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"amd64", "x86_64"},
		{"arm64", "aarch64"},
		{"other", "other"},
	}

	for _, tt := range tests {
		if got := mapArch(tt.input); got != tt.want {
			t.Errorf("mapArch(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	if err := os.WriteFile(src, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "content" {
		t.Errorf("got %s, want content", string(content))
	}
}

func TestGetPlatformAndArch(t *testing.T) {
	// Just ensure they don't panic and return valid mapped values or original
	p := GetPlatform()
	if p == "" {
		t.Error("GetPlatform() returned empty")
	}
	// Verify it matches mapPlatform(runtime.GOOS)
	if p != mapPlatform(runtime.GOOS) {
		t.Errorf("GetPlatform() = %s, want %s", p, mapPlatform(runtime.GOOS))
	}

	a := GetArch()
	if a == "" {
		t.Error("GetArch() returned empty")
	}
	if a != mapArch(runtime.GOARCH) {
		t.Errorf("GetArch() = %s, want %s", a, mapArch(runtime.GOARCH))
	}
}

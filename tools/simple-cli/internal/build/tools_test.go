package build

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSaveManifest(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	manifest := ToolManifest{
		"tool1": ToolInfo{Version: "1.0.0", LastCheck: time.Now()},
	}

	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	loaded, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}

	if loaded["tool1"].Version != "1.0.0" {
		t.Errorf("Loaded version = %s, want 1.0.0", loaded["tool1"].Version)
	}
}

func TestEnsureTool_Download(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := []byte("binary-content")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer server.Close()

	downloadCount := 0
	def := ToolDef{
		Name: "test-tool",
		CheckVersionFn: func() (string, error) {
			return "2.0.0", nil
		},
		DownloadURLFn: func(version string) string {
			downloadCount++
			return server.URL
		},
	}

	// First run: should download
	path, err := EnsureTool(def)
	if err != nil {
		t.Fatalf("EnsureTool() error = %v", err)
	}

	if !fileExists(path) {
		t.Error("Tool binary not found")
	}

	if downloadCount != 1 {
		t.Errorf("Expected 1 download, got %d", downloadCount)
	}

	// Verify manifest
	manifest, _ := LoadManifest()
	if manifest["test-tool"].Version != "2.0.0" {
		t.Errorf("Manifest version = %s, want 2.0.0", manifest["test-tool"].Version)
	}

	// Second run: should use cached
	// We verify this by ensuring DownloadURLFn is NOT called again,
	// because version check (2.0.0) matches manifest version (2.0.0).

	// Force check by setting LastCheck to old
	manifest["test-tool"] = ToolInfo{
		Version:   "2.0.0",
		LastCheck: time.Now().Add(-48 * time.Hour),
	}
	SaveManifest(manifest)

	_, err = EnsureTool(def)
	if err != nil {
		t.Fatalf("EnsureTool() second run error = %v", err)
	}

	if downloadCount != 1 {
		t.Errorf("Expected 1 download after second run (cached), got %d", downloadCount)
	}
}

func TestEnsureTool_HTTPError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Direct call to downloadTool to verify error handling
	err := downloadTool(server.URL, filepath.Join(tmpDir, "fail"), nil, nil)
	if err == nil {
		t.Error("Expected error for 404, got nil")
	}
}

func TestLoadManifest_Corrupt(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	toolsDir := filepath.Join(tmpDir, SimpleToolsDir)
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(toolsDir, ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("{invalid-json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest()
	if err == nil {
		t.Error("LoadManifest() expected error for corrupt JSON, got nil")
	}
}

func TestSaveManifest_Error(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create a file where directory should be to force mkdir error
	toolsDir := filepath.Join(tmpDir, SimpleToolsDir)
	if err := os.WriteFile(toolsDir, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	manifest := make(ToolManifest)
	err := SaveManifest(manifest)
	if err == nil {
		t.Error("SaveManifest() expected error, got nil")
	}
}

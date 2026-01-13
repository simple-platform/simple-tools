package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePlugin(t *testing.T) {
	tmpDir := t.TempDir()

	// Test extraction of sync plugin
	path, err := EnsurePlugin(tmpDir, false)
	if err != nil {
		t.Fatalf("EnsurePlugin(sync) failed: %v", err)
	}

	if filepath.Base(path) != "simple.plugin.wasm" {
		t.Errorf("Expected filename simple.plugin.wasm, got %s", filepath.Base(path))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("Extracted file is empty")
	}

	// Test extraction of async plugin
	pathAsync, err := EnsurePlugin(tmpDir, true)
	if err != nil {
		t.Fatalf("EnsurePlugin(async) failed: %v", err)
	}

	if filepath.Base(pathAsync) != "simple.plugin.async.wasm" {
		t.Errorf("Expected filename simple.plugin.async.wasm, got %s", filepath.Base(pathAsync))
	}

	infoAsync, err := os.Stat(pathAsync)
	if err != nil {
		t.Fatal(err)
	}
	if infoAsync.Size() == 0 {
		t.Error("Extracted async file is empty")
	}
}

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

func TestEnsurePlugin_UpdatesStaleFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a file with wrong/stale content at the plugin path.
	stale := []byte("stale plugin content that does not match embedded bytes")
	stalePath := filepath.Join(tmpDir, "simple.plugin.wasm")
	if err := os.WriteFile(stalePath, stale, 0644); err != nil {
		t.Fatalf("failed to write stale file: %v", err)
	}

	// EnsurePlugin should detect the hash mismatch and overwrite with the real plugin.
	path, err := EnsurePlugin(tmpDir, false)
	if err != nil {
		t.Fatalf("EnsurePlugin failed: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read updated plugin: %v", err)
	}

	embedded, _ := GetPluginBytes(false)
	if len(updated) != len(embedded) {
		t.Errorf("Expected plugin size %d after update, got %d", len(embedded), len(updated))
	}
}

func TestEnsurePlugin_SkipsWriteWhenUpToDate(t *testing.T) {
	tmpDir := t.TempDir()

	// First extraction — writes the file.
	path, err := EnsurePlugin(tmpDir, false)
	if err != nil {
		t.Fatalf("first EnsurePlugin failed: %v", err)
	}

	firstInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	firstMod := firstInfo.ModTime()

	// Second call — file already matches, should NOT overwrite (mod time unchanged).
	_, err = EnsurePlugin(tmpDir, false)
	if err != nil {
		t.Fatalf("second EnsurePlugin failed: %v", err)
	}

	secondInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if !secondInfo.ModTime().Equal(firstMod) {
		t.Error("Expected plugin file NOT to be rewritten when content matches, but mod time changed")
	}
}

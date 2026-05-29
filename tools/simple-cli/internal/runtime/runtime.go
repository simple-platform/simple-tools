package runtime

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed simple.plugin.wasm simple.plugin.async.wasm
var runtimeFS embed.FS

// GetPluginBytes returns the bytes of the embedded runtime plugin.
func GetPluginBytes(async bool) ([]byte, error) {
	filename := "simple.plugin.wasm"
	if async {
		filename = "simple.plugin.async.wasm"
	}
	return runtimeFS.ReadFile(filename)
}

// WritePluginToFile writes the embedded runtime plugin to the specified path.
func WritePluginToFile(destPath string, async bool) error {
	data, err := GetPluginBytes(async)
	if err != nil {
		return fmt.Errorf("failed to read embedded runtime: %w", err)
	}
	return os.WriteFile(destPath, data, 0644)
}

// EnsurePlugin ensures the runtime plugin on disk matches the plugin embedded in this binary.
//
// It computes the SHA-256 of the embedded plugin bytes and compares them against the
// on-disk file. If the file is missing, empty, or has a different hash (i.e. the CLI
// binary was updated with a new plugin), the file is overwritten with the embedded copy.
//
// This guarantees that every `simple build` always uses the plugin version that shipped
// with the currently installed CLI binary — no stale plugins, no silent mismatches.
//
// Returns the absolute path to the plugin file.
func EnsurePlugin(dir string, async bool) (string, error) {
	filename := "simple.plugin.wasm"
	if async {
		filename = "simple.plugin.async.wasm"
	}
	destPath := filepath.Join(dir, filename)

	embedded, err := GetPluginBytes(async)
	if err != nil {
		return "", fmt.Errorf("failed to read embedded runtime plugin: %w", err)
	}

	// Only write if the on-disk file is missing or has a different hash.
	if needsUpdate(destPath, embedded) {
		if err := os.WriteFile(destPath, embedded, 0644); err != nil {
			return "", fmt.Errorf("failed to write runtime plugin to %s: %w", destPath, err)
		}
	}

	return filepath.Abs(destPath)
}

// needsUpdate returns true if the file at path is missing, unreadable, or its
// SHA-256 hash differs from the provided data.
func needsUpdate(path string, data []byte) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		// File missing or unreadable — always write.
		return true
	}
	embeddedHash := sha256.Sum256(data)
	existingHash := sha256.Sum256(existing)
	return !bytes.Equal(embeddedHash[:], existingHash[:])
}

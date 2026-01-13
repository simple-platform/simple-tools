package runtime

import (
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

// EnsurePlugin ensures the runtime plugin exists in the given directory.
// Returns the absolute path to the plugin file.
func EnsurePlugin(dir string, async bool) (string, error) {
	filename := "simple.plugin.wasm"
	if async {
		filename = "simple.plugin.async.wasm"
	}
	destPath := filepath.Join(dir, filename)

	if err := WritePluginToFile(destPath, async); err != nil {
		return "", err
	}
	return filepath.Abs(destPath)
}

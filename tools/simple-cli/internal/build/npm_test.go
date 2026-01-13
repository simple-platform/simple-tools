package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDependencies(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "npm_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy package.json
	packageJSON := `{"name": "test-package", "version": "1.0.0"}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatalf("Failed to write package.json: %v", err)
	}

	// We can't easily mock exec.Command without more complex abstraction,
	// but we can verify that it attempts to run.
	// However, running actual 'npm install' might be slow or unstable in this environment if npm isn't present.
	// For this specific environment, we will skip the actual execution if npm is not found,
	// or we can test the failure case.

	// Let's test the failure case where the directory is invalid
	err = EnsureDependencies(filepath.Join(tmpDir, "nonexistent"))
	if err == nil {
		t.Error("Expected error for nonexistent directory, got nil")
	}

	// NOTE: To properly test success without running real npm, we would need to refactor npm.go
	// to use a command executor interface, similar to how we handle FileSystem.
	// For now, this adds basic coverage for the existence of the function and failure modes.
}

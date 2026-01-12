package build

import (
	"os"
	"testing"
)

func TestIntegration_DownloadTools(t *testing.T) {
	if os.Getenv("SIMPLE_CLI_INTEGRATION") == "" {
		t.Skip("Skipping integration test (set SIMPLE_CLI_INTEGRATION=1 to run)")
	}

	// Real network calls - careful
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Since we mock ensure functions in manager tests, here we want to test that
	// EnsureSCLParser etc. actually work with EnsureTool.
	// But ensureSCLParser etc. are vars in manager.go, but defined as functions in respective files.
	// This test calls the REAL functions.

	// Use EnsureSCLParser directly
	path, err := EnsureSCLParser(nil)
	if err != nil {
		t.Fatalf("EnsureSCLParser failed: %v", err)
	}

	if !fileExists(path) {
		t.Error("SCL Parser binary not found after ensure")
	}
}

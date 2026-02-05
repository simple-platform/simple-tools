package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLanguage(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Missing index.ts -> Error
	if err := ValidateLanguage(tmpDir); err == nil {
		t.Error("Expected error for missing index.ts, got nil")
	}

	// 2. With index.ts -> OK
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	indexFile := filepath.Join(srcDir, "index.ts")
	if err := os.WriteFile(indexFile, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ValidateLanguage(tmpDir); err != nil {
		t.Errorf("ValidateLanguage() error = %v", err)
	}
}

func TestParseExecutionEnvironment_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	// No 10_actions.scl

	env, err := ParseExecutionEnvironment("dummy-parser", tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if env != "server" {
		t.Errorf("got %s, want server (fallback)", env)
	}
}

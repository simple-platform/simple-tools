package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Removed custom invokeTestCmd abstraction to align with invokeCmd testing patterns.

// invokeTestCmd resets local cobra flags against test pollution before handing off cleanly to invokeCmd.
func invokeTestCmd(args ...string) (string, string, error) {
	_ = testCmd.Flags().Set("action", "")
	_ = testCmd.Flags().Set("behavior", "")
	_ = testCmd.Flags().Set("space", "")
	_ = testCmd.Flags().Set("coverage", "false")
	_ = testCmd.Flags().Set("json", "false")
	return invokeCmd(args...)
}

func TestTestCmd_AppNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup monorepo structure
	_ = os.Mkdir(filepath.Join(tmpDir, "apps"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"test", "com.example.missing"}
	_, _, err := invokeTestCmd(args...)

	if err == nil {
		t.Error("Expected error for missing app")
	}
	if !strings.Contains(err.Error(), "app not found") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestTestCmd_ActionNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup monorepo structure
	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(appDir, 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"test", "com.example.test", "--action", "missing-action"}
	_, _, err := invokeTestCmd(args...)

	if err == nil {
		t.Error("Expected error for missing action")
	}
	if !strings.Contains(err.Error(), "action not found") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestTestCmd_BehaviorNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup monorepo structure
	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "scripts", "record-behaviors"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"test", "com.example.test", "--behavior", "missing-behavior"}
	_, _, err := invokeTestCmd(args...)

	if err == nil {
		t.Error("Expected error for missing behavior")
	}
	if !strings.Contains(err.Error(), "behavior test not found") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestTestCmd_SpaceNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup monorepo structure
	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "spaces"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"test", "com.example.test", "--space", "missing-space"}
	_, _, err := invokeTestCmd(args...)

	if err == nil {
		t.Error("Expected error for missing space")
	}
	if !strings.Contains(err.Error(), "space not found") {
		t.Errorf("Unexpected error: %v", err)
	}
}

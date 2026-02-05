package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// invokeTestCmd is a helper to run test command
func invokeTestCmd(args ...string) (string, string, error) {
	// We need to mock exec.Command to fundamentally test this without running actual vitest
	// But since we can't easily mock exec.Command in this structure without injection,
	// we will verify what we can: flag parsing and directory validation.
	// For actual execution, we can rely on the integration test we did manually.
	// However, we CAN test that runTest fails if directories are missing.

	// For now, let's just test basic validation failures which verify our path logic
	// before it hits exec.Command
	// Use RootCmd to simulate full CLI execution including subcommands
	cmd := RootCmd
	// Reset flags to defaults to avoid pollution in RootCmd or subcommands is harder,
	// but SetArgs on RootCmd should route correctly.
	// We do need to handle output capturing if we want to check it, but for now we check errors.
	// We do need to handle output capturing if we want to check it, but for now we check errors.
	if err := testCmd.Flags().Set("action", ""); err != nil {
		return "", "", err
	}
	if err := testCmd.Flags().Set("behavior", ""); err != nil {
		return "", "", err
	}
	if err := testCmd.Flags().Set("coverage", "false"); err != nil {
		return "", "", err
	}
	if err := testCmd.Flags().Set("json", "false"); err != nil {
		return "", "", err
	}
	cmd.SetArgs(args)

	// Capture output
	// Note: We're not fully capturing stdout/stderr because runTest writes directly to os.Stdout/Stderr
	// but we can catch errors
	err := cmd.Execute()
	return "", "", err
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

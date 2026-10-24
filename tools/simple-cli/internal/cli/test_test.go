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

// TestTestCmd_RustWithoutCargo covers the dispatch to the Rust runner from the
// one direction a test can drive without a toolchain on the machine: an action
// with src/main.rs, and a PATH with no cargo on it.
//
// Reaching this error is the assertion. It is raised only for a directory the
// detector named as Rust, and only before any suite is started, so seeing it
// proves both halves of the dispatch — that src/main.rs selects cargo rather
// than falling through to the vitest branch, and that a machine without a
// toolchain is told so once, rather than once per action in an unreadable
// "executable file not found" from each.
//
// The detector itself lives in internal/build, where the build path reads the
// same function, and is exercised in that package's own tests.
func TestTestCmd_RustWithoutCargo(t *testing.T) {
	tmpDir := t.TempDir()

	actionDir := filepath.Join(tmpDir, "apps", "com.example.test", "actions", "greet-user")
	if err := os.MkdirAll(filepath.Join(actionDir, "src"), 0755); err != nil {
		t.Fatalf("Failed to create action directory: %v", err)
	}
	// src/main.rs is the whole of what makes this action Rust: the detector
	// reads the source that is present, never a manifest.
	if err := os.WriteFile(filepath.Join(actionDir, "src", "main.rs"), []byte("fn main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to write main.rs: %v", err)
	}

	// An empty directory as the entire PATH, so that no cargo can be found
	// whatever the machine running this has installed.
	emptyBin := filepath.Join(tmpDir, "empty-bin")
	if err := os.MkdirAll(emptyBin, 0755); err != nil {
		t.Fatalf("Failed to create empty bin directory: %v", err)
	}
	t.Setenv("PATH", emptyBin)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"test", "com.example.test", "--action", "greet-user"}
	_, _, err := invokeTestCmd(args...)

	if err == nil {
		t.Fatal("Expected the run to be refused when cargo is missing")
	}
	if !strings.Contains(err.Error(), "cargo not found on PATH") {
		t.Errorf("Unexpected error: %v", err)
	}
}

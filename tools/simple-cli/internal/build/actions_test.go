package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindActions(t *testing.T) {
	tmpDir := t.TempDir()

	// Structure:
	// - action1/action.scl
	// - action2/package.json
	// - not_action/file.txt
	// - nested/action3/action.scl (should be ignored by current FindActions if it's not recursive)

	dirs := []string{
		"action1",
		"action2",
		"not_action",
		"nested/action3",
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	createFile(t, filepath.Join(tmpDir, "action1", "action.scl"))
	createFile(t, filepath.Join(tmpDir, "action2", "package.json"))
	createFile(t, filepath.Join(tmpDir, "not_action", "file.txt"))
	createFile(t, filepath.Join(tmpDir, "nested", "action3", "action.scl"))

	actions, err := FindActions(tmpDir)
	if err != nil {
		t.Fatalf("FindActions() error = %v", err)
	}

	expected := []string{
		filepath.Join(tmpDir, "action1"),
		filepath.Join(tmpDir, "action2"),
	}

	// FindActions returns absolute paths (or joined paths).
	// Order might vary, so use map or check containment.
	if len(actions) != len(expected) {
		t.Errorf("got %d actions, want %d", len(actions), len(expected))
	}

	m := make(map[string]bool)
	for _, a := range actions {
		m[a] = true
	}

	for _, e := range expected {
		if !m[e] {
			t.Errorf("missing action: %s", e)
		}
	}
}

func TestIsActionDir(t *testing.T) {
	tmpDir := t.TempDir()

	createFile(t, filepath.Join(tmpDir, "action.scl"))
	if !IsActionDir(tmpDir) {
		t.Error("IsActionDir() = false for dir with action.scl")
	}

	tmpDir2 := t.TempDir()
	createFile(t, filepath.Join(tmpDir2, "package.json"))
	if !IsActionDir(tmpDir2) {
		t.Error("IsActionDir() = false for dir with package.json")
	}

	tmpDir3 := t.TempDir()
	createFile(t, filepath.Join(tmpDir3, "other.txt"))
	if IsActionDir(tmpDir3) {
		t.Error("IsActionDir() = true for dir without action files")
	}
}

func createFile(t *testing.T, path string) {
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
}

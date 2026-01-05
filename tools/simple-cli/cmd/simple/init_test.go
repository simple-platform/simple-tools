package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateMonorepoStructure_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test-project")

	err := createMonorepoStructure(OSFileSystem{}, testPath, "test-project")
	if err != nil {
		t.Fatalf("createMonorepoStructure failed: %v", err)
	}

	// Verify directories exist
	dirs := []string{
		"apps",
		".simple/context",
	}
	for _, dir := range dirs {
		path := filepath.Join(testPath, dir)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}

	// Verify files exist
	files := []string{
		"AGENTS.md",
		"README.md",
		".simple/context/01-platform-overview.md",
		".simple/context/02-scl-grammar.md",
		".simple/context/03-data-layer-scl.md",
		".simple/context/04-expression-language.md",
		".simple/context/05-app-records-overview.md",
		".simple/context/06-metadata-configuration.md",
		".simple/context/07-actions-and-triggers.md",
		".simple/context/08-record-behaviors.md",
		".simple/context/09-custom-views.md",
		".simple/context/10-graphql-api.md",
		".simple/context/11-sdk-reference.md",
	}
	for _, file := range files {
		path := filepath.Join(testPath, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("file %s was not created", file)
		}
	}
}

func TestCreateMonorepoStructure_ExistingPath(t *testing.T) {
	tmpDir := t.TempDir()
	existingPath := filepath.Join(tmpDir, "existing")

	// Create the path first
	os.MkdirAll(existingPath, 0755)

	err := createMonorepoStructure(OSFileSystem{}, existingPath, "existing")
	if err == nil {
		t.Error("expected error for existing path, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestCreateMonorepoStructure_ReadmeContainsProjectName(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "my-awesome-project")

	err := createMonorepoStructure(OSFileSystem{}, testPath, "my-awesome-project")
	if err != nil {
		t.Fatalf("createMonorepoStructure failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(testPath, "README.md"))
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}

	if !strings.Contains(string(content), "my-awesome-project") {
		t.Error("README.md should contain project name")
	}
}

func TestCreateMonorepoStructure_NestedPath(t *testing.T) {
	tmpDir := t.TempDir()
	// test-nested/deep/project
	nestedPath := filepath.Join(tmpDir, "test-nested", "deep", "project")

	err := createMonorepoStructure(OSFileSystem{}, nestedPath, "project")
	if err != nil {
		t.Fatalf("createMonorepoStructure failed for nested path: %v", err)
	}

	if !pathExists(OSFileSystem{}, nestedPath) {
		t.Error("nested path was not created")
	}

	if !pathExists(OSFileSystem{}, filepath.Join(nestedPath, "README.md")) {
		t.Error("README.md not found in nested path")
	}
}

func TestCreateMonorepoStructure_PermissionDenied(t *testing.T) {
	tmpDir := t.TempDir()
	lockedDir := filepath.Join(tmpDir, "locked")

	// Create a directory and make it read-only/no-execute
	if err := os.Mkdir(lockedDir, 0555); err != nil {
		t.Fatalf("failed to create locked dir: %v", err)
	}
	// Note: In some CI environments/root user, 0555 might still allow writing.
	// But standard user permissions block modification.
	// We'll try to create a subfolder inside it.

	targetPath := filepath.Join(lockedDir, "new-project")

	// For this test to work reliably across OSes, we might need to actually
	// force a failure by passing an invalid path like "/" if running as root,
	// but for standard user testing, read-only parent is good.
	// Actually, chmod 0000 is safer for "permission denied" simulation.
	os.Chmod(lockedDir, 0555) // Valid reading, no writing

	// Attempt creation
	err := createMonorepoStructure(OSFileSystem{}, targetPath, "new-project")

	// If running as root (some docker containers), this might pass.
	// But in general getting an error is expected.
	if os.Geteuid() != 0 {
		if err == nil {
			t.Error("expected permission error, got nil")
		} else if !strings.Contains(strings.ToLower(err.Error()), "permission") && !strings.Contains(strings.ToLower(err.Error()), "denied") && !strings.Contains(strings.ToLower(err.Error()), "read-only") {
			// Just ensure it's an error of some kind related to creating the dir
			t.Logf("Got expected error: %v", err)
		}
	}
}

func TestPathExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Existing path
	if !pathExists(OSFileSystem{}, tmpDir) {
		t.Error("pathExists should return true for existing path")
	}

	// Non-existing path
	if pathExists(OSFileSystem{}, filepath.Join(tmpDir, "nonexistent")) {
		t.Error("pathExists should return false for non-existing path")
	}
}

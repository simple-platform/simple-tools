package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOSFileSystem_Stat(t *testing.T) {
	fs := OSFileSystem{}

	// Test existing file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	info, err := fs.Stat(testFile)
	if err != nil {
		t.Fatalf("Stat failed for existing file: %v", err)
	}
	if info.Name() != "test.txt" {
		t.Errorf("Expected name 'test.txt', got '%s'", info.Name())
	}

	// Test non-existing file
	_, err = fs.Stat(filepath.Join(tmpDir, "nonexistent.txt"))
	if err == nil {
		t.Error("Expected error for non-existing file")
	}
}

func TestOSFileSystem_MkdirAll(t *testing.T) {
	fs := OSFileSystem{}
	tmpDir := t.TempDir()

	// Test creating nested directories
	nestedPath := filepath.Join(tmpDir, "a", "b", "c")
	err := fs.MkdirAll(nestedPath, 0755)
	if err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	info, err := os.Stat(nestedPath)
	if err != nil {
		t.Fatalf("Created directory doesn't exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("Expected a directory")
	}
}

func TestOSFileSystem_WriteFile(t *testing.T) {
	fs := OSFileSystem{}
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "output.txt")
	content := []byte("hello world")

	err := fs.WriteFile(testFile, content, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", string(data))
	}
}

func TestOSFileSystem_ReadFile(t *testing.T) {
	fs := OSFileSystem{}
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "input.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	// Test reading existing file
	data, err := fs.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("Expected 'test content', got '%s'", string(data))
	}

	// Test reading non-existing file
	_, err = fs.ReadFile(filepath.Join(tmpDir, "nonexistent.txt"))
	if err == nil {
		t.Error("Expected error for non-existing file")
	}
}

func TestMockFileSystem_ReadFile(t *testing.T) {
	// Test with ReadFileErr
	mock := &MockFileSystem{ReadFileErr: os.ErrPermission}
	_, err := mock.ReadFile("any")
	if err != os.ErrPermission {
		t.Errorf("Expected permission error, got: %v", err)
	}

	// Test with Files map
	mock = &MockFileSystem{
		Files: map[string][]byte{
			"test.txt": []byte("mock content"),
		},
	}
	data, err := mock.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "mock content" {
		t.Errorf("Expected 'mock content', got '%s'", string(data))
	}

	// Test file not found
	_, err = mock.ReadFile("notfound.txt")
	if !os.IsNotExist(err) {
		t.Errorf("Expected not exist error, got: %v", err)
	}
}

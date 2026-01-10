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

func TestMockFileSystem_Stat(t *testing.T) {
	// Test error case
	mock := &MockFileSystem{StatErr: os.ErrPermission}
	_, err := mock.Stat("any")
	if err != os.ErrPermission {
		t.Errorf("Expected permission error, got: %v", err)
	}

	// Test default not exist
	mock = &MockFileSystem{}
	_, err = mock.Stat("any")
	if !os.IsNotExist(err) {
		t.Errorf("Expected not exist error, got: %v", err)
	}
}

func TestMockFileSystem_WriteFile(t *testing.T) {
	mock := &MockFileSystem{WriteFileErr: os.ErrPermission}
	err := mock.WriteFile("any", []byte{}, 0644)
	if err != os.ErrPermission {
		t.Errorf("Expected permission error, got: %v", err)
	}
}

func TestMockFileSystem_MkdirAll(t *testing.T) {
	mock := &MockFileSystem{MkdirAllErr: os.ErrPermission}
	err := mock.MkdirAll("any", 0755)
	if err != os.ErrPermission {
		t.Errorf("Expected permission error, got: %v", err)
	}
}

func TestMockFileInfo(t *testing.T) {
	m := &mockFileInfo{}
	if m.Name() != "mock" {
		t.Error("Name() incorrect")
	}
	if m.Size() != 0 {
		t.Error("Size() incorrect")
	}
	if m.Mode() != 0755 {
		t.Error("Mode() incorrect")
	}
	if !m.ModTime().IsZero() {
		t.Error("ModTime() incorrect")
	}
	if !m.IsDir() {
		t.Error("IsDir() incorrect")
	}
	if m.Sys() != nil {
		t.Error("Sys() incorrect")
	}
}

func TestMockTemplateFS(t *testing.T) {
	// Test ReadFile error
	mock := &MockTemplateFS{ReadFileErr: os.ErrPermission}
	_, err := mock.ReadFile("file")
	if err != os.ErrPermission {
		t.Errorf("Expected permission error, got: %v", err)
	}

	// Test ReadFile specific error
	mock = &MockTemplateFS{
		ReadErrors: map[string]error{"file.txt": os.ErrNotExist},
	}
	_, err = mock.ReadFile("file.txt")
	if !os.IsNotExist(err) {
		t.Errorf("Expected not exist error, got: %v", err)
	}

	// Test ReadFile success (default mock)
	_, err = mock.ReadFile("other.txt")
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}

	// Test ReadDir error
	mock = &MockTemplateFS{ReadDirErr: os.ErrPermission}
	_, err = mock.ReadDir("dir")
	if err != os.ErrPermission {
		t.Errorf("Expected permission error, got: %v", err)
	}

	// Test ReadDir success
	mock = &MockTemplateFS{}
	entries, err := mock.ReadDir("dir")
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "file.md" {
		t.Error("ReadDir entries incorrect")
	}

	// Test Open (not implemented)
	_, err = mock.Open("file")
	if err == nil {
		t.Error("Expected error for Open")
	}

	// Test mockDirEntry
	entry := entries[0]
	if entry.IsDir() {
		t.Error("IsDir() incorrect")
	}
	if entry.Type() != 0 {
		t.Error("Type() incorrect")
	}
	info, err := entry.Info()
	if info != nil || err != nil {
		t.Error("Info() incorrect")
	}
}

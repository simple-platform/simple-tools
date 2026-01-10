package fsx

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

// MockFileSystem for testing error paths
type MockFileSystem struct {
	StatErr      error
	MkdirAllErr  error
	WriteFileErr error
	ReadFileErr  error
	Files        map[string][]byte
}

func (m *MockFileSystem) Stat(name string) (fs.FileInfo, error) {
	if m.StatErr != nil {
		return nil, m.StatErr
	}
	// Return a dummy FileInfo that says "exists" unless StatErr is os.ErrNotExist
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return m.MkdirAllErr
}

func (m *MockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return m.WriteFileErr
}

func (m *MockFileSystem) ReadFile(name string) ([]byte, error) {
	if m.ReadFileErr != nil {
		return nil, m.ReadFileErr
	}
	if content, ok := m.Files[name]; ok {
		return content, nil
	}
	return nil, os.ErrNotExist
}

// mockFileInfo implements fs.FileInfo
type mockFileInfo struct{}

func (m *mockFileInfo) Name() string       { return "mock" }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() os.FileMode  { return 0755 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return true }
func (m *mockFileInfo) Sys() any           { return nil }

// MockTemplateFS for testing read errors
type MockTemplateFS struct {
	ReadFileErr error
	ReadDirErr  error
	Files       map[string][]byte
	ReadErrors  map[string]error
}

func (m *MockTemplateFS) ReadFile(name string) ([]byte, error) {
	if m.ReadFileErr != nil {
		return nil, m.ReadFileErr
	}
	if err, ok := m.ReadErrors[name]; ok {
		return nil, err
	}
	if content, ok := m.Files[name]; ok {
		return content, nil
	}
	return []byte("mock content"), nil
}

func (m *MockTemplateFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if m.ReadDirErr != nil {
		return nil, m.ReadDirErr
	}
	return []fs.DirEntry{
		&mockDirEntry{name: "file.md"},
	}, nil
}

func (m *MockTemplateFS) Open(name string) (fs.File, error) {
	return nil, errors.New("not implemented for mock")
}

type mockDirEntry struct {
	name string
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return false }
func (m *mockDirEntry) Type() fs.FileMode          { return 0 }
func (m *mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

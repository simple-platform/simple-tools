package fsx

import (
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"
)

// MockFileSystem for testing error paths
type MockFileSystem struct {
	mu           sync.RWMutex
	StatErr      error
	MkdirAllErr  error
	WriteFileErr error
	ReadFileErr  error
	Files        map[string][]byte
}

func (m *MockFileSystem) Stat(name string) (fs.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.StatErr != nil {
		return nil, m.StatErr
	}
	// Check if file exists in Files map
	if _, ok := m.Files[name]; ok {
		return &mockFileInfo{name: name, isDir: false}, nil
	}
	// Return os.ErrNotExist if file not found
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.MkdirAllErr
}

func (m *MockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.WriteFileErr != nil {
		return m.WriteFileErr
	}
	// Store the written file in the Files map
	if m.Files == nil {
		m.Files = make(map[string][]byte)
	}
	m.Files[name] = data
	return nil
}

func (m *MockFileSystem) ReadFile(name string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.ReadFileErr != nil {
		return nil, m.ReadFileErr
	}
	if content, ok := m.Files[name]; ok {
		return content, nil
	}
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return []os.DirEntry{}, nil
}

// mockFileInfo implements fs.FileInfo
type mockFileInfo struct {
	name  string
	isDir bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() os.FileMode  { return 0755 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
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

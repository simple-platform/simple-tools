package build

import "os"

// MockFileSystem is an in-memory filesystem for the tests that only need to know
// which files exist and what happened to them.
//
// Anything that runs the generator needs a real directory instead: the generator
// is a child process and reads the disk, not this.
type MockFileSystem struct {
	files map[string]string
}

func (m *MockFileSystem) ReadFile(name string) ([]byte, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}

	return []byte(content), nil
}

func (m *MockFileSystem) WriteFile(name string, data []byte, _ os.FileMode) error {
	m.files[name] = string(data)

	return nil
}

func (m *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	if _, ok := m.files[name]; !ok {
		return nil, os.ErrNotExist
	}

	return nil, nil // Simplified for testing
}

func (m *MockFileSystem) MkdirAll(_ string, _ os.FileMode) error {
	return nil
}

func (m *MockFileSystem) ReadDir(_ string) ([]os.DirEntry, error) {
	return nil, nil
}

func (m *MockFileSystem) Remove(name string) error {
	delete(m.files, name)

	return nil
}

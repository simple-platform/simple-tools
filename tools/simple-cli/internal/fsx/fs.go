package fsx

import (
	"io/fs"
	"os"
)

// Permissions constants
const (
	DirPerm  os.FileMode = 0755 // Standard directory permissions: rwxr-xr-x
	FilePerm os.FileMode = 0644 // Standard file permissions: rw-r--r--
)

// FileSystem abstraction for mocking
type FileSystem interface {
	Stat(name string) (fs.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]os.DirEntry, error)
	Remove(name string) error
}

// TemplateFS abstraction for mocking embedded files
type TemplateFS interface {
	fs.ReadFileFS
	fs.ReadDirFS
}

// OSFileSystem implements FileSystem using os package
type OSFileSystem struct{}

func (OSFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (OSFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (OSFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (OSFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

// Remove deletes a file, treating an already-absent one as success. Callers
// discard generated output that a failed regeneration has made untrue, and
// whether it was there to begin with is not something they need to know.
func (OSFileSystem) Remove(name string) error {
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

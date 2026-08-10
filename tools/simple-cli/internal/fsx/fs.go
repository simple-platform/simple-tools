package fsx

import (
	"io/fs"
	"os"
	"path/filepath"
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

// ResolveUpward finds the first directory in dir's parent chain that contains
// the given relative path, and answers with the full path to it.
//
// IT ASKS THE QUESTION NODE ASKS. A module importing a package does not look in
// one directory: it looks in `node_modules` beside itself, then in each
// directory above, up to the root of the filesystem. A workspace puts the
// packages at the root and the code in a member far below it, so a lookup that
// checks one or two fixed directories reports "missing" for something every
// import in that member resolves without difficulty.
//
// A directory named `node_modules` is skipped rather than asked, because Node
// skips it too — asking it would mean looking under
// `node_modules/node_modules`, which is not where anything is installed.
//
// The relative path is taken in parts so a caller can ask for a package
// (`"node_modules", name`) or for an executable a package installed
// (`"node_modules", ".bin", name`) through the same walk. Those two lookups
// differ only in what they are looking for, never in where they look, and
// writing the walk twice is how they drift apart.
func ResolveUpward(fsys FileSystem, dir string, rel ...string) (string, bool) {
	for {
		if filepath.Base(dir) != "node_modules" {
			candidate := filepath.Join(append([]string{dir}, rel...)...)
			if _, err := fsys.Stat(candidate); err == nil {
				return candidate, true
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}

		dir = parent
	}
}

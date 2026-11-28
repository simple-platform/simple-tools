// Package testsupport contains helpers shared by Simple CLI tests.
package testsupport

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// ModuleRoot finds the folder that holds the current Go module's go.mod file.
func ModuleRoot(t testing.TB) string {
	t.Helper()

	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve test working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("find Go module from %s: %v", dir, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from %s", dir)
		}
		dir = parent
	}
}

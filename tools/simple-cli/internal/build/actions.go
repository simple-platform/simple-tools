package build

import (
	"os"
	"path/filepath"
)

// FindActions searches for action directories within a root directory.
// It looks for directories containing 'action.scl' or 'package.json'.
func FindActions(rootDir string) ([]string, error) {
	var actions []string

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			path := filepath.Join(rootDir, entry.Name())
			if isActionDir(path) {
				actions = append(actions, path)
			}
		}
	}

	return actions, nil
}

func isActionDir(path string) bool {
	// Check for action.scl
	if _, err := os.Stat(filepath.Join(path, "action.scl")); err == nil {
		return true
	}
	// Check for package.json
	if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
		return true
	}
	return false
}

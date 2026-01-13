package build

import (
	"os"
	"path/filepath"
)

// FindActions searches for action directories within an app directory.
// It looks for directories containing 'action.scl' or 'package.json' inside the 'actions' subdirectory.
func FindActions(appDir string) ([]string, error) {
	var actions []string
	actionsDir := filepath.Join(appDir, "actions")

	if _, err := os.Stat(actionsDir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(actionsDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			path := filepath.Join(actionsDir, entry.Name())
			if IsActionDir(path) {
				actions = append(actions, path)
			}
		}
	}

	return actions, nil
}

func IsActionDir(path string) bool {
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

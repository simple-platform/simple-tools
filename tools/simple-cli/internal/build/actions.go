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
	// Check for action.scl - this is the definitive indicator of an action
	if _, err := os.Stat(filepath.Join(path, "action.scl")); err == nil {
		return true
	}

	// Check for package.json, but ensure it's not a Space
	if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
		// If it's a Space (has vite.config.ts or index.html), it's not an Action
		if _, err := os.Stat(filepath.Join(path, "vite.config.ts")); err == nil {
			return false
		}
		if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
			return false
		}
		return true
	}

	return false
}

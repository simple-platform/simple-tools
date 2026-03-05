package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var ExecCommandFunc = exec.Command

// FindSpaces searches for space directories within an app directory.
// It looks for directories containing 'package.json' inside the 'spaces' subdirectory.
func FindSpaces(appDir string) ([]string, error) {
	var spaces []string
	spacesDir := filepath.Join(appDir, "spaces")

	if _, err := os.Stat(spacesDir); os.IsNotExist(err) {
		return nil, nil // No spaces dir is fine
	}

	entries, err := os.ReadDir(spacesDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			path := filepath.Join(spacesDir, entry.Name())
			if IsSpaceDir(path) {
				spaces = append(spaces, path)
			}
		}
	}

	return spaces, nil
}

func IsSpaceDir(path string) bool {
	// A space directory must contain a package.json
	if _, err := os.Stat(filepath.Join(path, "package.json")); err != nil {
		return false
	}

	// It must also contain Vite-specific files to distinguish it from a JS action
	if _, err := os.Stat(filepath.Join(path, "vite.config.ts")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
		return true
	}

	return false
}

type SpaceBuildResult struct {
	SpaceName string
	Error     error
}

// BuildSpace executes the build process for a single space directory.
// It runs npm install and npm run build.
func (m *BuildManager) BuildSpace(ctx context.Context, spaceDir string, onProgress ProgressReporter) SpaceBuildResult {
	spaceName := filepath.Base(spaceDir)

	report := func(status string) {
		if onProgress != nil {
			onProgress(spaceName, status, false, nil)
		}
	}

	report("Installing dependencies...")
	if err := EnsureDependenciesFunc(spaceDir); err != nil {
		report("Failed")
		return SpaceBuildResult{SpaceName: spaceName, Error: fmt.Errorf("npm install failed: %w", err)}
	}

	report("Building UI...")

	// Default vite build puts output in dist/ directory.
	// We'll run `npm run build` which should be defined in package.json
	cmd := ExecCommandFunc("npm", "run", "build")
	cmd.Dir = spaceDir

	if !m.options.Verbose || onProgress != nil {
		// Output is suppressed if not in verbose mode OR if using progress UI,
		// but we'll collect it on error
		out, err := cmd.CombinedOutput()
		if err != nil {
			report("Failed")
			return SpaceBuildResult{
				SpaceName: spaceName,
				Error:     fmt.Errorf("build failed: %w\nOutput: %s", err, out),
			}
		}
	} else {
		// In verbose mode without progress UI, pipe output to standard outputs
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			report("Failed")
			return SpaceBuildResult{SpaceName: spaceName, Error: fmt.Errorf("build failed: %w", err)}
		}
	}

	report("Done")
	return SpaceBuildResult{SpaceName: spaceName, Error: nil}
}

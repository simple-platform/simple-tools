package build

import (
	"fmt"
	"os/exec"
)

func EnsureDependencies(dir string) error {
	// Check if package.json exists?
	// For now, just run npm install
	cmd := exec.Command("npm", "install")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm install failed: %s: %w", string(output), err)
	}
	return nil
}

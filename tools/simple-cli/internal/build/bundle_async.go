package build

import (
	"fmt"
	"os/exec"
)

// BundleAsync bundles the action for async execution using simple-sdk-build from @simpleplatform/sdk.
func BundleAsync(dir, entryPoint, outFile string) error {
	cmd := exec.Command("npx", "simple-sdk-build", entryPoint, outFile)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("simple-sdk-build failed: %s: %w", string(output), err)
	}
	return nil
}

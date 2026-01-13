package build

import (
	"fmt"
	"os/exec"
)

func BundleJS(dir, entryPoint, outFile string, minify bool, defines map[string]string) error {
	// Using esbuild CLI
	args := []string{
		entryPoint,
		"--bundle",
		"--platform=node",
		"--outfile=" + outFile,
	}

	if minify {
		args = append(args, "--minify")
	}

	for k, v := range defines {
		args = append(args, fmt.Sprintf("--define:%s=%s", k, v))
	}

	cmd := exec.Command("npx", append([]string{"esbuild"}, args...)...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("esbuild failed: %s: %w", string(output), err)
	}

	return nil
}

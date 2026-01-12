package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func BundleAction(dir string) (string, error) {
	entryPoint := filepath.Join(dir, "src", "index.ts")
	if !fileExists(entryPoint) {
		entryPoint = filepath.Join(dir, "index.ts")
	}

	outDir := filepath.Join(dir, "build")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}

	outFile := filepath.Join(outDir, "index.js")

	// Using esbuild CLI for now
	args := []string{
		entryPoint,
		"--bundle",
		"--platform=node",
		"--outfile=" + outFile,
	}

	cmd := exec.Command("npx", append([]string{"esbuild"}, args...)...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("esbuild failed: %s: %w", string(output), err)
	}

	return outFile, nil
}

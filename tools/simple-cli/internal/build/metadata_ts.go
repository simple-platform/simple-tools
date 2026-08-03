package build

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"simple-cli/internal/fsx"
)

//go:embed scripts/extract-ts-metadata.js
var extractScriptContent string

//go:embed scripts/extract_godoc.go
var extractGoDocContent string

// extractTypeScriptMetadata describes a TypeScript action from its own source
// by running the Node generator, which parses TypeScript with ts-morph and
// ts-json-schema-generator rather than having this package reimplement either.
//
// The extraction script is located at ~/.simple/scripts/extract-ts-metadata.js
//
// THE GENERATOR'S OUTPUT IS THE FILE. It writes action.json itself and what it
// wrote is left exactly as it wrote it — read back only far enough to prove it
// is there and parses.
//
// It used to be re-rendered through this package's own structs on the way past.
// Those structs model a SUBSET of JSON Schema, so a shape they cannot hold was
// silently flattened, dropped, or refused: a member typed as a union stopped
// the CLI dead on a source the platform's own build accepts, and everything
// that survived came back out with its keys reordered. Either way the file this
// tool left behind was not the file the other tool writes from the same source,
// which is the one thing a generated contract may not do.
//
// Returns error if:
//   - Node.js is not available
//   - Required npm packages are not installed
//   - Extraction script is not found
//   - Script execution fails, including refusing a malformed exposure statement
//   - No readable action.json was produced
func extractTypeScriptMetadata(fs fsx.FileSystem, actionDir string) error {
	// Check if Node.js is available
	if err := checkNodeJS(); err != nil {
		return fmt.Errorf("node.js is required for TypeScript metadata extraction: %w", err)
	}

	// Ensure required npm packages are installed
	if err := ensureNPMPackages(); err != nil {
		return fmt.Errorf("failed to install required npm packages: %w", err)
	}

	// Get the script path from ~/.simple/scripts
	scriptPath, err := getScriptPath()
	if err != nil {
		return fmt.Errorf("failed to locate extraction script: %w", err)
	}

	// Execute the Node.js script
	if err := executeScript(scriptPath, actionDir); err != nil {
		// A refusal is handed back as the generator wrote it. Wrapping it would
		// put this layer's account of how a child process ended in front of the
		// one sentence the author has to read to fix their source.
		var refusal *AnnotationRefusal
		if errors.As(err, &refusal) {
			return err
		}

		return fmt.Errorf("failed to execute extraction script: %w", err)
	}

	data, err := fs.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		return fmt.Errorf("failed to read generated action.json: %w", err)
	}

	if !json.Valid(data) {
		return fmt.Errorf("the generator produced an action.json that is not valid JSON")
	}

	return nil
}

// checkNodeJS verifies that Node.js is available on the system
func checkNodeJS() error {
	cmd := exec.Command("node", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("node.js is required for TypeScript metadata extraction: %w", err)
	}

	// Check Node.js version (should be >= 18 for ESM support)
	version := strings.TrimSpace(string(output))
	if !strings.HasPrefix(version, "v") {
		return fmt.Errorf("unexpected node version format: %s", version)
	}

	return nil
}

// ensureNPMPackages checks if required packages are installed and installs them if needed
func ensureNPMPackages() error {
	// Check if packages are already installed by trying to resolve them
	// We check in the workspace root (where pnpm workspace is configured)
	workspaceRoot, err := findWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("failed to find workspace root: %w", err)
	}

	// Check if packages exist in node_modules
	packagesToCheck := []string{
		"ts-json-schema-generator",
		"ts-morph",
	}

	allInstalled := true
	for _, pkg := range packagesToCheck {
		pkgPath := filepath.Join(workspaceRoot, "node_modules", pkg)
		if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
			allInstalled = false
			break
		}
	}

	// If packages are not installed, install them.
	// Output is captured rather than inherited: builds run under a progress UI
	// that repaints in place, and a concurrent write from a child process
	// corrupts the frame. The output is surfaced only if the install fails.
	if !allInstalled {
		cmd := exec.Command("pnpm", "add", "-w", "-D", "ts-json-schema-generator", "ts-morph")
		cmd.Dir = workspaceRoot

		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to install packages: %w\nOutput: %s", err, out)
		}
	}

	return nil
}

// findWorkspaceRoot finds the pnpm workspace root by looking for pnpm-workspace.yaml
func findWorkspaceRoot() (string, error) {
	// Start from current directory and walk up
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		// Check for pnpm-workspace.yaml
		workspaceFile := filepath.Join(dir, "pnpm-workspace.yaml")
		if _, err := os.Stat(workspaceFile); err == nil {
			return dir, nil
		}

		// Check for package.json with workspaces field (fallback)
		packageJSON := filepath.Join(dir, "package.json")
		if _, err := os.Stat(packageJSON); err == nil {
			// This might be the root, return it
			return dir, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", fmt.Errorf("workspace root not found (no pnpm-workspace.yaml or package.json)")
		}
		dir = parent
	}
}

// getScriptPath returns the path to the extraction script in ~/.simple/scripts
// If the script doesn't exist, it extracts the embedded script to that location
func getScriptPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	scriptsDir := filepath.Join(homeDir, ".simple", "scripts")
	scriptPath := filepath.Join(scriptsDir, "extract-ts-metadata.js")

	// Create ~/.simple/scripts directory if it doesn't exist
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create scripts directory: %w", err)
	}

	if err := writeIfChanged(scriptPath, []byte(extractScriptContent)); err != nil {
		return "", fmt.Errorf("failed to write extraction script: %w", err)
	}

	goDocPath := filepath.Join(scriptsDir, "extract_godoc.go")
	if err := writeIfChanged(goDocPath, []byte(extractGoDocContent)); err != nil {
		return "", fmt.Errorf("failed to write Go metadata helper: %w", err)
	}

	return scriptPath, nil
}

func writeIfChanged(path string, content []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == string(content) {
		return nil
	}

	return os.WriteFile(path, content, 0644)
}

// executeScript runs the Node.js extraction script
func executeScript(scriptPath, actionDir string) error {
	// Find workspace root to run the script from there (so Node.js can find packages)
	workspaceRoot, err := findWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("failed to find workspace root: %w", err)
	}

	// Copy the script to workspace root temporarily so Node.js ESM can find node_modules
	// ESM module resolution looks for node_modules relative to the script location
	tempScriptPath := filepath.Join(workspaceRoot, ".extract-ts-metadata.tmp.js")
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("failed to read extraction script: %w", err)
	}

	if err := os.WriteFile(tempScriptPath, scriptContent, 0644); err != nil {
		return fmt.Errorf("failed to create temporary script: %w", err)
	}
	defer func() {
		_ = os.Remove(tempScriptPath) // Clean up after execution
	}()

	// Captured, not inherited: this runs once per action while the progress UI
	// is repainting, and interleaved child output corrupts the frame.
	cmd := exec.Command("node", tempScriptPath, actionDir)
	cmd.Dir = workspaceRoot

	if out, err := cmd.CombinedOutput(); err != nil {
		// A refusal is the generator working, and it is the one failure whose
		// text an author is meant to act on — so it is handed back as the
		// sentence the generator wrote rather than buried under this layer's
		// account of how the process ended.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == AnnotationRefusalExitCode {
			return &AnnotationRefusal{Refusal: strings.TrimSpace(string(out))}
		}

		return fmt.Errorf("script execution failed: %w\nOutput: %s", err, out)
	}

	return nil
}

package build

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"simple-cli/internal/fsx"
)

//go:embed scripts/extract-ts-metadata.js
var extractScriptContent string

// extractTypeScriptMetadata extracts metadata from a TypeScript action using Node.js.
// It uses ts-morph and ts-json-schema-generator to parse TypeScript and generate JSON Schema.
//
// This approach delegates TypeScript parsing to battle-tested Node.js libraries rather than
// attempting to parse TypeScript in Go, which is complex and error-prone.
//
// The extraction script is located at ~/.simple/scripts/extract-ts-metadata.js
//
// Returns error if:
//   - Node.js is not available
//   - Required npm packages are not installed
//   - Extraction script is not found
//   - Script execution fails
//   - Generated action.json cannot be read
func extractTypeScriptMetadata(fs fsx.FileSystem, actionDir string) (*ActionMetadata, error) {
	// Check if Node.js is available
	if err := checkNodeJS(); err != nil {
		return nil, fmt.Errorf("node.js is required for TypeScript metadata extraction: %w", err)
	}

	// Ensure required npm packages are installed
	if err := ensureNPMPackages(); err != nil {
		return nil, fmt.Errorf("failed to install required npm packages: %w", err)
	}

	// Get the script path from ~/.simple/scripts
	scriptPath, err := getScriptPath()
	if err != nil {
		return nil, fmt.Errorf("failed to locate extraction script: %w", err)
	}

	// Execute the Node.js script
	if err := executeScript(scriptPath, actionDir); err != nil {
		return nil, fmt.Errorf("failed to execute extraction script: %w", err)
	}

	// The script writes action.json directly, so we just need to read it back
	// to return the metadata (even though we don't use it in the current flow)
	actionJSONPath := filepath.Join(actionDir, "action.json")
	data, err := fs.ReadFile(actionJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated action.json: %w", err)
	}

	// Parse the JSON to return ActionMetadata
	// (This is mainly for consistency with the function signature)
	var metadata ActionMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse generated action.json: %w", err)
	}

	return &metadata, nil
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

	// If packages are not installed, install them
	if !allInstalled {
		fmt.Println("Installing required npm packages (ts-json-schema-generator, ts-morph)...")
		cmd := exec.Command("pnpm", "add", "-w", "-D", "ts-json-schema-generator", "ts-morph")
		cmd.Dir = workspaceRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install packages: %w", err)
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

	// Check if script already exists
	if _, err := os.Stat(scriptPath); err == nil {
		return scriptPath, nil
	}

	// Script doesn't exist, extract it from embedded content
	// Create ~/.simple/scripts directory if it doesn't exist
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create scripts directory: %w", err)
	}

	// Write the embedded script content
	if err := os.WriteFile(scriptPath, []byte(extractScriptContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write extraction script: %w", err)
	}

	return scriptPath, nil
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

	cmd := exec.Command("node", tempScriptPath, actionDir)
	cmd.Dir = workspaceRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("script execution failed: %w", err)
	}

	return nil
}

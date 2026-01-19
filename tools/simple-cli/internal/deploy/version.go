// Package deploy provides deployment functionality for Simple Platform applications.
package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FileSystem abstracts file operations for testing.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Stat(path string) (os.FileInfo, error)
}

// OSFileSystem implements FileSystem using the os package.
type OSFileSystem struct{}

func (OSFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (OSFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (OSFileSystem) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// SCLParser abstracts the scl-parser CLI for testing.
type SCLParser interface {
	Parse(path string) ([]SCLBlock, error)
}

// DefaultSCLParser uses the scl-parser CLI binary.
type DefaultSCLParser struct {
	ParserPath string
}

// SCLBlock represents a block in the SCL AST.
type SCLBlock struct {
	Type     string     `json:"type"`
	Key      string     `json:"key"`
	Name     string     `json:"name"`
	Value    any        `json:"value"`
	Children []SCLBlock `json:"children"`
}

// Parse executes scl-parser CLI and returns the AST.
func (p *DefaultSCLParser) Parse(path string) ([]SCLBlock, error) {
	cmd := exec.Command(p.ParserPath, path)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("scl-parser failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("scl-parser execution failed: %w", err)
	}

	var blocks []SCLBlock
	if err := json.Unmarshal(output, &blocks); err != nil {
		return nil, fmt.Errorf("failed to parse scl-parser output: %w", err)
	}

	return blocks, nil
}

// VersionManager handles version bumping and app.scl updates.
type VersionManager struct {
	FS         FileSystem
	Parser     SCLParser
	ParserPath string
}

// NewVersionManager creates a VersionManager with the default filesystem.
func NewVersionManager(parserPath string) *VersionManager {
	return &VersionManager{
		FS:         OSFileSystem{},
		Parser:     &DefaultSCLParser{ParserPath: parserPath},
		ParserPath: parserPath,
	}
}

// AppSCL represents the parsed app.scl structure.
type AppSCL struct {
	ID      string
	Version string
}

// ParseAppSCL parses app.scl using scl-parser and extracts id and version.
func (vm *VersionManager) ParseAppSCL(appPath string) (*AppSCL, error) {
	sclPath := filepath.Join(appPath, "app.scl")

	blocks, err := vm.Parser.Parse(sclPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse app.scl: %w", err)
	}

	app := &AppSCL{}

	// Extract properties via helper
	app.ID, app.Version = extractFromBlocks(blocks)

	if app.ID == "" {
		return nil, fmt.Errorf("id not found in app.scl")
	}
	if app.Version == "" {
		return nil, fmt.Errorf("version not found in app.scl")
	}

	return app, nil
}

// extractFromBlocks handles the scl-parser's actual output format
func extractFromBlocks(blocks []SCLBlock) (id, version string) {
	for _, block := range blocks {
		// Handle KV properties
		if block.Type == "kv" {
			switch block.Key {
			case "id":
				if s, ok := block.Value.(string); ok {
					id = s
				}
			case "version":
				if s, ok := block.Value.(string); ok {
					version = s
				}
			}
		}
	}
	return
}

// BumpVersion calculates the new version and updates app.scl.
// Returns the new version string.
func (vm *VersionManager) BumpVersion(appPath, env, bumpType string) (string, error) {
	appSCLPath := filepath.Join(appPath, "app.scl")

	// Read current content for modification
	content, err := vm.FS.ReadFile(appSCLPath)
	if err != nil {
		return "", fmt.Errorf("failed to read app.scl: %w", err)
	}

	// Parse to get current version
	app, err := vm.ParseAppSCL(appPath)
	if err != nil {
		return "", err
	}

	newVersion, err := ComputeNewVersion(app.Version, env, bumpType)
	if err != nil {
		return "", err
	}

	// Update app.scl with new version - use minimal string replacement
	// This is the ONLY place we use string manipulation (not regex)
	newContent := replaceVersionInContent(string(content), app.Version, newVersion)
	if err := vm.FS.WriteFile(appSCLPath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write app.scl: %w", err)
	}

	return newVersion, nil
}

// replaceVersionInContent replaces the old version with new version in content.
// Uses simple string replacement - minimal text manipulation.
func replaceVersionInContent(content, oldVersion, newVersion string) string {
	// Replace quoted versions: "1.0.0" -> "1.0.1"
	content = strings.ReplaceAll(content, `"`+oldVersion+`"`, `"`+newVersion+`"`)
	// Replace single-quoted versions: '1.0.0' -> '1.0.1'
	content = strings.ReplaceAll(content, `'`+oldVersion+`'`, `'`+newVersion+`'`)
	// Replace unquoted versions (if on a line with "version"): version 1.0.0 -> version 1.0.1
	// This is safe because version strings are unique
	content = strings.ReplaceAll(content, " "+oldVersion+"\n", " "+newVersion+"\n")
	content = strings.ReplaceAll(content, " "+oldVersion+"\r\n", " "+newVersion+"\r\n")
	// Handle case where version is at end of file without newline
	if strings.HasSuffix(content, " "+oldVersion) {
		content = strings.TrimSuffix(content, " "+oldVersion) + " " + newVersion
	}
	return content
}

// ComputeNewVersion implements the version state machine.
//
// Version Flow:
//   - Prod → Non-prod (requires --bump): 1.0.0 + patch → 1.0.1-dev.1
//   - Non-prod → Same env: 1.0.1-dev.1 → 1.0.1-dev.2
//   - Non-prod → Different env: 1.0.1-dev.5 → 1.0.1-staging.1
//   - Non-prod → Prod: 1.0.1-staging.3 → 1.0.1
//   - Prod → Prod (requires --bump): 1.0.0 + patch → 1.0.1
func ComputeNewVersion(current, env, bumpType string) (string, error) {
	major, minor, patch, prerelease := ParseVersion(current)

	isProd := env == "prod"
	hasPrerelease := prerelease != ""

	if isProd {
		// Prod: strip prerelease
		if hasPrerelease {
			// Already has version from dev/staging, just strip prerelease
			return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
		}
		// No prerelease means we need --bump
		if bumpType == "" {
			return "", fmt.Errorf("--bump required for prod deployment from prod version")
		}
		return bumpMajorMinorPatch(major, minor, patch, bumpType)
	}

	// Non-prod environments
	currentEnv := extractEnvFromPrerelease(prerelease)
	currentCounter := extractCounter(prerelease)

	if !hasPrerelease {
		// Starting from a prod version, need --bump
		if bumpType == "" {
			return "", fmt.Errorf("--bump required for first deploy after prod release")
		}
		// Bump and add prerelease
		base, err := bumpMajorMinorPatch(major, minor, patch, bumpType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s-%s.1", base, env), nil
	}

	if currentEnv == env {
		// Same environment, increment counter
		return fmt.Sprintf("%d.%d.%d-%s.%d", major, minor, patch, env, currentCounter+1), nil
	}

	// Different environment, reset counter
	return fmt.Sprintf("%d.%d.%d-%s.1", major, minor, patch, env), nil
}

// bumpMajorMinorPatch applies the bump type to the version.
func bumpMajorMinorPatch(major, minor, patch int, bumpType string) (string, error) {
	switch bumpType {
	case "major":
		return fmt.Sprintf("%d.0.0", major+1), nil
	case "minor":
		return fmt.Sprintf("%d.%d.0", major, minor+1), nil
	case "patch":
		return fmt.Sprintf("%d.%d.%d", major, minor, patch+1), nil
	default:
		return "", fmt.Errorf("invalid bump type: %s (must be major|minor|patch)", bumpType)
	}
}

// ParseVersion extracts version components from a version string.
// Supports formats: "1.2.3" and "1.2.3-prerelease.5"
func ParseVersion(v string) (major, minor, patch int, prerelease string) {
	// Split on first hyphen to separate base version from prerelease
	parts := strings.SplitN(v, "-", 2)
	if len(parts) == 2 {
		prerelease = parts[1]
	}

	// Parse major.minor.patch
	mmp := strings.Split(parts[0], ".")
	if len(mmp) >= 3 {
		major, _ = strconv.Atoi(mmp[0])
		minor, _ = strconv.Atoi(mmp[1])
		patch, _ = strconv.Atoi(mmp[2])
	}
	return
}

// extractEnvFromPrerelease extracts the environment name from prerelease.
// Example: "dev.5" → "dev"
func extractEnvFromPrerelease(pr string) string {
	if pr == "" {
		return ""
	}
	parts := strings.Split(pr, ".")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}

// extractCounter extracts the numeric counter from prerelease.
// Example: "dev.5" → 5
func extractCounter(pr string) int {
	if pr == "" {
		return 0
	}
	parts := strings.Split(pr, ".")
	if len(parts) >= 2 {
		n, _ := strconv.Atoi(parts[len(parts)-1])
		return n
	}
	return 0
}

// ExtractAppID extracts the app ID from app.scl using scl-parser.
func ExtractAppID(parserPath, appPath string) (string, error) {
	vm := NewVersionManager(parserPath)
	app, err := vm.ParseAppSCL(appPath)
	if err != nil {
		return "", err
	}
	return app.ID, nil
}

// ExtractVersion extracts the version from app.scl using scl-parser.
func ExtractVersion(parserPath, appPath string) (string, error) {
	vm := NewVersionManager(parserPath)
	app, err := vm.ParseAppSCL(appPath)
	if err != nil {
		return "", err
	}
	return app.Version, nil
}

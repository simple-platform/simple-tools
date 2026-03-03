package scaffold

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"simple-cli/internal/fsx"
)

// SpaceConfig holds configuration for creating a new space.
type SpaceConfig struct {
	AppID       string
	SpaceName   string
	DisplayName string
	Description string
}

// CreateSpaceStructure scaffolds a new space inside an app's spaces/ directory.
//
// It creates:
//   - apps/<appID>/spaces/<spaceName>/
//   - apps/<appID>/spaces/<spaceName>/package.json
//   - apps/<appID>/spaces/<spaceName>/vite.config.ts
//   - apps/<appID>/spaces/<spaceName>/vitest.config.ts
//   - apps/<appID>/spaces/<spaceName>/tsconfig.json
//   - apps/<appID>/spaces/<spaceName>/index.html
//   - apps/<appID>/spaces/<spaceName>/src/main.tsx
//   - apps/<appID>/spaces/<spaceName>/src/App.tsx
//   - apps/<appID>/spaces/<spaceName>/tests/App.test.tsx
//   - apps/<appID>/records/10_spaces.scl (appended or created)
func CreateSpaceStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string, cfg SpaceConfig) error {
	appPath := filepath.Join(rootPath, "apps", cfg.AppID)

	// Validate: app must exist
	if !PathExists(fsys, appPath) {
		return fmt.Errorf("app does not exist: %s", cfg.AppID)
	}

	// Check for duplicate space in SCL
	recordsPath := filepath.Join(appPath, "records")
	spacesScl := filepath.Join(recordsPath, "10_spaces.scl")
	spaceNameScl := strings.ReplaceAll(cfg.SpaceName, "-", "_")

	if PathExists(fsys, spacesScl) {
		exists, err := checkSCLEntityMatchType(spacesScl, "set", "dev_simple_system.space", spaceNameScl)
		if err != nil {
			return fmt.Errorf("failed to check space existence: %w", err)
		}
		if exists {
			return fmt.Errorf("space already exists in records: %s", spaceNameScl)
		}
	}

	spacePath := filepath.Join(appPath, "spaces", cfg.SpaceName)

	// Validate: space directory must not exist
	if PathExists(fsys, spacePath) {
		return fmt.Errorf("space directory already exists: %s", cfg.SpaceName)
	}

	// Create directories
	dirs := []string{
		spacePath,
		filepath.Join(spacePath, "src"),
		filepath.Join(spacePath, "tests"),
	}

	for _, dir := range dirs {
		if err := fsys.MkdirAll(dir, fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Template data
	data := map[string]string{
		"AppID":        cfg.AppID,
		"SpaceName":    cfg.SpaceName,
		"SpaceNameScl": spaceNameScl,
		"DisplayName":  cfg.DisplayName,
		"Description":  cfg.Description,
	}

	// Render space files
	spaceFiles := []struct {
		src string
		dst string
	}{
		{"templates/space/package.json", filepath.Join(spacePath, "package.json")},
		{"templates/space/vite.config.ts", filepath.Join(spacePath, "vite.config.ts")},
		{"templates/space/vitest.config.ts", filepath.Join(spacePath, "vitest.config.ts")},
		{"templates/space/tsconfig.json", filepath.Join(spacePath, "tsconfig.json")},
		{"templates/space/index.html", filepath.Join(spacePath, "index.html")},
		{"templates/space/src/main.tsx", filepath.Join(spacePath, "src", "main.tsx")},
		{"templates/space/src/App.tsx", filepath.Join(spacePath, "src", "App.tsx")},
		{"templates/space/tests/App.test.tsx", filepath.Join(spacePath, "tests", "App.test.tsx")},
	}

	for _, f := range spaceFiles {
		if err := renderTemplate(fsys, tplFS, f.src, f.dst, data); err != nil {
			return err
		}
	}

	// Create or append to records/10_spaces.scl
	if err := fsys.MkdirAll(recordsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create records directory: %w", err)
	}

	if err := appendSpaceRecord(fsys, tplFS, spacesScl, data); err != nil {
		return err
	}

	return nil
}

// appendSpaceRecord appends a space record to the 10_spaces.scl file.
func appendSpaceRecord(fsys fsx.FileSystem, tplFS fsx.TemplateFS, dst string, data map[string]string) error {
	// Read the template
	content, err := tplFS.ReadFile("templates/space/10_spaces.scl")
	if err != nil {
		return fmt.Errorf("failed to read space template: %w", err)
	}

	tmpl, err := template.New("space").Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse space template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute space template: %w", err)
	}

	// Check if file exists and append, otherwise create
	var existingContent []byte
	if PathExists(fsys, dst) {
		existingContent, err = fsys.ReadFile(dst)
		if err != nil {
			return fmt.Errorf("failed to read existing %s: %w", dst, err)
		}
	}

	var finalContent []byte
	if len(existingContent) > 0 {
		// Append with newline separator
		finalContent = append(existingContent, '\n')
		finalContent = append(finalContent, buf.Bytes()...)
	} else {
		finalContent = buf.Bytes()
	}

	if err := fsys.WriteFile(dst, finalContent, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

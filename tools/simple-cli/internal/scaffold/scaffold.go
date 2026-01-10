// Package scaffold provides scaffolding utilities for Simple Platform monorepos.
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"simple-cli/internal/fsx"
	"text/template"
)

//go:embed templates
var TemplatesFS embed.FS

// CreateMonorepoStructure creates all directories and files for a new monorepo.
//
// It creates:
//   - apps/ directory (empty)
//   - .simple/context/ directory with documentation files
//   - AGENTS.md at root
//   - README.md at root (templated with project name)
//
// Returns an error if the target path already exists.
func CreateMonorepoStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath, projectName string) error {
	// Validate: path must not exist
	if PathExists(fsys, rootPath) {
		return fmt.Errorf("path already exists: %s", rootPath)
	}

	// Create directories
	if err := createDirectories(fsys, rootPath); err != nil {
		return err
	}

	// Copy context documentation
	if err := copyContextDocs(fsys, tplFS, rootPath); err != nil {
		return err
	}

	// Copy AGENTS.md
	if err := copyTemplate(fsys, tplFS, "templates/AGENTS.md", filepath.Join(rootPath, "AGENTS.md")); err != nil {
		return err
	}

	// Generate README.md with project name
	data := map[string]string{"ProjectName": projectName}
	if err := renderTemplate(fsys, tplFS, "templates/README.md", filepath.Join(rootPath, "README.md"), data); err != nil {
		return err
	}

	return nil
}

// CreateAppStructure scaffolds a new application inside the apps/ directory.
//
// It creates:
//   - apps/<appID>/
//   - apps/<appID>/app.scl
//   - apps/<appID>/tables.scl
//   - apps/<appID>/actions/
//   - apps/<appID>/records/
//   - apps/<appID>/scripts/
func CreateAppStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath, appID, displayName, description string) error {
	appPath := filepath.Join(rootPath, "apps", appID)

	// Validate: path must not exist
	if PathExists(fsys, appPath) {
		return fmt.Errorf("app already exists: %s", appID)
	}

	// Create app directory
	if err := fsys.MkdirAll(appPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Create standard subdirectories
	subDirs := []string{"actions", "records", "scripts"}
	for _, dir := range subDirs {
		if err := fsys.MkdirAll(filepath.Join(appPath, dir), fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", dir, err)
		}
	}

	// Render app.scl
	data := map[string]string{
		"AppID":       appID,
		"DisplayName": displayName,
		"Description": description,
	}
	if err := renderTemplate(fsys, tplFS, "templates/app/app.scl", filepath.Join(appPath, "app.scl"), data); err != nil {
		return err
	}

	// Copy tables.scl
	if err := copyTemplate(fsys, tplFS, "templates/app/tables.scl", filepath.Join(appPath, "tables.scl")); err != nil {
		return err
	}

	return nil
}

// ActionConfig holds the configuration for creating a new action.
type ActionConfig struct {
	AppID        string
	ActionName   string
	DisplayName  string
	Description  string
	Scope        string
	ExecutionEnv string
}

// CreateActionStructure scaffolds a new action inside an app's actions/ directory.
//
// It creates:
//   - apps/<appID>/actions/<actionName>/
//   - apps/<appID>/actions/<actionName>/package.json
//   - apps/<appID>/actions/<actionName>/index.ts
//   - apps/<appID>/actions/<actionName>/tsconfig.json
//   - apps/<appID>/actions/<actionName>/vitest.config.ts
//   - apps/<appID>/actions/<actionName>/tests/helpers.ts
//   - apps/<appID>/actions/<actionName>/tests/index.test.ts
//   - apps/<appID>/records/10_actions.scl (appended or created)
func CreateActionStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string, cfg ActionConfig) error {
	appPath := filepath.Join(rootPath, "apps", cfg.AppID)

	// Validate: app must exist
	if !PathExists(fsys, appPath) {
		return fmt.Errorf("app does not exist: %s", cfg.AppID)
	}

	actionPath := filepath.Join(appPath, "actions", cfg.ActionName)

	// Validate: action must not exist
	if PathExists(fsys, actionPath) {
		return fmt.Errorf("action already exists: %s", cfg.ActionName)
	}

	// Create action directory and tests subdirectory
	if err := fsys.MkdirAll(actionPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create action directory: %w", err)
	}

	testsPath := filepath.Join(actionPath, "tests")
	if err := fsys.MkdirAll(testsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create tests directory: %w", err)
	}

	// Template data
	data := map[string]string{
		"ActionName":   cfg.ActionName,
		"DisplayName":  cfg.DisplayName,
		"Description":  cfg.Description,
		"Scope":        cfg.Scope,
		"ExecutionEnv": cfg.ExecutionEnv,
	}

	// Render action files
	actionFiles := []struct {
		src string
		dst string
	}{
		{"templates/action/package.json", filepath.Join(actionPath, "package.json")},
		{"templates/action/index.ts", filepath.Join(actionPath, "index.ts")},
		{"templates/action/tsconfig.json", filepath.Join(actionPath, "tsconfig.json")},
		{"templates/action/vitest.config.ts", filepath.Join(actionPath, "vitest.config.ts")},
		{"templates/action/tests/helpers.ts", filepath.Join(testsPath, "helpers.ts")},
		{"templates/action/tests/index.test.ts", filepath.Join(testsPath, "index.test.ts")},
	}

	for _, f := range actionFiles {
		if err := renderTemplate(fsys, tplFS, f.src, f.dst, data); err != nil {
			return err
		}
	}

	// Create or append to records/10_actions.scl
	recordsPath := filepath.Join(appPath, "records")
	if err := fsys.MkdirAll(recordsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create records directory: %w", err)
	}

	actionsScl := filepath.Join(recordsPath, "10_actions.scl")
	if err := appendActionRecord(fsys, tplFS, actionsScl, data); err != nil {
		return err
	}

	return nil
}

// appendActionRecord appends an action record to the 10_actions.scl file.
func appendActionRecord(fsys fsx.FileSystem, tplFS fsx.TemplateFS, dst string, data map[string]string) error {
	// Read the template
	content, err := tplFS.ReadFile("templates/action/10_actions.scl")
	if err != nil {
		return fmt.Errorf("failed to read action template: %w", err)
	}

	tmpl, err := template.New("action").Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse action template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute action template: %w", err)
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

// PathExists checks if a path exists on the filesystem.
func PathExists(fsys fsx.FileSystem, path string) bool {
	_, err := fsys.Stat(path)
	return !os.IsNotExist(err)
}

// createDirectories creates the required directory structure.
func createDirectories(fsys fsx.FileSystem, rootPath string) error {
	dirs := []string{
		"apps",
		".simple/context",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(rootPath, dir)
		if err := fsys.MkdirAll(fullPath, fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// copyContextDocs copies all files from templates/context/ to .simple/context/.
func copyContextDocs(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string) error {
	srcDir := "templates/context"
	dstDir := filepath.Join(rootPath, ".simple/context")

	entries, err := tplFS.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read templates/context: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue // Skip directories
		}

		srcPath := srcDir + "/" + entry.Name()
		dstPath := filepath.Join(dstDir, entry.Name())

		if err := copyTemplate(fsys, tplFS, srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// copyTemplate copies a file from the embedded filesystem to disk.
func copyTemplate(fsys fsx.FileSystem, tplFS fsx.TemplateFS, src, dst string) error {
	content, err := tplFS.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", src, err)
	}

	if err := fsys.WriteFile(dst, content, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// renderTemplate renders a Go template with data and writes to disk.
func renderTemplate(fsys fsx.FileSystem, tplFS fsx.TemplateFS, src, dst string, data map[string]string) error {
	content, err := tplFS.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", src, err)
	}

	tmpl, err := template.New(filepath.Base(src)).Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", src, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template %s: %w", src, err)
	}

	if err := fsys.WriteFile(dst, buf.Bytes(), fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

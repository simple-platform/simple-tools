// Package main provides scaffolding utilities for Simple Platform monorepos.
package main

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed templates
var templatesFS embed.FS

// createMonorepoStructure creates all directories and files for a new monorepo.
//
// It creates:
//   - apps/ directory (empty)
//   - .simple/context/ directory with documentation files
//   - AGENTS.md at root
//   - README.md at root (templated with project name)
//
// Returns an error if the target path already exists.
// createMonorepoStructure creates all directories and files for a new monorepo.
//
// It creates:
//   - apps/ directory (empty)
//   - .simple/context/ directory with documentation files
//   - AGENTS.md at root
//   - README.md at root (templated with project name)
//
// Returns an error if the target path already exists.
func createMonorepoStructure(fsys FileSystem, rootPath, projectName string) error {
	// Validate: path must not exist
	if pathExists(fsys, rootPath) {
		return fmt.Errorf("path already exists: %s", rootPath)
	}

	// Create directories
	if err := createDirectories(fsys, rootPath); err != nil {
		return err
	}

	// Copy context documentation
	if err := copyContextDocs(fsys, rootPath); err != nil {
		return err
	}

	// Copy AGENTS.md
	if err := copyTemplate(fsys, "templates/AGENTS.md", filepath.Join(rootPath, "AGENTS.md")); err != nil {
		return err
	}

	// Generate README.md with project name
	data := map[string]string{"ProjectName": projectName}
	if err := renderTemplate(fsys, "templates/README.md", filepath.Join(rootPath, "README.md"), data); err != nil {
		return err
	}

	return nil
}

// pathExists checks if a path exists on the filesystem.
func pathExists(fsys FileSystem, path string) bool {
	_, err := fsys.Stat(path)
	return !os.IsNotExist(err)
}

// createDirectories creates the required directory structure.
func createDirectories(fsys FileSystem, rootPath string) error {
	dirs := []string{
		"apps",
		".simple/context",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(rootPath, dir)
		if err := fsys.MkdirAll(fullPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// copyContextDocs copies all files from templates/context/ to .simple/context/.
func copyContextDocs(fsys FileSystem, rootPath string) error {
	srcDir := "templates/context"
	dstDir := filepath.Join(rootPath, ".simple/context")

	entries, err := templatesFS.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read templates/context: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue // Skip directories
		}

		srcPath := srcDir + "/" + entry.Name()
		dstPath := filepath.Join(dstDir, entry.Name())

		if err := copyTemplate(fsys, srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// copyTemplate copies a file from the embedded filesystem to disk.
func copyTemplate(fsys FileSystem, src, dst string) error {
	content, err := templatesFS.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", src, err)
	}

	if err := fsys.WriteFile(dst, content, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// renderTemplate renders a Go template with data and writes to disk.
func renderTemplate(fsys FileSystem, src, dst string, data map[string]string) error {
	content, err := templatesFS.ReadFile(src)
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

	if err := fsys.WriteFile(dst, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

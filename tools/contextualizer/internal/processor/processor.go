// Package processor handles the scanning, reading, and filtering of files
// to generate the context string.
package processor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"contextualizer/internal/config"

	"github.com/bmatcuk/doublestar/v4"
)

// Processor encapsulates the state needed to process a project directory.
type Processor struct {
	Config      *config.Config // Configuration including ignore patterns
	ProjectRoot string         // Absolute path to the project root
}

// New creates a new Processor instance.
func New(cfg *config.Config, root string) *Processor {
	return &Processor{
		Config:      cfg,
		ProjectRoot: root,
	}
}

// ProcessDirectory walks the specified directory recursively and returns the combined content of all valid files.
// It respects ignore patterns and skips binary files or files exceeding size limits.
func (p *Processor) ProcessDirectory(dirPath string) (string, error) {
	var sb strings.Builder

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(p.ProjectRoot, path)
		if err != nil {
			return err
		}

		// Normalize separators to slashes for consistent matching logic across OSs
		relPath = filepath.ToSlash(relPath)

		// Check if the path should be ignored
		if p.shouldIgnore(relPath, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		// It's a file, check metadata first
		info, err := d.Info()
		if err != nil {
			return nil // Skip if can't get info
		}

		// Skip if too large (10MB limit is a safety guard to prevent memory exhaustion)
		if info.Size() > 10*1024*1024 {
			sb.WriteString(fmt.Sprintf("===== %s (Skipped: Too large) =====\n\n", relPath))
			return nil
		}

		content, isBinary, err := readFile(path)
		if err != nil {
			sb.WriteString(fmt.Sprintf("===== %s (Error reading file) =====\n\n", relPath))
			return nil
		}
		if isBinary {
			// Skip binary files silently to avoid cluttering the context
			return nil
		}

		// Append formatted content with header
		sb.WriteString(fmt.Sprintf("===== %s =====\n%s\n\n", relPath, content))

		return nil
	})

	return sb.String(), err
}

// shouldIgnore checks if a given path matches any of the configured ignore patterns.
// It supports doublestar globs (e.g., **/*.txt) and basic directory matching.
func (p *Processor) shouldIgnore(path string, isDir bool) bool {
	// Basic ignore implementation matching TS logic (check patterns)
	// In TS: "node_modules/" matches directory.

	pathToCheck := path
	if isDir && !strings.HasSuffix(pathToCheck, "/") {
		pathToCheck += "/"
	}

	for _, pattern := range p.Config.Ignore {
		matched, _ := doublestar.Match(pattern, pathToCheck)
		if matched {
			return true
		}

		// Try recursive match if not absolute
		// e.g. "dist/" should match "sub/dist/" or "dist/"
		if !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "**/") {
			matched, _ := doublestar.Match("**/"+pattern, pathToCheck)
			if matched {
				return true
			}
		}
		// Also check if the pattern matches a parent directory
		// e.g. pattern "node_modules/" should match "node_modules/foo.js"
		if !isDir && strings.HasSuffix(pattern, "/") {
			if strings.HasPrefix(pathToCheck, pattern) {
				return true
			}
		}
	}
	return false
}

// readFile reads the file content and checks if it appears to be binary.
// It uses a simple heuristic: valid UTF-8 and no null bytes in the first 1KB.
func readFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}

	if !utf8.Valid(data) {
		return "", true, nil // Treat as binary
	}

	// Check for null bytes as a heuristic for binary content
	for i := 0; i < len(data) && i < 1024; i++ {
		if data[i] == 0 {
			return "", true, nil
		}
	}

	return string(data), false, nil
}

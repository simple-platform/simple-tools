package processor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"contextualizer/internal/config"
)

type Processor struct {
	Config      *config.Config
	ProjectRoot string
}

func New(cfg *config.Config, root string) *Processor {
	return &Processor{
		Config:      cfg,
		ProjectRoot: root,
	}
}

// ProcessDirectory walks the directory and returns the combined content
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
		
		// Use slash for consistent matching
		relPath = filepath.ToSlash(relPath)

		// Check ignore
		if p.shouldIgnore(relPath, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		// It's a file
		info, err := d.Info()
		if err != nil {
			return nil // Skip if can't get info
		}
		
		// Skip if too large? (Optional, but TS didn't seem to enforce size limit, just binary check)
		if info.Size() > 10*1024*1024 { // 10MB limit safety
		    sb.WriteString(fmt.Sprintf("=== %s (Skipped: Too large) ===\n\n", relPath))
			return nil
		}

		content, isBinary, err := readFile(path)
		if err != nil {
			sb.WriteString(fmt.Sprintf("=== %s (Error reading file) ===\n\n", relPath))
			return nil
		}
		if isBinary {
			// Skip binary silent or with message? TS skips silently.
			return nil
		}

		sb.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", relPath, content))

		return nil
	})

	return sb.String(), err
}

func (p *Processor) shouldIgnore(path string, isDir bool) bool {
    // Basic ignore implementation matching TS logic (check patterns)
    // In TS: "node_modules/" matches directory.
    
    pathToCheck := path
    if isDir && !strings.HasSuffix(pathToCheck, "/") {
        pathToCheck += "/"
    }

	for _, pattern := range p.Config.Ignore {
		// Handle negation if needed (simplified for now: TS supported negation)
		// Assuming standard glob behavior
		
		matched, _ := doublestar.Match(pattern, pathToCheck)
		if matched {
			return true
		}
		
		// Also check if the pattern matches a parent directory
		// e.g. pattern "node_modules/" should match "node_modules/foo.js"
		// This is implicitly handled if we skip the dir in WalkDir, but for file check:
		if !isDir && strings.HasSuffix(pattern, "/") {
		    if strings.HasPrefix(pathToCheck, pattern) {
		        return true
		    }
		}
	}
	return false
}

func readFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}

	if !utf8.Valid(data) {
		return "", true, nil // Treat as binary
	}
	
	// Heuristic for binary (null byte check)
	for i := 0; i < len(data) && i < 1024; i++ {
	    if data[i] == 0 {
	        return "", true, nil
	    }
	}

	return string(data), false, nil
}

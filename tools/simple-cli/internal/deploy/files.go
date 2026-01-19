package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// FileInfo represents a file to deploy.
type FileInfo struct {
	Path    string // Relative path from app root
	Hash    string // SHA256 hash of content
	Size    int64  // File size in bytes
	Content []byte // File content (loaded in parallel)
}

// FileCollector handles parallel file collection with dependency injection.
type FileCollector struct {
	FS         FileSystem
	NumWorkers int
}

// NewFileCollector creates a FileCollector with default settings.
func NewFileCollector() *FileCollector {
	return &FileCollector{
		FS:         OSFileSystem{},
		NumWorkers: runtime.NumCPU(),
	}
}

// CollectFiles gathers all deployable files with MAXIMUM parallelization.
// Mirrors Publisher.stream_files/1 logic from simple_devops.
func (c *FileCollector) CollectFiles(appPath string) (map[string]FileInfo, error) {
	// Collect file paths first (fast, single-threaded)
	paths, err := c.collectPaths(appPath)
	if err != nil {
		return nil, err
	}

	if len(paths) == 0 {
		return make(map[string]FileInfo), nil
	}

	// Process files in parallel
	numWorkers := c.NumWorkers
	if numWorkers < 1 {
		numWorkers = 1
	}
	if numWorkers > len(paths) {
		numWorkers = len(paths)
	}

	jobs := make(chan string, len(paths))
	results := make(chan *FileInfo, len(paths))
	errors := make(chan error, len(paths))

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				fi, err := c.processFile(appPath, path)
				if err != nil {
					errors <- err
					continue
				}
				if fi != nil {
					results <- fi
				}
			}
		}()
	}

	// Send jobs
	for _, p := range paths {
		jobs <- p
	}
	close(jobs)

	// Wait for workers to finish
	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	var firstErr error
	for err := range errors {
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}

	// Collect results
	files := make(map[string]FileInfo)
	for fi := range results {
		files[fi.Path] = *fi
	}

	return files, nil
}

// collectPaths returns all file paths to be deployed.
// Mirrors the allowlist in SimpleDevOps.Publisher.stream_files/1
func (c *FileCollector) collectPaths(appPath string) ([]string, error) {
	var paths []string

	// Root config files
	for _, name := range []string{"app.scl", "tables.scl"} {
		fullPath := filepath.Join(appPath, name)
		if _, err := c.FS.Stat(fullPath); err == nil {
			paths = append(paths, name)
		}
	}

	// Security directory - all files
	paths = append(paths, c.globFiles(appPath, "security")...)

	// Scripts directory - all files
	paths = append(paths, c.globFiles(appPath, "scripts")...)

	// Records directory - all files
	paths = append(paths, c.globFiles(appPath, "records")...)

	// Actions - only WASM build outputs
	actionsDir := filepath.Join(appPath, "actions")
	entries, _ := os.ReadDir(actionsDir)
	for _, entry := range entries {
		if entry.IsDir() {
			// release.wasm
			wasmPath := filepath.Join("actions", entry.Name(), "build", "release.wasm")
			if _, err := c.FS.Stat(filepath.Join(appPath, wasmPath)); err == nil {
				paths = append(paths, wasmPath)
			}
			// release.async.wasm
			asyncWasmPath := filepath.Join("actions", entry.Name(), "build", "release.async.wasm")
			if _, err := c.FS.Stat(filepath.Join(appPath, asyncWasmPath)); err == nil {
				paths = append(paths, asyncWasmPath)
			}
		}
	}

	return paths, nil
}

// globFiles recursively finds all files in a directory.
func (c *FileCollector) globFiles(appPath, dir string) []string {
	var result []string
	dirPath := filepath.Join(appPath, dir)

	_ = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(appPath, path)
		result = append(result, rel)
		return nil
	})

	return result
}

// processFile reads a file, computes hash, and returns FileInfo.
func (c *FileCollector) processFile(appPath, relPath string) (*FileInfo, error) {
	absPath := filepath.Join(appPath, relPath)

	content, err := c.FS.ReadFile(absPath)
	if err != nil {
		// File might have been deleted between path collection and processing
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	hash := sha256.Sum256(content)

	return &FileInfo{
		Path:    relPath,
		Hash:    hex.EncodeToString(hash[:]),
		Size:    int64(len(content)),
		Content: content,
	}, nil
}

// DeployResult represents the result of a successful deployment.
type DeployResult struct {
	AppID     string `json:"app_id"`
	Version   string `json:"version"`
	FileCount int    `json:"file_count"`
}

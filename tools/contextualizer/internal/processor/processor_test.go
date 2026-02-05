package processor

import (
	"contextualizer/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProcessor_ProcessDirectory verifies the core logic of the processor.
// It checks:
// 1. Ignoring explicitly configured directories (node_modules, dist).
// 2. Ignoring binary files (configured extension and binary content detection).
// 3. Handling large files (skip logic).
// 4. Correctly reading and aggregating valid text files.
// 5. Handling invalid UTF-8 and permission errors.
func TestProcessor_ProcessDirectory(t *testing.T) {
	tmp := t.TempDir()

	// Setup test filesystem structure:
	// tmp/src/file.txt            -> Should be included
	// tmp/node_modules/bad.js     -> Should be ignored (dir)
	// tmp/dist/ignored.js         -> Should be ignored (dir)
	// tmp/binary.bin              -> Should be ignored (pattern)
	// tmp/src/large.txt           -> Should be skipped (size)

	createFile(t, tmp, "src/file.txt", "hello world")
	createFile(t, tmp, "node_modules/bad.js", "bad")
	createFile(t, tmp, "dist/ignored.js", "ignored")
	createFile(t, tmp, "binary.bin", string([]byte{'a', 0, 'b'}))
	createFile(t, tmp, "src/large.txt", strings.Repeat("a", 15*1024*1024)) // 15MB

	cfg := &config.Config{
		Ignore: []string{"node_modules/", "dist/", "*.bin"},
	}

	proc := New(cfg, tmp)

	// Case 1: Process subdirectory "src" directly
	// Should contain the valid file content and skip the large file.
	output, err := proc.ProcessDirectory(filepath.Join(tmp, "src"))
	if err != nil {
		t.Fatalf("ProcessDirectory failed: %v", err)
	}

	if !strings.Contains(output, "src/file.txt") {
		t.Error("Output should contain src/file.txt")
	}
	if !strings.Contains(output, "hello world") {
		t.Error("Output should contain file content")
	}
	// Check large file skip
	if !strings.Contains(output, "Skipped: Too large") {
		t.Error("Should skip large file")
	}

	// Case 2: Process Root to verify ignore patterns work on directories
	// We normally call ProcessDirectory on subdirs in the real app, but
	// testing from root validates the recursive walk logic respect ignores.

	outputRoot, err := proc.ProcessDirectory(tmp)
	if err != nil {
		t.Fatalf("ProcessDirectory(root) failed: %v", err)
	}

	if strings.Contains(outputRoot, "node_modules/bad.js") {
		t.Error("Should ignore node_modules content")
	}
	if strings.Contains(outputRoot, "dist/ignored.js") {
		t.Error("Should ignore dist content")
	}
	if strings.Contains(outputRoot, "binary.bin") {
		t.Error("Should ignore binary files (via extension or content check? Config has *.bin)")
	}

	// Case 3: Test Invalid UTF-8 (Binary content detection fallback)
	badFile := filepath.Join(tmp, "bad_utf8.txt")
	if err := os.WriteFile(badFile, []byte{0xff, 0xfe, 0xfd}, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	outputRoot, _ = proc.ProcessDirectory(tmp) // Re-scan

	if strings.Contains(outputRoot, "bad_utf8.txt") {
		// Should be skipped as binary
		// processor.go checks !utf8.Valid(data) and returns isBinary=true
		t.Error("Should have skipped bad_utf8.txt")
	}

	// Case 4: Test Error Case (File with no permissions)
	lockedFile := filepath.Join(tmp, "locked.txt")
	if err := os.WriteFile(lockedFile, []byte("secret"), 0000); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	defer func() { _ = os.Chmod(lockedFile, 0644) }() // Cleanup permissions so TempDir cleanup works

	outputError, _ := proc.ProcessDirectory(tmp)
	if !strings.Contains(outputError, "Error reading file") {
		// internal/processor/processor.go: readFile catches err and prints "Error reading file"
		t.Error("Should report error for locked file")
	}
}

// createFile is a test helper to easily create files with content and parent directories.
func createFile(t *testing.T, root, path, content string) {
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

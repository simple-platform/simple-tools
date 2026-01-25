package processor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"contextualizer/internal/config"
)

func TestProcessor_ProcessDirectory(t *testing.T) {
	tmp := t.TempDir()
	
	// Setup:
	// tmp/src/file.txt
	// tmp/node_modules/bad.js
	// tmp/dist/ignored.js
	// tmp/binary.bin (with null byte)
	
	createFile(t, tmp, "src/file.txt", "hello world")
	createFile(t, tmp, "node_modules/bad.js", "bad")
	createFile(t, tmp, "dist/ignored.js", "ignored")
	createFile(t, tmp, "binary.bin", string([]byte{'a', 0, 'b'}))
	createFile(t, tmp, "src/large.txt", strings.Repeat("a", 15*1024*1024)) // 15MB

	cfg := &config.Config{
		Ignore: []string{"node_modules/", "dist/", "*.bin"},
	}
	
	proc := New(cfg, tmp)
	
	// Process "src"
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
	
	// Process Root 
	// Note: We normally call ProcessDirectory on subdirs, but let's test root walk
	// to verify ignore patterns work on directories
	
	// "node_modules" should be skipped entirely
	// We can't easily test "SkipDir" return from outside, but we can check if content is missing
	
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
	if strings.Contains(outputRoot, "dist/ignored.js") {
		t.Error("Should ignore dist content")
	}
	if strings.Contains(outputRoot, "binary.bin") {
		t.Error("Should ignore binary files (via extension or content check? Config has *.bin)")
	}
	
	// Test Invalid UTF8
	badFile := filepath.Join(tmp, "bad_utf8.txt")
	if err := os.WriteFile(badFile, []byte{0xff, 0xfe, 0xfd}, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	outputRoot, _ = proc.ProcessDirectory(tmp) // Re-scan
	if strings.Contains(outputRoot, "bad_utf8.txt") {
	    // Should be skipped as binary
	    // processor.go: !utf8.Valid(data) -> return "", true, nil
	    // ProcessDirectory: if isBinary -> return nil
	    // Content shouldn't be there.
	    t.Error("Should have skipped bad_utf8.txt")
	} 
	
	// Test Error Case: File with no permissions
	lockedFile := filepath.Join(tmp, "locked.txt")
	if err := os.WriteFile(lockedFile, []byte("secret"), 0000); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	defer func() { _ = os.Chmod(lockedFile, 0644) }() // Cleanup
	
	outputError, _ := proc.ProcessDirectory(tmp)
	if !strings.Contains(outputError, "Error reading file") {
	    // Note: Depends on if WalkDir fails or readFile fails. 
	    // internal/processor/processor.go: readFile catches err and prints "Error reading file"
	    t.Error("Should report error for locked file")
	}
}

func createFile(t *testing.T, root, path, content string) {
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

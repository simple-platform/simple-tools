package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestFileCollector_CollectFiles(t *testing.T) {
	// Create a temporary app structure
	dir := t.TempDir()

	// Create app.scl
	appSCL := filepath.Join(dir, "app.scl")
	_ = os.WriteFile(appSCL, []byte("id test\nversion 1.0.0"), 0644)

	// Create tables.scl
	tablesSCL := filepath.Join(dir, "tables.scl")
	_ = os.WriteFile(tablesSCL, []byte("table users {}"), 0644)

	// Create security directory with files
	securityDir := filepath.Join(dir, "security")
	_ = os.MkdirAll(securityDir, 0755)
	_ = os.WriteFile(filepath.Join(securityDir, "policy.scl"), []byte("policy {}"), 0644)

	// Create scripts directory
	scriptsDir := filepath.Join(dir, "scripts")
	_ = os.MkdirAll(scriptsDir, 0755)
	_ = os.WriteFile(filepath.Join(scriptsDir, "init.js"), []byte("console.log('init')"), 0644)

	// Create records directory
	recordsDir := filepath.Join(dir, "records")
	_ = os.MkdirAll(recordsDir, 0755)
	_ = os.WriteFile(filepath.Join(recordsDir, "seed.json"), []byte("[]"), 0644)

	// Create action with WASM
	actionDir := filepath.Join(dir, "actions", "my-action", "build")
	_ = os.MkdirAll(actionDir, 0755)
	_ = os.WriteFile(filepath.Join(actionDir, "release.wasm"), []byte("wasm-bytes"), 0644)
	_ = os.WriteFile(filepath.Join(actionDir, "release.async.wasm"), []byte("async-wasm"), 0644)

	collector := NewFileCollector()
	files, err := collector.CollectFiles(dir)

	if err != nil {
		t.Fatalf("CollectFiles() unexpected error = %v", err)
	}

	expectedFiles := []string{
		"app.scl",
		"tables.scl",
		"security/policy.scl",
		"scripts/init.js",
		"records/seed.json",
		"actions/my-action/build/release.wasm",
		"actions/my-action/build/release.async.wasm",
	}

	for _, name := range expectedFiles {
		if _, ok := files[name]; !ok {
			t.Errorf("CollectFiles() missing file %q", name)
		}
	}

	// Verify hash is correct
	appContent := []byte("id test\nversion 1.0.0")
	expectedHash := sha256.Sum256(appContent)
	if files["app.scl"].Hash != hex.EncodeToString(expectedHash[:]) {
		t.Errorf("CollectFiles() app.scl hash mismatch")
	}

	// Verify content is loaded
	if string(files["app.scl"].Content) != "id test\nversion 1.0.0" {
		t.Errorf("CollectFiles() app.scl content mismatch")
	}

	// Verify size
	if files["app.scl"].Size != int64(len(appContent)) {
		t.Errorf("CollectFiles() app.scl size = %d, want %d", files["app.scl"].Size, len(appContent))
	}
}

func TestFileCollector_CollectFiles_EmptyApp(t *testing.T) {
	dir := t.TempDir()

	collector := NewFileCollector()
	files, err := collector.CollectFiles(dir)

	if err != nil {
		t.Fatalf("CollectFiles() unexpected error = %v", err)
	}

	if len(files) != 0 {
		t.Errorf("CollectFiles() expected 0 files, got %d", len(files))
	}
}

func TestFileCollector_CollectFiles_OnlyAppSCL(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "app.scl"), []byte("content"), 0644)

	collector := NewFileCollector()
	files, err := collector.CollectFiles(dir)

	if err != nil {
		t.Fatalf("CollectFiles() unexpected error = %v", err)
	}

	if len(files) != 1 {
		t.Errorf("CollectFiles() expected 1 file, got %d", len(files))
	}

	if _, ok := files["app.scl"]; !ok {
		t.Error("CollectFiles() missing app.scl")
	}
}

func TestFileCollector_CollectFiles_NestedSecurityFiles(t *testing.T) {
	dir := t.TempDir()

	// Create nested security structure
	securityDir := filepath.Join(dir, "security", "policies", "admin")
	_ = os.MkdirAll(securityDir, 0755)
	_ = os.WriteFile(filepath.Join(securityDir, "admin-policy.scl"), []byte("admin policy"), 0644)

	collector := NewFileCollector()
	files, err := collector.CollectFiles(dir)

	if err != nil {
		t.Fatalf("CollectFiles() unexpected error = %v", err)
	}

	expectedPath := filepath.Join("security", "policies", "admin", "admin-policy.scl")
	if _, ok := files[expectedPath]; !ok {
		t.Errorf("CollectFiles() missing nested file %q", expectedPath)
	}
}

func TestFileCollector_CollectFiles_MultipleActions(t *testing.T) {
	dir := t.TempDir()

	// Create multiple actions
	for _, actionName := range []string{"action-a", "action-b", "action-c"} {
		actionDir := filepath.Join(dir, "actions", actionName, "build")
		_ = os.MkdirAll(actionDir, 0755)
		_ = os.WriteFile(filepath.Join(actionDir, "release.wasm"), []byte("wasm-"+actionName), 0644)
	}

	collector := NewFileCollector()
	files, err := collector.CollectFiles(dir)

	if err != nil {
		t.Fatalf("CollectFiles() unexpected error = %v", err)
	}

	for _, actionName := range []string{"action-a", "action-b", "action-c"} {
		expectedPath := filepath.Join("actions", actionName, "build", "release.wasm")
		if _, ok := files[expectedPath]; !ok {
			t.Errorf("CollectFiles() missing action file %q", expectedPath)
		}
	}
}

func TestFileCollector_CollectFiles_IgnoresNonWASM(t *testing.T) {
	dir := t.TempDir()

	// Create action with WASM and other files
	actionDir := filepath.Join(dir, "actions", "my-action", "build")
	_ = os.MkdirAll(actionDir, 0755)
	_ = os.WriteFile(filepath.Join(actionDir, "release.wasm"), []byte("wasm"), 0644)
	_ = os.WriteFile(filepath.Join(actionDir, "debug.wasm"), []byte("debug"), 0644)
	_ = os.WriteFile(filepath.Join(actionDir, "other.txt"), []byte("other"), 0644)

	// Also create source files that should be ignored
	srcDir := filepath.Join(dir, "actions", "my-action", "src")
	_ = os.MkdirAll(srcDir, 0755)
	_ = os.WriteFile(filepath.Join(srcDir, "index.ts"), []byte("source"), 0644)

	collector := NewFileCollector()
	files, err := collector.CollectFiles(dir)

	if err != nil {
		t.Fatalf("CollectFiles() unexpected error = %v", err)
	}

	// Should only have release.wasm
	expectedPath := filepath.Join("actions", "my-action", "build", "release.wasm")
	if _, ok := files[expectedPath]; !ok {
		t.Errorf("CollectFiles() missing release.wasm")
	}

	// Should NOT have debug.wasm, other.txt, or source files
	unexpectedPaths := []string{
		filepath.Join("actions", "my-action", "build", "debug.wasm"),
		filepath.Join("actions", "my-action", "build", "other.txt"),
		filepath.Join("actions", "my-action", "src", "index.ts"),
	}

	for _, p := range unexpectedPaths {
		if _, ok := files[p]; ok {
			t.Errorf("CollectFiles() should not include %q", p)
		}
	}
}

func TestFileCollector_Parallelization(t *testing.T) {
	dir := t.TempDir()

	// Create many files to trigger parallelization
	securityDir := filepath.Join(dir, "security")
	_ = os.MkdirAll(securityDir, 0755)

	for i := 0; i < 20; i++ {
		name := filepath.Join(securityDir, "policy"+strconv.Itoa(i)+".scl")
		_ = os.WriteFile(name, []byte("policy content"), 0644)
	}

	// Use single worker to ensure it still works
	collector := &FileCollector{
		FS:         OSFileSystem{},
		NumWorkers: 1,
	}

	files, err := collector.CollectFiles(dir)
	if err != nil {
		t.Fatalf("CollectFiles() with 1 worker unexpected error = %v", err)
	}

	if len(files) != 20 {
		t.Errorf("CollectFiles() expected 20 files, got %d", len(files))
	}

	// Use many workers
	collector.NumWorkers = 10
	files2, err := collector.CollectFiles(dir)
	if err != nil {
		t.Fatalf("CollectFiles() with 10 workers unexpected error = %v", err)
	}

	if len(files2) != 20 {
		t.Errorf("CollectFiles() expected 20 files, got %d", len(files2))
	}
}

func TestNewFileCollector(t *testing.T) {
	collector := NewFileCollector()
	if collector == nil {
		t.Fatal("NewFileCollector() returned nil")
	}
	if collector.FS == nil {
		t.Error("NewFileCollector() FS is nil")
	}
	if collector.NumWorkers < 1 {
		t.Error("NewFileCollector() NumWorkers should be at least 1")
	}
}

func TestProcessFile_HashCorrectness(t *testing.T) {
	dir := t.TempDir()
	content := []byte("test content for hashing")
	testFile := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(testFile, content, 0644)

	collector := NewFileCollector()
	fi, err := collector.processFile(dir, "test.txt")

	if err != nil {
		t.Fatalf("processFile() unexpected error = %v", err)
	}

	expectedHash := sha256.Sum256(content)
	if fi.Hash != hex.EncodeToString(expectedHash[:]) {
		t.Errorf("processFile() hash = %q, want %q", fi.Hash, hex.EncodeToString(expectedHash[:]))
	}
}

func TestProcessFile_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	collector := NewFileCollector()

	fi, err := collector.processFile(dir, "nonexistent.txt")

	// Should return nil, nil for missing files (they might have been deleted)
	if err != nil {
		t.Errorf("processFile() should return nil error for missing file, got %v", err)
	}
	if fi != nil {
		t.Error("processFile() should return nil FileInfo for missing file")
	}
}

func TestCollectPaths_EmptyDirectories(t *testing.T) {
	dir := t.TempDir()

	// Create empty directories
	_ = os.MkdirAll(filepath.Join(dir, "security"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "scripts"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "records"), 0755)

	collector := NewFileCollector()
	paths, err := collector.collectPaths(dir)

	if err != nil {
		t.Fatalf("collectPaths() unexpected error = %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("collectPaths() expected 0 paths for empty dirs, got %d", len(paths))
	}
}

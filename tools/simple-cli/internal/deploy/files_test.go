package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"

	"simple-cli/internal/fsx"
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

	// Create space with dist/
	spaceDir := filepath.Join(dir, "spaces", "my-space", "dist")
	_ = os.MkdirAll(spaceDir, 0755)
	_ = os.WriteFile(filepath.Join(spaceDir, "index.html"), []byte("<html>"), 0644)

	spaceAssetsDir := filepath.Join(spaceDir, "assets")
	_ = os.MkdirAll(spaceAssetsDir, 0755)
	_ = os.WriteFile(filepath.Join(spaceAssetsDir, "index-abc.js"), []byte("js-bundle"), 0644)

	// Create assets directory
	assetsDir := filepath.Join(dir, "assets")
	_ = os.MkdirAll(assetsDir, 0755)
	_ = os.WriteFile(filepath.Join(assetsDir, "image.png"), []byte("image-data"), 0644)

	// Create knowledge directory
	knowledgeDir := filepath.Join(dir, "knowledge")
	_ = os.MkdirAll(knowledgeDir, 0755)
	_ = os.WriteFile(filepath.Join(knowledgeDir, "docs.md"), []byte("# Docs"), 0644)

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
		"spaces/my-space/dist/index.html",
		"spaces/my-space/dist/assets/index-abc.js",
		"assets/image.png",
		"knowledge/docs.md",
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

// A RECORD THAT SOURCES A FIELD FROM action.json NEEDS action.json DEPLOYED.
//
// An app may keep a logic record's description and parameter schema from
// drifting by reading them out of the generated artifact rather than restating
// them, and that expression is resolved against the deployed file set. This
// collector used to gather only the built modules out of an action directory,
// so such an app uploaded without complaint and then failed every one of those
// records on install, with a file_not_found out of content storage naming a
// file the author could plainly see on disk.
//
// The two files either side of it are here so the test cannot pass by
// collecting the whole directory: the source stays out, the built module comes
// along.
func TestFileCollector_CollectFiles_IncludesActionMetadata(t *testing.T) {
	dir := t.TempDir()

	action := filepath.Join(dir, "actions", "read-site-readiness")
	_ = os.MkdirAll(filepath.Join(action, "build"), 0755)
	_ = os.MkdirAll(filepath.Join(action, "src"), 0755)
	_ = os.WriteFile(filepath.Join(action, "action.json"), []byte(`{"description":"d","schema":{}}`), 0644)
	_ = os.WriteFile(filepath.Join(action, "build", "release.wasm"), []byte("wasm"), 0644)
	_ = os.WriteFile(filepath.Join(action, "src", "index.ts"), []byte("source"), 0644)

	collector := NewFileCollector()

	files, err := collector.CollectFiles(dir)
	if err != nil {
		t.Fatalf("CollectFiles() unexpected error = %v", err)
	}

	metadata := filepath.Join("actions", "read-site-readiness", "action.json")
	if _, ok := files[metadata]; !ok {
		t.Errorf("CollectFiles() left out %q, which a record's $file() expression resolves against", metadata)
	}

	if _, ok := files[filepath.Join("actions", "read-site-readiness", "build", "release.wasm")]; !ok {
		t.Errorf("CollectFiles() missing release.wasm")
	}

	if _, ok := files[filepath.Join("actions", "read-site-readiness", "src", "index.ts")]; ok {
		t.Errorf("CollectFiles() deployed an action's source, which is not deployed")
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
	_ = os.MkdirAll(filepath.Join(dir, "knowledge"), 0755)

	collector := NewFileCollector()
	paths, err := collector.collectPaths(dir)

	if err != nil {
		t.Fatalf("collectPaths() unexpected error = %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("collectPaths() expected 0 paths for empty dirs, got %d", len(paths))
	}
}

// progressRecorder collects OnProgress calls from concurrent workers.
type progressRecorder struct {
	mu    sync.Mutex
	done  []int
	total []int
}

func (r *progressRecorder) record(done, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = append(r.done, done)
	r.total = append(r.total, total)
}

// check verifies that n paths were each reported exactly once, as done
// values 1..n in some order, all against a total of n.
func (r *progressRecorder) check(t *testing.T, n int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.done) != n {
		t.Fatalf("OnProgress called %d times, want %d", len(r.done), n)
	}
	got := slices.Sorted(slices.Values(r.done))
	for i, done := range got {
		if done != i+1 {
			t.Fatalf("done values = %v, want each of 1..%d once", got, n)
		}
	}
	for _, total := range r.total {
		if total != n {
			t.Fatalf("total = %d, want %d", total, n)
		}
	}
}

// writeFiles creates n small files under dir/security and returns dir.
func writeFiles(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	security := filepath.Join(dir, "security")
	if err := os.MkdirAll(security, fsx.DirPerm); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		name := filepath.Join(security, "policy"+strconv.Itoa(i)+".scl")
		if err := os.WriteFile(name, []byte("policy"), fsx.FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFileCollector_OnProgress(t *testing.T) {
	const files = 20
	dir := writeFiles(t, files)

	for _, workers := range []int{1, 4, 10} {
		t.Run(strconv.Itoa(workers)+" workers", func(t *testing.T) {
			var rec progressRecorder
			collector := &FileCollector{FS: OSFileSystem{}, NumWorkers: workers, OnProgress: rec.record}

			got, err := collector.CollectFiles(dir)
			if err != nil {
				t.Fatalf("CollectFiles() error = %v", err)
			}
			if len(got) != files {
				t.Fatalf("CollectFiles() = %d files, want %d", len(got), files)
			}
			rec.check(t, files)
		})
	}
}

// readFailFS reads through to the disk except for the files it has an error
// for, keyed by base name.
type readFailFS struct {
	OSFileSystem
	errs map[string]error
}

func (f readFailFS) ReadFile(path string) ([]byte, error) {
	if err, ok := f.errs[filepath.Base(path)]; ok {
		return nil, err
	}
	return f.OSFileSystem.ReadFile(path)
}

// A path that vanished or could not be read still counts as processed, or
// the progress would stop short of its total.
func TestFileCollector_OnProgressCountsMissingAndFailedFiles(t *testing.T) {
	const files = 10
	dir := writeFiles(t, files)

	tests := []struct {
		name      string
		errs      map[string]error
		wantFiles int
		wantErr   bool
	}{
		{
			name:      "vanished files",
			errs:      map[string]error{"policy2.scl": os.ErrNotExist, "policy7.scl": os.ErrNotExist},
			wantFiles: files - 2,
		},
		{
			name:    "unreadable files",
			errs:    map[string]error{"policy3.scl": os.ErrPermission, "policy5.scl": os.ErrPermission},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec progressRecorder
			collector := &FileCollector{FS: readFailFS{errs: tt.errs}, NumWorkers: 4, OnProgress: rec.record}

			got, err := collector.CollectFiles(dir)
			if tt.wantErr {
				if !errors.Is(err, os.ErrPermission) {
					t.Fatalf("CollectFiles() error = %v, want os.ErrPermission", err)
				}
			} else {
				if err != nil {
					t.Fatalf("CollectFiles() error = %v", err)
				}
				if len(got) != tt.wantFiles {
					t.Errorf("CollectFiles() = %d files, want %d", len(got), tt.wantFiles)
				}
			}
			rec.check(t, files)
		})
	}
}

func TestFileCollector_NilOnProgress(t *testing.T) {
	dir := writeFiles(t, 5)
	collector := &FileCollector{FS: OSFileSystem{}, NumWorkers: 2}

	got, err := collector.CollectFiles(dir)
	if err != nil {
		t.Fatalf("CollectFiles() error = %v", err)
	}
	if len(got) != 5 {
		t.Errorf("CollectFiles() = %d files, want 5", len(got))
	}
}

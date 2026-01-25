package build

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGzip(t *testing.T) {
	tmpDir := t.TempDir()
	gzPath := filepath.Join(tmpDir, "test.gz")
	destPath := filepath.Join(tmpDir, "test.txt")

	// Create a dummy gzip file
	f, err := os.Create(gzPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	w := gzip.NewWriter(f)
	_, _ = w.Write([]byte("hello world"))
	_ = w.Close()

	// Extract
	if err := ExtractGzip(gzPath, destPath); err != nil {
		t.Fatalf("ExtractGzip() error = %v", err)
	}

	// Verify content
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello world" {
		t.Errorf("got %s, want hello world", string(content))
	}
}

func TestExtractTarGzFile(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "test.tar.gz")
	destPath := filepath.Join(tmpDir, "extracted.txt")

	// Create a dummy tar.gz file
	f, err := os.Create(tgzPath)
	if err != nil {
		t.Fatal(err)
	}

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	body := []byte("tar content")
	hdr := &tar.Header{
		Name: "bin/target-file",
		Mode: 0600,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}

	// Close writers to flush data
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Extract
	if err := ExtractTarGzFile(tgzPath, destPath, "bin/target-file"); err != nil {
		t.Fatalf("ExtractTarGzFile() error = %v", err)
	}

	// Verify
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(body) {
		t.Errorf("got %s, want %s", string(content), string(body))
	}

	// Test Not Found
	if err := ExtractTarGzFile(tgzPath, filepath.Join(tmpDir, "fail"), "missing"); err == nil {
		t.Error("Expected error for missing file")
	}
}

func TestExtractTarGz(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "test.tar.gz")
	destDir := filepath.Join(tmpDir, "output")

	// Create dummy tarball with structure:
	// root/
	//   bin/
	//     exec
	//   lib/
	//     data.txt
	f, err := os.Create(tgzPath)
	if err != nil {
		t.Fatal(err)
	}

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	files := map[string]string{
		"root/bin/exec":     "executable content",
		"root/lib/data.txt": "library content",
	}

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	// Make sure to add directories too if needed, but file paths implicitly create them in logic
	// But let's add root dir explicitly to test that too
	hdr := &tar.Header{
		Name:     "root/",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// Test extraction with stripping 1 component
	if err := ExtractTarGz(tgzPath, destDir, 1); err != nil {
		t.Fatalf("ExtractTarGz() error = %v", err)
	}

	// Verify bin/exec exists in output
	execPath := filepath.Join(destDir, "bin", "exec")
	content, err := os.ReadFile(execPath)
	if err != nil {
		t.Errorf("failed to read extracted file: %v", err)
	}
	if string(content) != "executable content" {
		t.Errorf("content mismatch")
	}

	// Verify lib/data.txt
	libPath := filepath.Join(destDir, "lib", "data.txt")
	if _, err := os.Stat(libPath); os.IsNotExist(err) {
		t.Errorf("library file missing")
	}

	// Verify root/ was stripped (should not exist)
	rootPath := filepath.Join(destDir, "root")
	if _, err := os.Stat(rootPath); err == nil {
		t.Errorf("root directory was not stripped")
	}
}

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
	defer f.Close()

	w := gzip.NewWriter(f)
	w.Write([]byte("hello world"))
	w.Close()

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

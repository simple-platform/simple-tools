package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A TOOL IS INSTALLED BY RENAMING A FINISHED FILE ONTO ITS NAME, AND THESE
// TESTS HOLD THAT.
//
// What they look for is what a rename leaves and a write-in-place does not: a
// file at the tool's name that is a different file from the one that was
// there, nothing half-made beside it, and the old tool untouched when the new
// one could not be made.

// serving answers every request with one body.
func serving(t *testing.T, body []byte) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	return server
}

// toolAt names a tool whose one version is downloaded from a server.
func toolAt(server *httptest.Server, name, version string, postFn func(string, string) error) ToolDef {
	return ToolDef{
		Name:           name,
		CheckVersionFn: func() (string, error) { return version, nil },
		DownloadURLFn:  func(string) string { return server.URL },
		PostDownloadFn: postFn,
	}
}

// identityOf looks at a file and settles which file it is.
//
// On Windows a file's identity is not read when the file is looked at. It is
// read from the path the first time it is compared, which in these tests
// would be after the tool has been replaced. Comparing the file with itself
// reads it now.
func identityOf(t *testing.T, path string) os.FileInfo {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to look at %s: %v", path, err)
	}
	if !os.SameFile(info, info) {
		t.Fatalf("%s is not the same file as itself", path)
	}

	return info
}

// namesIn lists what a directory holds.
func namesIn(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list %s: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// archiveOf writes files into a .tar.gz, each under one top-level directory as
// a released archive has them.
func archiveOf(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var packed bytes.Buffer
	zipped := gzip.NewWriter(&packed)
	archive := tar.NewWriter(zipped)

	for name, content := range files {
		header := &tar.Header{Name: "binaryen-version_1/" + name, Mode: 0755, Size: int64(len(content))}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatalf("failed to write the archive: %v", err)
		}
		if _, err := archive.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write the archive: %v", err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("failed to finish the archive: %v", err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatalf("failed to finish the archive: %v", err)
	}

	return packed.Bytes()
}

func TestEnsureTool_ReplacesAToolWithADifferentFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, err := EnsureTool(toolAt(serving(t, []byte("version one")), "test-tool", "1.0.0", nil))
	if err != nil {
		t.Fatalf("EnsureTool() for the first version error = %v", err)
	}
	before := identityOf(t, path)

	// The manifest is aged so the next call asks for the version again and
	// finds a newer one.
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	manifest["test-tool"] = ToolInfo{Version: "1.0.0", LastCheck: time.Now().Add(-48 * time.Hour)}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	if _, err := EnsureTool(toolAt(serving(t, []byte("version two")), "test-tool", "2.0.0", nil)); err != nil {
		t.Fatalf("EnsureTool() for the second version error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil || string(content) != "version two" {
		t.Errorf("the tool holds %q (%v), want %q", content, err, "version two")
	}
	if os.SameFile(before, identityOf(t, path)) {
		t.Error("the tool is the same file as before: it was written into, not replaced")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0100 == 0 {
		t.Errorf("the tool cannot be run: mode %v (%v)", info.Mode(), err)
	}
	if names := namesIn(t, filepath.Dir(path)); !slices.Equal(names, []string{"test-tool"}) {
		t.Errorf("the tools directory holds %v, want only the tool", names)
	}
}

func TestEnsureTool_AnInstallThatFailsLeavesTheToolAsItWas(t *testing.T) {
	unpackFails := errors.New("the download cannot be unpacked")

	tests := []struct {
		name    string
		status  int
		postFn  func(string, string) error
		wantErr string
	}{
		{
			name:    "the download is refused",
			status:  http.StatusInternalServerError,
			wantErr: "unexpected status: 500",
		},
		{
			name:    "the download cannot be unpacked",
			status:  http.StatusOK,
			postFn:  func(string, string) error { return unpackFails },
			wantErr: "post-download processing failed: the download cannot be unpacked",
		},
		{
			name:   "unpacking leaves nothing behind when it fails half way",
			status: http.StatusOK,
			postFn: func(_, staged string) error {
				if err := os.WriteFile(staged, []byte("half a tool"), 0600); err != nil {
					return err
				}

				return unpackFails
			},
			wantErr: "post-download processing failed: the download cannot be unpacked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			path, err := EnsureTool(toolAt(serving(t, []byte("version one")), "test-tool", "1.0.0", nil))
			if err != nil {
				t.Fatalf("EnsureTool() for the first version error = %v", err)
			}
			before := identityOf(t, path)

			manifest, err := LoadManifest()
			if err != nil {
				t.Fatalf("LoadManifest() error = %v", err)
			}
			manifest["test-tool"] = ToolInfo{Version: "1.0.0", LastCheck: time.Now().Add(-48 * time.Hour)}
			if err := SaveManifest(manifest); err != nil {
				t.Fatalf("SaveManifest() error = %v", err)
			}

			failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("version two"))
			}))
			defer failing.Close()

			_, err = EnsureTool(toolAt(failing, "test-tool", "2.0.0", tt.postFn))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("EnsureTool() error = %v, want one containing %q", err, tt.wantErr)
			}

			content, err := os.ReadFile(path)
			if err != nil || string(content) != "version one" {
				t.Errorf("the tool holds %q (%v), want it left as %q", content, err, "version one")
			}
			if !os.SameFile(before, identityOf(t, path)) {
				t.Error("the tool is a different file after an install that failed")
			}
			if names := namesIn(t, filepath.Dir(path)); !slices.Equal(names, []string{"test-tool"}) {
				t.Errorf("the tools directory holds %v, want only the tool", names)
			}
		})
	}
}

// A name that cannot be taken, here because a directory holds it, fails the
// install and leaves nothing staged beside it.
func TestEnsureTool_ANameThatCannotBeTaken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binDir, err := GetToolsBinDir()
	if err != nil {
		t.Fatalf("GetToolsBinDir() error = %v", err)
	}
	inTheWay := filepath.Join(binDir, "test-tool")
	if err := os.MkdirAll(inTheWay, 0755); err != nil {
		t.Fatalf("failed to put a directory in the way: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inTheWay, "kept"), []byte("kept"), 0644); err != nil {
		t.Fatalf("failed to fill the directory in the way: %v", err)
	}

	_, err = EnsureTool(toolAt(serving(t, []byte("version one")), "test-tool", "1.0.0", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to install "+inTheWay) {
		t.Fatalf("EnsureTool() error = %v, want the install refused", err)
	}

	if names := namesIn(t, binDir); !slices.Equal(names, []string{"test-tool"}) {
		t.Errorf("the tools directory holds %v, want only what was in the way", names)
	}
	if names := namesIn(t, inTheWay); !slices.Equal(names, []string{"kept"}) {
		t.Errorf("the directory in the way holds %v, want it untouched", names)
	}
}

// wasm-opt is released as a tree, and every file of it is installed the same
// way: unpacked elsewhere, then renamed into place.
func TestEnsureTool_InstallsATreeByRenamingEachFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root, err := GetToolsDir()
	if err != nil {
		t.Fatalf("GetToolsDir() error = %v", err)
	}
	library := filepath.Join(root, "lib", "libbinaryen.dylib")
	if err := os.MkdirAll(filepath.Dir(library), 0755); err != nil {
		t.Fatalf("failed to create the library's directory: %v", err)
	}
	if err := os.WriteFile(library, []byte("the library of an older version"), 0644); err != nil {
		t.Fatalf("failed to install an older library: %v", err)
	}
	before := identityOf(t, library)

	archive := archiveOf(t, map[string]string{
		"bin/wasm-opt":          "the tool",
		"bin/wasm-as":           "another program released with it",
		"lib/libbinaryen.dylib": "the library the tool loads",
	})

	path, err := EnsureTool(toolAt(serving(t, archive), WasmOptName, "1", extractWasmOpt))
	if err != nil {
		t.Fatalf("EnsureTool() error = %v", err)
	}

	want := map[string]string{
		path:                                  "the tool",
		filepath.Join(root, "bin", "wasm-as"): "another program released with it",
		library:                               "the library the tool loads",
	}
	for file, content := range want {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != content {
			t.Errorf("%s holds %q (%v), want %q", file, got, err, content)
		}
	}
	if os.SameFile(before, identityOf(t, library)) {
		t.Error("the library is the same file as before: it was written into, not replaced")
	}

	// Nothing staged or half unpacked is left: bin holds the two programs,
	// and the tools directory holds bin, lib and the manifest.
	if names := namesIn(t, filepath.Join(root, "bin")); !slices.Equal(names, []string{"wasm-as", "wasm-opt"}) {
		t.Errorf("bin holds %v, want the two programs", names)
	}
	for _, name := range namesIn(t, root) {
		if strings.HasPrefix(name, ".unpack-") {
			t.Errorf("the directory the archive was unpacked into was left behind: %s", name)
		}
	}
}

// An archive without the tool installs nothing under the tool's name. Without
// the check, the empty file staged for the tool would be installed as the tool.
func TestExtractWasmOpt_AnArchiveWithoutTheTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	archive := archiveOf(t, map[string]string{"bin/wasm-as": "another program released with it"})

	_, err := EnsureTool(toolAt(serving(t, archive), WasmOptName, "1", extractWasmOpt))
	if err == nil || !strings.Contains(err.Error(), "the archive carries no "+filepath.Join("bin", WasmOptName)) {
		t.Fatalf("EnsureTool() error = %v, want the archive refused", err)
	}

	binDir, err := GetToolsBinDir()
	if err != nil {
		t.Fatalf("GetToolsBinDir() error = %v", err)
	}
	if names := namesIn(t, binDir); slices.Contains(names, WasmOptName) {
		t.Errorf("bin holds %v, want no %s", names, WasmOptName)
	}
}

// A download that is not an archive at all is refused before anything under
// the tools directory is touched.
func TestExtractWasmOpt_ADownloadThatIsNotAnArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := EnsureTool(toolAt(serving(t, []byte("not an archive")), WasmOptName, "1", extractWasmOpt))
	if err == nil {
		t.Fatal("EnsureTool() error = nil, want the download refused")
	}

	root, err := GetToolsDir()
	if err != nil {
		t.Fatalf("GetToolsDir() error = %v", err)
	}
	for _, name := range namesIn(t, root) {
		if strings.HasPrefix(name, ".unpack-") {
			t.Errorf("the directory the archive was unpacked into was left behind: %s", name)
		}
	}
}

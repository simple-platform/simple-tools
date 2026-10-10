package build

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"simple-cli/internal/fsx"
	"simple-cli/internal/home"
	"strings"
	"sync"
	"time"
)

const (
	SimpleToolsDir      = ".simple"
	ManifestFileName    = "tools.json"
	UpdateCheckInterval = 24 * time.Hour
)

type ToolInfo struct {
	Version   string    `json:"version"`
	LastCheck time.Time `json:"lastCheck"`
}

type ToolManifest map[string]ToolInfo

type ToolDef struct {
	Name           string
	CheckVersionFn func() (string, error)
	DownloadURLFn  func(version string) string
	PostDownloadFn func(downloadPath, destPath string) error
	OnStatus       func(status string)
}

func GetToolsDir() (string, error) {
	homeDir, err := home.Dir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, SimpleToolsDir), nil
}

func LoadManifest() (ToolManifest, error) {
	toolsDir, err := GetToolsDir()
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(toolsDir, ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return make(ToolManifest), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest ToolManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	return manifest, nil
}

func SaveManifest(manifest ToolManifest) error {
	toolsDir, err := GetToolsDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return fmt.Errorf("failed to create tools directory: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(toolsDir, ManifestFileName)
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}
	return nil
}

var manifestMu sync.Mutex

func GetToolsBinDir() (string, error) {
	toolsDir, err := GetToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(toolsDir, "bin"), nil
}

func EnsureTool(def ToolDef) (string, error) {
	binDir, err := GetToolsBinDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	manifestMu.Lock()
	manifest, err := LoadManifest()
	if err != nil {
		manifestMu.Unlock()
		return "", err
	}

	toolPath := filepath.Join(binDir, def.Name)
	info, exists := manifest[def.Name]
	manifestMu.Unlock()

	needsCheck := !exists || time.Since(info.LastCheck) > UpdateCheckInterval

	var latestVersion string
	if needsCheck {
		latestVersion, err = def.CheckVersionFn()
		if err != nil {
			return "", fmt.Errorf("failed to check version for %s: %w", def.Name, err)
		}
	} else {
		latestVersion = info.Version
	}

	binaryExists := fileExists(toolPath)
	needsDownload := !binaryExists || (needsCheck && info.Version != latestVersion)

	if needsDownload {
		downloadURL := def.DownloadURLFn(latestVersion)

		onProgress := func(current, total int64) {
			if def.OnStatus != nil && total > 0 {
				percent := float64(current) / float64(total) * 100
				def.OnStatus(fmt.Sprintf("Downloading %.0f%%...", percent))
			}
		}

		if def.OnStatus != nil {
			def.OnStatus("Downloading...")
		}
		if err := downloadTool(downloadURL, toolPath, def.PostDownloadFn, onProgress); err != nil {
			return "", fmt.Errorf("failed to download %s: %w", def.Name, err)
		}
	}

	manifestMu.Lock()
	defer manifestMu.Unlock()

	manifest, err = LoadManifest()
	if err != nil {
		return "", fmt.Errorf("failed to reload manifest: %w", err)
	}
	if manifest == nil {
		manifest = make(ToolManifest)
	}
	manifest[def.Name] = ToolInfo{
		Version:   latestVersion,
		LastCheck: time.Now(),
	}
	if err := SaveManifest(manifest); err != nil {
		return "", err
	}

	return toolPath, nil
}

func downloadTool(url, destPath string, postFn func(string, string) error, onProgress func(int64, int64)) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: failed to close response body: %v\n", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "simple-tool-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("Warning: failed to remove temp file: %v\n", err)
		}
	}()

	reader := &progressReader{
		Reader:     resp.Body,
		total:      resp.ContentLength,
		onProgress: onProgress,
	}

	if _, err := io.Copy(tmpFile, reader); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write download: %w", err)
	}
	_ = tmpFile.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// THE TOOL IS MADE BESIDE ITS DESTINATION AND RENAMED ONTO IT, never
	// written into it.
	//
	// Another process may be running the tool at this moment, or about to:
	// two commands started together both find it missing or out of date, and
	// both come here. Written into in place, the file is there and unfinished
	// for as long as the copy takes, and whoever runs it then is refused or
	// runs half a program. Linux will not start a file that is open for
	// writing, and macOS ties a program to the file it was checked on and
	// stalls or kills a process whose file is rewritten underneath it.
	//
	// A rename gives the name to a finished file in one step. Whoever is
	// running the old one keeps the file they started from, and two installs
	// at once each rename a whole file of their own.
	staged, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".new-*")
	if err != nil {
		return fmt.Errorf("failed to stage %s: %w", destPath, err)
	}
	stagedPath := staged.Name()
	_ = staged.Close()
	defer func() {
		// Already gone when the rename below has happened.
		if err := os.Remove(stagedPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("Warning: failed to remove staged file: %v\n", err)
		}
	}()

	if postFn != nil {
		if err := postFn(tmpPath, stagedPath); err != nil {
			return fmt.Errorf("post-download processing failed: %w", err)
		}
	} else {
		if err := copyFile(tmpPath, stagedPath); err != nil {
			return err
		}
	}

	if err := os.Chmod(stagedPath, fsx.ExecPerm); err != nil {
		return fmt.Errorf("failed to chmod: %w", err)
	}

	if err := os.Rename(stagedPath, destPath); err != nil {
		return fmt.Errorf("failed to install %s: %w", destPath, err)
	}

	return nil
}

type progressReader struct {
	io.Reader
	total      int64
	current    int64
	onProgress func(int64, int64)
	lastUpdate int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		pr.current += int64(n)
		if pr.onProgress != nil {
			shouldUpdate := true
			if pr.total > 0 {
				pct := float64(pr.current) / float64(pr.total) * 100
				lastPct := float64(pr.lastUpdate) / float64(pr.total) * 100
				if pct-lastPct < 1.0 {
					shouldUpdate = false
				}
			}
			if shouldUpdate {
				pr.onProgress(pr.current, pr.total)
				pr.lastUpdate = pr.current
			}
		}
	}
	return n, err
}

func ExtractGzip(srcPath, destPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := src.Close(); err != nil {
			fmt.Printf("Warning: failed to close src: %v\n", err)
		}
	}()

	gr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := gr.Close(); err != nil {
			fmt.Printf("Warning: failed to close gzip reader: %v\n", err)
		}
	}()

	dest, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := dest.Close(); err != nil {
			fmt.Printf("Warning: failed to close dest: %v\n", err)
		}
	}()

	_, err = io.Copy(dest, gr)
	return err
}

func ExtractTarGzFile(srcPath, destPath, targetSuffix string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer func() {
		if err := src.Close(); err != nil {
			fmt.Printf("Warning: failed to close src: %v\n", err)
		}
	}()

	gr, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() {
		if err := gr.Close(); err != nil {
			fmt.Printf("Warning: failed to close gzip reader: %v\n", err)
		}
	}()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		if strings.HasSuffix(header.Name, targetSuffix) && header.Typeflag == tar.TypeReg {
			dest, err := os.Create(destPath)
			if err != nil {
				return fmt.Errorf("failed to create destination file: %w", err)
			}
			defer func() {
				if err := dest.Close(); err != nil {
					fmt.Printf("Warning: failed to close dest: %v\n", err)
				}
			}()

			if _, err := io.Copy(dest, tr); err != nil {
				return fmt.Errorf("failed to extract file: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("file matching %q not found in archive", targetSuffix)
}

func GetPlatform() string {
	return mapPlatform(runtime.GOOS)
}

func mapPlatform(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	default:
		return goos
	}
}

func GetArch() string {
	return mapArch(runtime.GOARCH)
}

func mapArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return goarch
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = srcFile.Close()
	}()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = dstFile.Close()
	}()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

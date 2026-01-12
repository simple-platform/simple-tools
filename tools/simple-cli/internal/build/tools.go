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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, SimpleToolsDir), nil
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

func EnsureTool(def ToolDef) (string, error) {
	toolsDir, err := GetToolsDir()
	if err != nil {
		return "", err
	}

	manifestMu.Lock()
	manifest, err := LoadManifest()
	if err != nil {
		manifestMu.Unlock()
		return "", err
	}

	toolPath := filepath.Join(toolsDir, def.Name)
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "simple-tool-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	reader := &progressReader{
		Reader:     resp.Body,
		total:      resp.ContentLength,
		onProgress: onProgress,
	}

	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write download: %w", err)
	}
	tmpFile.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if postFn != nil {
		if err := postFn(tmpPath, destPath); err != nil {
			return fmt.Errorf("post-download processing failed: %w", err)
		}
	} else {
		if err := copyFile(tmpPath, destPath); err != nil {
			return err
		}
	}

	if err := os.Chmod(destPath, 0755); err != nil {
		return fmt.Errorf("failed to chmod: %w", err)
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
	defer src.Close()

	gr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gr.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, gr)
	return err
}

func ExtractTarGzFile(srcPath, destPath, targetSuffix string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer src.Close()

	gr, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

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
			defer dest.Close()

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
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

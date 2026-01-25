package build

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ExtractTarGz(srcPath, destDir string, stripComponents int) error {
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

		// Strip components
		parts := strings.Split(header.Name, "/")
		if len(parts) <= stripComponents {
			continue
		}
		relPath := filepath.Join(parts[stripComponents:]...)

		// Zip Slip protection
		if strings.Contains(relPath, "..") || strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "\\") {
			continue
		}

		target := filepath.Join(destDir, relPath)
		// Check that target is within destDir (or is destDir itself)
		cleanDest := filepath.Clean(destDir)
		cleanTarget := filepath.Clean(target)
		if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", target)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create directory for file: %w", err)
			}
			f, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to extract file: %w", err)
			}
			_ = f.Close()
			if err := os.Chmod(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to chmod: %w", err)
			}
		}
	}
	return nil
}

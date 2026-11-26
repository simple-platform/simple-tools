package scaffold

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"path"
	"sort"
	"sync"

	"simple-cli/internal/fsx"
)

// DocumentationFile is one shipped assistant document and its repository path.
type DocumentationFile struct {
	Path    string
	Content []byte
}

// DocumentationStamp records the shipped files and the CLI version that wrote them.
type DocumentationStamp struct {
	Fingerprint string   `json:"fingerprint"`
	CLIVersion  string   `json:"cliVersion"`
	Files       []string `json:"files"`
}

var (
	documentationFingerprintOnce sync.Once
	documentationFingerprint     string
	documentationFingerprintErr  error
)

// DocumentationFiles lists the assistant documents with their init destinations.
func DocumentationFiles(tplFS fsx.TemplateFS) ([]DocumentationFile, error) {
	var files []DocumentationFile
	for _, root := range [][2]string{
		{"templates/context", ".simple/context"}, {"templates/agent", ".agent"},
	} {
		if err := collectDocumentationFiles(tplFS, root[0], root[1], &files); err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func collectDocumentationFiles(tplFS fsx.TemplateFS, source, destination string, files *[]DocumentationFile) error {
	entries, err := tplFS.ReadDir(source)
	if err != nil {
		return fmt.Errorf("failed to read documentation templates %s: %w", source, err)
	}
	for _, entry := range entries {
		sourcePath := path.Join(source, entry.Name())
		destinationPath := path.Join(destination, entry.Name())
		if entry.IsDir() {
			if err := collectDocumentationFiles(tplFS, sourcePath, destinationPath, files); err != nil {
				return err
			}
			continue
		}
		content, err := tplFS.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to read documentation template %s: %w", sourcePath, err)
		}
		*files = append(*files, DocumentationFile{Path: destinationPath, Content: content})
	}
	return nil
}

// DocumentationFingerprint returns the shipped documentation fingerprint once per process.
func DocumentationFingerprint() (string, error) {
	documentationFingerprintOnce.Do(func() {
		files, err := DocumentationFiles(TemplatesFS)
		if err != nil {
			documentationFingerprintErr = err
			return
		}
		documentationFingerprint = FingerprintDocumentationFiles(files)
	})
	return documentationFingerprint, documentationFingerprintErr
}

// FingerprintDocumentationFiles hashes the sorted documentation paths and contents.
func FingerprintDocumentationFiles(files []DocumentationFile) string {
	digest := sha256.New()
	writeHashField(digest, []byte("documentation-content"))
	for _, file := range files {
		writeHashField(digest, []byte(file.Path))
		writeHashField(digest, file.Content)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeHashField(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

// MarshalDocumentationStamp encodes sorted documentation paths with stable formatting.
func MarshalDocumentationStamp(fingerprint, cliVersion string, files []DocumentationFile) ([]byte, error) {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	sort.Strings(paths)
	raw, err := json.MarshalIndent(DocumentationStamp{Fingerprint: fingerprint, CLIVersion: cliVersion, Files: paths}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode documentation stamp: %w", err)
	}
	return append(raw, '\n'), nil
}

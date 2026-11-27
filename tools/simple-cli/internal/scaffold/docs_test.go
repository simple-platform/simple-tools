package scaffold

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"simple-cli/internal/fsx"
)

func TestDocumentationFiles(t *testing.T) {
	tests := []struct {
		name string
		fs   *fstest.MapFS
		want []string
	}{
		{
			name: "paths are sorted and rooted at destinations",
			fs: &fstest.MapFS{
				"templates/context/z.md":                {Data: []byte("z")},
				"templates/context/a.md":                {Data: []byte("a")},
				"templates/agent/skills/build/SKILL.md": {Data: []byte("skill")},
				"templates/README.md":                   {Data: []byte("excluded")},
				"templates/AGENTS.md":                   {Data: []byte("excluded")},
				"templates/cursorrules":                 {Data: []byte("excluded")},
			},
			want: []string{
				".agent/skills/build/SKILL.md",
				".simple/context/.cli/AGENTS.template.md",
				".simple/context/.cli/cursorrules.template",
				".simple/context/a.md",
				".simple/context/z.md",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := DocumentationFiles(tt.fs)
			if err != nil {
				t.Fatalf("DocumentationFiles() error = %v", err)
			}
			got := make([]string, len(files))
			for i, file := range files {
				got[i] = file.Path
			}
			if len(got) != len(tt.want) {
				t.Fatalf("paths = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("paths = %v, want %v", got, tt.want)
					break
				}
			}
		})
	}
}

func TestDocumentationFilesIncludeRootTemplateCopies(t *testing.T) {
	files, err := DocumentationFiles(TemplatesFS)
	if err != nil {
		t.Fatalf("DocumentationFiles() error = %v", err)
	}
	want := map[string]string{
		".simple/context/.cli/AGENTS.template.md":   "templates/AGENTS.md",
		".simple/context/.cli/cursorrules.template": "templates/cursorrules",
	}
	for path, template := range want {
		expected, err := TemplatesFS.ReadFile(template)
		if err != nil {
			t.Fatalf("reading %s: %v", template, err)
		}
		found := false
		for _, file := range files {
			if file.Path == path {
				found = true
				if string(file.Content) != string(expected) {
					t.Errorf("%s content differs from %s", path, template)
				}
				break
			}
		}
		if !found {
			t.Errorf("DocumentationFiles() omitted root template copy %s", path)
		}
	}
}

func TestDocumentationFilesRootTemplateReadError(t *testing.T) {
	fsys := &fsx.MockTemplateFS{ReadErrors: map[string]error{
		"templates/AGENTS.md": errors.New("template unavailable"),
	}}
	_, err := DocumentationFiles(fsys)
	if err == nil || !strings.Contains(err.Error(), "failed to read root guidance template templates/AGENTS.md") {
		t.Errorf("DocumentationFiles() error = %v, want root template read error", err)
	}
}

func TestDocumentationTemplateErrors(t *testing.T) {
	tests := []struct {
		name string
		fs   *fsx.MockTemplateFS
		want string
	}{
		{name: "directory read", fs: &fsx.MockTemplateFS{ReadDirErr: errors.New("cannot list")}, want: "failed to read documentation templates"},
		{name: "file read", fs: &fsx.MockTemplateFS{ReadErrors: map[string]error{"templates/context/file.md": errors.New("cannot read")}, Files: map[string][]byte{"templates/context/file.md": []byte("guide")}}, want: "failed to read documentation template"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DocumentationFiles(tt.fs)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("DocumentationFiles() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFingerprintDocumentationFilesChangesWithDocumentSet(t *testing.T) {
	tests := []struct {
		name   string
		change func(*fstest.MapFS)
	}{
		{name: "content edit", change: func(files *fstest.MapFS) { (*files)["templates/context/guide.md"].Data = []byte("changed") }},
		{name: "added file", change: func(files *fstest.MapFS) {
			(*files)["templates/context/extra.md"] = &fstest.MapFile{Data: []byte("extra")}
		}},
		{name: "removed file", change: func(files *fstest.MapFS) { delete(*files, "templates/context/guide.md") }},
		{name: "renamed file", change: func(files *fstest.MapFS) {
			file := (*files)["templates/context/guide.md"]
			delete(*files, "templates/context/guide.md")
			(*files)["templates/context/renamed.md"] = file
		}},
		{name: "root template edit", change: func(files *fstest.MapFS) { (*files)["templates/AGENTS.md"].Data = []byte("different root guidance") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &fstest.MapFS{
				"templates/context/guide.md": {Data: []byte("first")},
				"templates/context/keep.md":  {Data: []byte("keep")},
				"templates/agent/skill.md":   {Data: []byte("skill")},
				"templates/AGENTS.md":        {Data: []byte("root guidance")},
				"templates/cursorrules":      {Data: []byte("cursor rules")},
			}
			baseFiles, err := DocumentationFiles(base)
			if err != nil {
				t.Fatalf("base DocumentationFiles() error = %v", err)
			}
			first := FingerprintDocumentationFiles(baseFiles)
			again := FingerprintDocumentationFiles(baseFiles)
			if first != again {
				t.Fatalf("fingerprints differ: %q and %q", first, again)
			}
			changedFS := &fstest.MapFS{}
			for name, file := range *base {
				copyFile := *file
				copyFile.Data = append([]byte(nil), file.Data...)
				(*changedFS)[name] = &copyFile
			}
			tt.change(changedFS)
			changedFiles, err := DocumentationFiles(changedFS)
			if err != nil {
				t.Fatalf("changed DocumentationFiles() error = %v", err)
			}
			changed := FingerprintDocumentationFiles(changedFiles)
			if changed == first {
				t.Errorf("fingerprint did not change after %s", tt.name)
			}
		})
	}
}

func TestDocumentationFingerprint(t *testing.T) {
	first, err := DocumentationFingerprint()
	if err != nil {
		t.Fatalf("DocumentationFingerprint() error = %v", err)
	}
	again, err := DocumentationFingerprint()
	if err != nil {
		t.Fatalf("second DocumentationFingerprint() error = %v", err)
	}
	if first != again {
		t.Errorf("fingerprints differ: %q and %q", first, again)
	}
}

func TestCreateMonorepoStructureWritesDocsStamp(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "running version is recorded", version: "1.2.3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disk := &fsx.MockFileSystem{Files: make(map[string][]byte)}
			root := filepath.Join("/repo", tt.name)
			err := CreateMonorepoStructure(disk, TemplatesFS, root, MonorepoConfig{
				ProjectName: "example",
				TenantName:  "acme",
				CLIVersion:  tt.version,
			})
			if err != nil {
				t.Fatalf("CreateMonorepoStructure() error = %v", err)
			}

			raw, ok := disk.Files[filepath.Join(root, filepath.FromSlash(DocumentationStampPath))]
			if !ok {
				t.Fatal("docs stamp was not written")
			}
			if _, ok := disk.Files[filepath.Join(root, ".simple/docs.json")]; ok {
				t.Error("docs stamp was written at obsolete path .simple/docs.json")
			}
			var stamp struct {
				Fingerprint string   `json:"fingerprint"`
				CLIVersion  string   `json:"cliVersion"`
				Files       []string `json:"files"`
			}
			if err := json.Unmarshal(raw, &stamp); err != nil {
				t.Fatalf("invalid docs stamp: %v", err)
			}
			if stamp.Fingerprint == "" || stamp.CLIVersion != tt.version || len(stamp.Files) == 0 {
				t.Errorf("stamp = %+v, want fingerprint, version %q, and docs paths", stamp, tt.version)
			}
			for _, path := range []string{".simple/context/.cli/AGENTS.template.md", ".simple/context/.cli/cursorrules.template"} {
				if _, ok := disk.Files[filepath.Join(root, filepath.FromSlash(path))]; !ok {
					t.Errorf("root template copy %s was not written", path)
				}
				if !containsPath(stamp.Files, path) {
					t.Errorf("stamp omits root template copy %s: %v", path, stamp.Files)
				}
			}
			if raw[len(raw)-1] != '\n' {
				t.Error("stamp does not end with a newline")
			}
			for i := 1; i < len(stamp.Files); i++ {
				if stamp.Files[i-1] > stamp.Files[i] {
					t.Errorf("stamp paths are not sorted: %v", stamp.Files)
					break
				}
			}
		})
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func TestMarshalDocumentationStamp(t *testing.T) {
	raw, err := MarshalDocumentationStamp("fingerprint", "2.0.0", []DocumentationFile{
		{Path: ".simple/context/z.md"},
		{Path: ".agent/skill.md"},
	})
	if err != nil {
		t.Fatalf("MarshalDocumentationStamp() error = %v", err)
	}
	want := "{\n  \"fingerprint\": \"fingerprint\",\n  \"cliVersion\": \"2.0.0\",\n  \"files\": [\n    \".agent/skill.md\",\n    \".simple/context/z.md\"\n  ]\n}\n"
	if string(raw) != want {
		t.Errorf("stamp = %q, want %q", raw, want)
	}
}

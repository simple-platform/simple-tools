package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"
)

func docsTestTemplates() *fstest.MapFS {
	return &fstest.MapFS{
		"templates/context/guide.md":             {Data: []byte("current guidance")},
		"templates/agent/skills/tool/SKILL.md":   {Data: []byte("current skill")},
		"templates/AGENTS.md":                    {Data: []byte("current agents")},
		"templates/cursorrules":                  {Data: []byte("current cursor rules")},
		"templates/context/nested/reference.txt": {Data: []byte("nested guidance")},
	}
}

func docsStampJSON(fingerprint, cliVersion string, files ...string) []byte {
	raw, _ := json.Marshal(struct {
		Fingerprint string   `json:"fingerprint"`
		CLIVersion  string   `json:"cliVersion"`
		Files       []string `json:"files"`
	}{fingerprint, cliVersion, files})
	return raw
}

func docsTestDisk(t *testing.T, templates *fstest.MapFS) *fsx.MockFileSystem {
	t.Helper()
	files := map[string][]byte{
		"/repo/simple.scl": []byte("workspace"),
	}
	docs, err := scaffold.DocumentationFiles(templates)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range docs {
		files[filepath.Join("/repo", filepath.FromSlash(file.Path))] = file.Content
	}
	for _, item := range []struct{ source, destination string }{
		{"templates/AGENTS.md", "/repo/AGENTS.md"},
		{"templates/cursorrules", "/repo/.cursorrules"},
	} {
		content, err := templates.ReadFile(item.source)
		if err != nil {
			t.Fatal(err)
		}
		files[item.destination] = content
	}
	return &fsx.MockFileSystem{Files: files}
}

type docsFailureFS struct {
	*fsx.MockFileSystem
	readFailurePath string
}

type docsRemovalFS struct {
	*fsx.MockFileSystem
	directoryPath     string
	nonRegularPath    string
	symlinkPath       string
	statFailurePath   string
	removeFailurePath string
}

type docsRootAccessFS struct {
	*fsx.MockFileSystem
	accessed []string
}

type docsMappedFS struct {
	*fsx.MockFileSystem
	links         map[string]string
	dirs          map[string]bool
	writes        []string
	reads         []string
	evalErrorPath string
}

func newDocsMappedFS(base *fsx.MockFileSystem) *docsMappedFS {
	return &docsMappedFS{
		MockFileSystem: base,
		links:          make(map[string]string),
		dirs: map[string]bool{
			"/repo": true, "/repo/.simple": true, "/repo/.simple/context": true,
			"/repo/.agent": true, "/repo/inside-agent": true,
			"/outside": true, "/outside/agent": true,
		},
	}
}

func (f *docsMappedFS) mapped(name string) (string, error) {
	mapped := filepath.Clean(name)
	seen := make(map[string]bool)
	for attempt := 0; attempt <= len(f.links); attempt++ {
		parts := strings.Split(strings.TrimPrefix(mapped, string(filepath.Separator)), string(filepath.Separator))
		prefix := string(filepath.Separator)
		changed := false
		for i, part := range parts {
			prefix = filepath.Join(prefix, part)
			if target, ok := f.links[prefix]; ok {
				if seen[prefix] {
					return "", fs.ErrInvalid
				}
				seen[prefix] = true
				mapped = filepath.Join(append([]string{target}, parts[i+1:]...)...)
				changed = true
				break
			}
		}
		if !changed {
			return mapped, nil
		}
	}
	return "", fs.ErrInvalid
}

func (f *docsMappedFS) exists(name string) (bool, bool) {
	if f.dirs[name] {
		return true, true
	}
	if _, ok := f.Files[name]; ok {
		return true, false
	}
	for file := range f.Files {
		if strings.HasPrefix(file, name+string(filepath.Separator)) {
			return true, true
		}
	}
	for dir := range f.dirs {
		if strings.HasPrefix(dir, name+string(filepath.Separator)) {
			return true, true
		}
	}
	return false, false
}

func (f *docsMappedFS) EvalSymlinks(name string) (string, error) {
	if name == f.evalErrorPath {
		return "", fs.ErrPermission
	}
	mapped, err := f.mapped(name)
	if err != nil {
		return "", err
	}
	if exists, _ := f.exists(mapped); !exists {
		return "", fs.ErrNotExist
	}
	return mapped, nil
}

func (f *docsMappedFS) Lstat(name string) (fs.FileInfo, error) {
	name = filepath.Clean(name)
	if _, ok := f.links[name]; ok {
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeSymlink | 0777}, nil
	}
	mapped, err := f.mapped(name)
	if err != nil {
		return nil, err
	}
	exists, isDir := f.exists(mapped)
	if !exists {
		return nil, fs.ErrNotExist
	}
	if isDir {
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeDir | 0755}, nil
	}
	return docsTestFileInfo{name: filepath.Base(name), mode: 0644}, nil
}

func (f *docsMappedFS) Stat(name string) (fs.FileInfo, error) {
	mapped, err := f.mapped(name)
	if err != nil {
		return nil, err
	}
	if exists, isDir := f.exists(mapped); exists && isDir {
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeDir | 0755}, nil
	}
	return f.MockFileSystem.Stat(mapped)
}

func (f *docsMappedFS) MkdirAll(name string, perm os.FileMode) error {
	mapped, err := f.mapped(name)
	if err != nil {
		return err
	}
	f.dirs[mapped] = true
	return f.MockFileSystem.MkdirAll(mapped, perm)
}

func (f *docsMappedFS) ReadFile(name string) ([]byte, error) {
	mapped, err := f.mapped(name)
	if err != nil {
		return nil, err
	}
	f.reads = append(f.reads, mapped)
	return f.MockFileSystem.ReadFile(mapped)
}

func (f *docsMappedFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	mapped, err := f.mapped(name)
	if err != nil {
		return err
	}
	f.writes = append(f.writes, mapped)
	return f.MockFileSystem.WriteFile(mapped, data, perm)
}

func (f *docsMappedFS) Remove(name string) error {
	mapped, err := f.mapped(name)
	if err != nil {
		return err
	}
	return f.MockFileSystem.Remove(mapped)
}

func (f *docsRootAccessFS) ReadFile(name string) ([]byte, error) {
	if name == "/repo/AGENTS.md" || name == "/repo/.cursorrules" {
		f.accessed = append(f.accessed, "read "+name)
	}
	return f.MockFileSystem.ReadFile(name)
}

func (f *docsRootAccessFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if name == "/repo/AGENTS.md" || name == "/repo/.cursorrules" {
		f.accessed = append(f.accessed, "write "+name)
	}
	return f.MockFileSystem.WriteFile(name, data, perm)
}

func (f *docsRemovalFS) Stat(name string) (fs.FileInfo, error) {
	if name == f.statFailurePath {
		return nil, fs.ErrPermission
	}
	switch name {
	case f.directoryPath:
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeDir | 0755}, nil
	case f.nonRegularPath:
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeDevice | 0600}, nil
	default:
		return f.MockFileSystem.Stat(name)
	}
}

func (f *docsRemovalFS) Lstat(name string) (fs.FileInfo, error) {
	if name == f.statFailurePath {
		return nil, fs.ErrPermission
	}
	switch name {
	case f.directoryPath:
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeDir | 0755}, nil
	case f.nonRegularPath:
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeDevice | 0600}, nil
	case f.symlinkPath:
		return docsTestFileInfo{name: filepath.Base(name), mode: fs.ModeSymlink | 0777}, nil
	default:
		return f.MockFileSystem.Lstat(name)
	}
}

func (f *docsRemovalFS) ReadDir(name string) ([]os.DirEntry, error) {
	if f.symlinkPath != "" && filepath.Dir(f.symlinkPath) == name {
		return []os.DirEntry{docsTestDirEntry{name: filepath.Base(f.symlinkPath), mode: fs.ModeSymlink}}, nil
	}
	return f.MockFileSystem.ReadDir(name)
}

func (f *docsRemovalFS) Remove(name string) error {
	if name == f.removeFailurePath {
		return fs.ErrPermission
	}
	return f.MockFileSystem.Remove(name)
}

type docsTestFileInfo struct {
	name string
	mode fs.FileMode
}

func (i docsTestFileInfo) Name() string       { return i.name }
func (i docsTestFileInfo) Size() int64        { return 0 }
func (i docsTestFileInfo) Mode() fs.FileMode  { return i.mode }
func (i docsTestFileInfo) ModTime() time.Time { return time.Time{} }
func (i docsTestFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i docsTestFileInfo) Sys() any           { return nil }

type docsTestDirEntry struct {
	name string
	mode fs.FileMode
}

func (e docsTestDirEntry) Name() string      { return e.name }
func (e docsTestDirEntry) IsDir() bool       { return e.mode.IsDir() }
func (e docsTestDirEntry) Type() fs.FileMode { return e.mode }
func (e docsTestDirEntry) Info() (fs.FileInfo, error) {
	return docsTestFileInfo(e), nil
}

func (f *docsFailureFS) ReadFile(name string) ([]byte, error) {
	if name == f.readFailurePath {
		return nil, fs.ErrPermission
	}
	return f.MockFileSystem.ReadFile(name)
}

func TestRunDocsUpdate(t *testing.T) {
	tests := []struct {
		name  string
		stamp bool
	}{
		{name: "writes changed docs, removes only recorded obsolete docs, and preserves additions", stamp: true},
		{name: "missing stamp removes nothing", stamp: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templates := docsTestTemplates()
			files := map[string][]byte{
				"/repo/simple.scl":                           []byte("workspace"),
				"/repo/.simple/context/guide.md":             []byte("old guidance"),
				"/repo/.simple/context/nested/reference.txt": []byte("nested guidance"),
				"/repo/.simple/context/local.md":             []byte("team context"),
				"/repo/.agent/skills/tool/SKILL.md":          []byte("old skill"),
				"/repo/.agent/team.md":                       []byte("team agent note"),
				"/repo/.agent/obsolete.md":                   []byte("old shipped doc"),
				"/repo/.simple/context/obsolete.md":          []byte("old shipped context"),
				"/outside.md":                                []byte("outside file"),
				"/repo/AGENTS.md":                            []byte("team agents"),
				"/repo/.cursorrules":                         []byte("current cursor rules"),
				"/repo/package.json":                         []byte("team package settings"),
			}
			if !tt.stamp {
				files["/repo/.cursorrules"] = []byte("team cursor rules")
			} else {
				files["/repo/.simple/docs.json"] = docsStampJSON("previous", "1.0.0",
					".agent/obsolete.md", ".agent/obsolete.md", ".agent/skills/tool/SKILL.md", ".agent/gone.md", ".agent/../AGENTS.md",
					"/outside.md", ".simple/context/../../simple.scl", ".simple/context/obsolete.md")
			}
			disk := &fsx.MockFileSystem{Files: files}
			out := new(bytes.Buffer)
			deps := docsUpdateDeps{fsys: disk, templates: templates, cwd: "/repo/subdir", cliVersion: "2.0.0"}

			if err := runDocsUpdate(out, deps, false); err != nil {
				t.Fatalf("runDocsUpdate() error = %v", err)
			}
			if got, want := string(disk.Files["/repo/.simple/context/guide.md"]), "current guidance"; got != want {
				t.Errorf("context content = %q, want %q", got, want)
			}
			if got, want := string(disk.Files["/repo/.agent/skills/tool/SKILL.md"]), "current skill"; got != want {
				t.Errorf("skill content = %q, want %q", got, want)
			}
			for _, path := range []string{"/repo/.agent/team.md", "/repo/.simple/context/local.md", "/repo/package.json", "/outside.md", "/repo/simple.scl"} {
				if _, ok := disk.Files[path]; !ok {
					t.Errorf("unmanaged file %s was removed", path)
				}
			}
			if !tt.stamp {
				for _, path := range []string{"/repo/.agent/obsolete.md", "/repo/.simple/context/obsolete.md"} {
					if _, ok := disk.Files[path]; !ok {
						t.Errorf("file %s was removed without an old stamp", path)
					}
				}
			}
			if _, ok := disk.Files["/repo/.agent/obsolete.md"]; ok && tt.stamp {
				t.Error("obsolete agent doc from the old stamp was not removed")
			}
			if _, ok := disk.Files["/repo/.simple/context/obsolete.md"]; ok && tt.stamp {
				t.Error("obsolete context doc from the old stamp was not removed")
			}
			if got := string(disk.Files["/repo/AGENTS.md"]); got != "team agents" {
				t.Errorf("AGENTS.md changed to %q", got)
			}
			if strings.Contains(out.String(), "package.json") {
				t.Errorf("output mentioned a package file: %q", out.String())
			}
			if strings.Contains(out.String(), "nested/reference.txt") {
				t.Errorf("matching nested documentation was reported as updated: %q", out.String())
			}
			if strings.Contains(out.String(), "differs") || strings.Contains(out.String(), "AGENTS.md differs") {
				t.Errorf("output reports team-owned root files: %q", out.String())
			}
			if !strings.Contains(out.String(), "Updated .simple/context/guide.md") || !strings.Contains(out.String(), "Updated .simple/docs.json") {
				t.Errorf("output did not list updated files: %q", out.String())
			}
		})
	}
}

func TestRunDocsUpdateSecondRun(t *testing.T) {
	tests := []struct {
		name string
		json bool
		want string
	}{
		{name: "text", want: "Docs are already up to date.\n"},
		{name: "JSON", json: true, want: "{\n  \"updated\": [],\n  \"removed\": [],\n  \"upToDate\": true\n}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templates := docsTestTemplates()
			disk := &fsx.MockFileSystem{Files: map[string][]byte{
				"/repo/simple.scl":                           []byte("workspace"),
				"/repo/AGENTS.md":                            []byte("current agents"),
				"/repo/.cursorrules":                         []byte("current cursor rules"),
				"/repo/package.json":                         []byte("must stay"),
				"/repo/.simple/context/guide.md":             []byte("current guidance"),
				"/repo/.agent/skills/tool/SKILL.md":          []byte("current skill"),
				"/repo/.simple/context/nested/reference.txt": []byte("nested guidance"),
			}}
			deps := docsUpdateDeps{fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0"}
			if err := runDocsUpdate(new(bytes.Buffer), deps, false); err != nil {
				t.Fatalf("first run error = %v", err)
			}
			out := new(bytes.Buffer)
			if err := runDocsUpdate(out, deps, tt.json); err != nil {
				t.Fatalf("second run error = %v", err)
			}
			if out.String() != tt.want {
				t.Errorf("second run output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestRunDocsUpdateOutsideRepository(t *testing.T) {
	disk := &fsx.MockFileSystem{Files: make(map[string][]byte)}
	err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{
		fsys: disk, templates: docsTestTemplates(), cwd: "/outside", cliVersion: "2.0.0",
	}, false)
	if err == nil || err.Error() != "run simple docs update inside a repository made with simple init" {
		t.Errorf("runDocsUpdate() error = %v, want one plain repository instruction", err)
	}
}

func TestDocsUpdateRefusesStampFromNewerVersion(t *testing.T) {
	templates := docsTestTemplates()
	disk := docsTestDisk(t, templates)
	stampPath := "/repo/.simple/docs.json"
	disk.Files[stampPath] = docsStampJSON("different", "9.0.0", ".simple/context/guide.md")
	before := make(map[string][]byte, len(disk.Files))
	for name, content := range disk.Files {
		before[name] = append([]byte(nil), content...)
	}

	err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{
		fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0",
	}, false)
	if err == nil || !strings.Contains(err.Error(), "9.0.0") || !strings.Contains(err.Error(), "2.0.0") || !strings.Contains(err.Error(), "simple version update") {
		t.Fatalf("runDocsUpdate() error = %v, want newer-version instruction naming both versions", err)
	}
	if !equalFileMaps(disk.Files, before) {
		t.Error("files changed after refusing a stamp from a newer CLI")
	}
}

func TestDocsReminderSilentWhenStampIsNewer(t *testing.T) {
	disk := &fsx.MockFileSystem{Files: map[string][]byte{
		"/repo/simple.scl":        []byte("workspace"),
		"/repo/.simple/docs.json": docsStampJSON("newer-docs", "9.0.0"),
	}}
	out := new(bytes.Buffer)
	announceDocsUpdateTo(out, "2.0.0", false, true, "", "simple build", disk,
		func() (string, error) { return "/repo", nil }, func() (string, error) { return "older-docs", nil })
	if out.Len() != 0 {
		t.Errorf("docs reminder for a newer stamp = %q, want nothing", out.String())
	}
}

func TestDocumentationStampIsNewer(t *testing.T) {
	tests := []struct {
		name           string
		stampVersion   string
		runningVersion string
		want           bool
	}{
		{name: "newer release", stampVersion: "9.0.0", runningVersion: "2.0.0", want: true},
		{name: "older release", stampVersion: "1.0.0", runningVersion: "2.0.0"},
		{name: "unknown stamp", stampVersion: "", runningVersion: "2.0.0"},
		{name: "development stamp", stampVersion: "dev", runningVersion: "2.0.0"},
		{name: "development running version", stampVersion: "9.0.0", runningVersion: "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := documentationStampIsNewer(tt.stampVersion, tt.runningVersion); got != tt.want {
				t.Errorf("documentationStampIsNewer(%q, %q) = %v, want %v", tt.stampVersion, tt.runningVersion, got, tt.want)
			}
		})
	}
}

func TestDocsUpdateLeavesStampWhenNothingChanged(t *testing.T) {
	templates := docsTestTemplates()
	disk := docsTestDisk(t, templates)
	files, err := scaffold.DocumentationFiles(templates)
	if err != nil {
		t.Fatalf("DocumentationFiles() error = %v", err)
	}
	fingerprint := scaffold.FingerprintDocumentationFiles(files)
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	stampPath := "/repo/.simple/docs.json"
	disk.Files[stampPath] = docsStampJSON(fingerprint, "1.0.0", paths...)
	before := append([]byte(nil), disk.Files[stampPath]...)
	out := new(bytes.Buffer)
	if err := runDocsUpdate(out, docsUpdateDeps{
		fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0",
	}, false); err != nil {
		t.Fatalf("runDocsUpdate() error = %v", err)
	}
	if !bytes.Equal(disk.Files[stampPath], before) {
		t.Errorf("stamp changed from %q to %q", before, disk.Files[stampPath])
	}
	if out.String() != "Docs are already up to date.\n" {
		t.Errorf("output = %q, want up-to-date message", out.String())
	}
}

func TestDocsUpdateRewritesStampWhenFileListDiffers(t *testing.T) {
	templates := docsTestTemplates()
	disk := docsTestDisk(t, templates)
	files, err := scaffold.DocumentationFiles(templates)
	if err != nil {
		t.Fatalf("DocumentationFiles() error = %v", err)
	}
	stampPath := "/repo/.simple/docs.json"
	disk.Files[stampPath] = docsStampJSON(scaffold.FingerprintDocumentationFiles(files), "1.0.0", files[0].Path)
	if err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{
		fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0",
	}, false); err != nil {
		t.Fatalf("runDocsUpdate() error = %v", err)
	}
	var stamp scaffold.DocumentationStamp
	if err := json.Unmarshal(disk.Files[stampPath], &stamp); err != nil {
		t.Fatalf("stamp is invalid: %v", err)
	}
	if len(stamp.Files) != len(files) || stamp.CLIVersion != "2.0.0" {
		t.Errorf("stamp = %+v, want every shipped file and version 2.0.0", stamp)
	}
}

func TestDocsUpdateLeavesRootFilesAlone(t *testing.T) {
	templates := docsTestTemplates()
	base := docsTestDisk(t, templates)
	base.Files["/repo/AGENTS.md"] = []byte("team-owned AGENTS")
	base.Files["/repo/.cursorrules"] = []byte("team-owned cursor rules")
	base.Files["/repo/.simple/context/AGENTS.template.md"] = []byte("old root reference")
	base.Files["/repo/.simple/context/cursorrules.template"] = []byte("old cursor reference")
	base.Files["/repo/.simple/docs.json"] = docsStampJSON("old fingerprint", "1.0.0",
		".simple/context/AGENTS.template.md", ".simple/context/cursorrules.template")
	disk := &docsRootAccessFS{MockFileSystem: base}
	deps := docsUpdateDeps{fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0"}
	out := new(bytes.Buffer)
	if err := runDocsUpdate(out, deps, false); err != nil {
		t.Fatalf("first runDocsUpdate() error = %v", err)
	}
	if got := string(disk.Files["/repo/AGENTS.md"]); got != "team-owned AGENTS" {
		t.Errorf("AGENTS.md = %q, want team content", got)
	}
	if got := string(disk.Files["/repo/.cursorrules"]); got != "team-owned cursor rules" {
		t.Errorf(".cursorrules = %q, want team content", got)
	}
	for path, want := range map[string]string{
		"/repo/.simple/context/AGENTS.template.md":   "current agents",
		"/repo/.simple/context/cursorrules.template": "current cursor rules",
	} {
		if got := string(disk.Files[path]); got != want {
			t.Errorf("reference copy %s = %q, want %q", path, got, want)
		}
	}
	if len(disk.accessed) != 0 {
		t.Errorf("root guidance files were accessed: %v", disk.accessed)
	}
	const closing = "AGENTS.md and .cursorrules are your team's own files and were not changed. Current templates are in .simple/context/AGENTS.template.md and .simple/context/cursorrules.template.\n"
	if !strings.HasSuffix(out.String(), closing) {
		t.Errorf("first run output does not end with root template guidance: %q", out.String())
	}
	second := new(bytes.Buffer)
	if err := runDocsUpdate(second, deps, false); err != nil {
		t.Fatalf("second runDocsUpdate() error = %v", err)
	}
	if second.String() != "Docs are already up to date.\n" {
		t.Errorf("second run output = %q, want up-to-date message", second.String())
	}
	if len(disk.accessed) != 0 {
		t.Errorf("root guidance files were accessed: %v", disk.accessed)
	}
}

func TestDocsUpdateRefusesPathsOutsideRepository(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T, *docsMappedFS)
		wantPath  string
		inside    bool
	}{
		{
			name: "managed folder leads outside and old stamp lists victim",
			configure: func(t *testing.T, disk *docsMappedFS) {
				disk.links["/repo/.agent"] = "/outside/agent"
				disk.Files["/outside/agent/victim"] = []byte("keep outside victim")
				disk.Files["/repo/.simple/docs.json"] = docsStampJSON("old", "1.0.0", ".agent/victim")
			},
			wantPath: ".agent/victim",
		},
		{
			name: "shipped file link leads outside",
			configure: func(t *testing.T, disk *docsMappedFS) {
				disk.links["/repo/.simple/context/guide.md"] = "/outside/guide.md"
				disk.Files["/outside/guide.md"] = []byte("outside target")
			},
			wantPath: ".simple/context/guide.md",
		},
		{
			name: "stamp link leads outside",
			configure: func(t *testing.T, disk *docsMappedFS) {
				disk.links["/repo/.simple/docs.json"] = "/outside/docs.json"
				disk.Files["/outside/docs.json"] = docsStampJSON("old", "1.0.0")
			},
			wantPath: ".simple/docs.json",
		},
		{
			name: "dangling shipped file link",
			configure: func(t *testing.T, disk *docsMappedFS) {
				disk.links["/repo/.agent/skills/tool/SKILL.md"] = "/outside/missing"
			},
			wantPath: ".agent/skills/tool/SKILL.md",
		},
		{
			name: "cannot resolve shipped file",
			configure: func(_ *testing.T, disk *docsMappedFS) {
				disk.evalErrorPath = "/repo/.simple/context/guide.md"
			},
			wantPath: ".simple/context/guide.md",
		},
		{
			name: "cannot resolve repository root",
			configure: func(_ *testing.T, disk *docsMappedFS) {
				disk.evalErrorPath = "/repo"
			},
			wantPath: "repository's real location",
		},
		{
			name: "managed folder links inside repository",
			configure: func(_ *testing.T, disk *docsMappedFS) {
				disk.links["/repo/.agent"] = "/repo/inside-agent"
			},
			inside: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templates := docsTestTemplates()
			base := docsTestDisk(t, templates)
			disk := newDocsMappedFS(base)
			tt.configure(t, disk)
			before := make(map[string][]byte, len(disk.Files))
			for name, content := range disk.Files {
				before[name] = append([]byte(nil), content...)
			}
			out := new(bytes.Buffer)
			err := runDocsUpdate(out, docsUpdateDeps{
				fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0",
			}, false)
			if tt.inside {
				if err != nil {
					t.Fatalf("runDocsUpdate() error = %v, want internal link to work", err)
				}
				if _, ok := disk.Files["/repo/inside-agent/skills/tool/SKILL.md"]; !ok {
					t.Error("shipped files were not written through the internal link")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantPath) || !strings.Contains(err.Error(), "changed nothing") {
				t.Fatalf("runDocsUpdate() error = %v, want one refusal naming %s and saying nothing changed", err, tt.wantPath)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("refusal is not one plain line: %q", err.Error())
			}
			if len(disk.writes) != 0 || !equalFileMaps(disk.Files, before) {
				t.Errorf("files were changed or written after refusal; writes = %v", disk.writes)
			}
			if tt.name == "managed folder leads outside and old stamp lists victim" {
				if got := string(disk.Files["/outside/agent/victim"]); got != "keep outside victim" {
					t.Errorf("outside victim = %q, want preserved", got)
				}
			}
		})
	}
}

func equalFileMaps(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for name, content := range left {
		if !bytes.Equal(content, right[name]) {
			return false
		}
	}
	return true
}

func TestRunDocsUpdateJSONChanges(t *testing.T) {
	templates := docsTestTemplates()
	disk := &fsx.MockFileSystem{Files: map[string][]byte{
		"/repo/simple.scl":   []byte("workspace"),
		"/repo/AGENTS.md":    []byte("current agents"),
		"/repo/.cursorrules": []byte("current cursor rules"),
	}}
	out := new(bytes.Buffer)
	if err := runDocsUpdate(out, docsUpdateDeps{fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0"}, true); err != nil {
		t.Fatalf("runDocsUpdate() error = %v", err)
	}
	var result docsUpdateResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON result: %v", err)
	}
	if result.UpToDate || !containsString(result.Updated, ".simple/context/guide.md") || !containsString(result.Updated, ".simple/docs.json") {
		t.Errorf("result = %+v, want updated docs and a stale result", result)
	}
}

func TestRunDocsUpdateSkipsUnremovableStampEntries(t *testing.T) {
	tests := []struct {
		name       string
		stampPath  string
		filePath   string
		configure  func(*docsRemovalFS)
		wantRetain []string
		wantReport bool
	}{
		{
			name:      "directory with files",
			stampPath: ".agent/rules",
			configure: func(fsys *docsRemovalFS) {
				fsys.directoryPath = "/repo/.agent/rules"
			},
			wantRetain: []string{"/repo/.agent/rules/custom.md"},
		},
		{
			name:      "missing file",
			stampPath: ".agent/missing.md",
		},
		{
			name:      "removal fails",
			stampPath: ".agent/locked.md",
			filePath:  "/repo/.agent/locked.md",
			configure: func(fsys *docsRemovalFS) {
				fsys.removeFailurePath = "/repo/.agent/locked.md"
			},
			wantRetain: []string{"/repo/.agent/locked.md"},
			wantReport: true,
		},
		{
			name:      "cannot inspect file",
			stampPath: ".agent/unavailable.md",
			filePath:  "/repo/.agent/unavailable.md",
			configure: func(fsys *docsRemovalFS) {
				fsys.statFailurePath = "/repo/.agent/unavailable.md"
			},
			wantRetain: []string{"/repo/.agent/unavailable.md"},
			wantReport: true,
		},
		{
			name:      "symbolic link",
			stampPath: ".agent/link.md",
			filePath:  "/repo/.agent/link.md",
			configure: func(fsys *docsRemovalFS) {
				fsys.symlinkPath = "/repo/.agent/link.md"
			},
			wantRetain: []string{"/repo/.agent/link.md"},
		},
		{
			name:      "non-regular file",
			stampPath: ".agent/device",
			filePath:  "/repo/.agent/device",
			configure: func(fsys *docsRemovalFS) {
				fsys.nonRegularPath = "/repo/.agent/device"
			},
			wantRetain: []string{"/repo/.agent/device"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templates := docsTestTemplates()
			base := docsTestDisk(t, templates)
			base.Files["/repo/.simple/context/guide.md"] = []byte("old guidance")
			base.Files["/repo/.simple/docs.json"] = docsStampJSON("previous", "1.0.0", tt.stampPath, ".agent/obsolete.md")
			base.Files["/repo/.agent/obsolete.md"] = []byte("old obsolete guidance")
			base.Files["/repo/.agent/unlisted.md"] = []byte("local guidance")
			base.Files["/repo/package.json"] = []byte("local package settings")
			for _, retained := range tt.wantRetain {
				base.Files[retained] = []byte("must remain")
			}
			if tt.name == "directory with files" {
				base.Files["/repo/.agent/rules/custom.md"] = []byte("nested local guidance")
			}
			if tt.filePath != "" && tt.filePath != "/repo/.agent/obsolete.md" {
				base.Files[tt.filePath] = []byte("must remain")
			}
			fsys := &docsRemovalFS{MockFileSystem: base}
			if tt.configure != nil {
				tt.configure(fsys)
			}
			out := new(bytes.Buffer)
			deps := docsUpdateDeps{fsys: fsys, templates: templates, cwd: "/repo", cliVersion: "2.0.0"}

			if err := runDocsUpdate(out, deps, false); err != nil {
				t.Fatalf("runDocsUpdate() error = %v", err)
			}
			if got := string(base.Files["/repo/.simple/context/guide.md"]); got != "current guidance" {
				t.Errorf("guide = %q, want updated content", got)
			}
			if _, ok := base.Files["/repo/.agent/obsolete.md"]; ok {
				t.Error("obsolete file from old stamp was not removed")
			}
			for _, retained := range append(tt.wantRetain, "/repo/.agent/unlisted.md", "/repo/package.json") {
				if _, ok := base.Files[retained]; !ok {
					t.Errorf("file %s was removed", retained)
				}
			}
			var stamp scaffold.DocumentationStamp
			if err := json.Unmarshal(base.Files["/repo/.simple/docs.json"], &stamp); err != nil {
				t.Fatalf("new documentation stamp is invalid: %v", err)
			}
			if stamp.CLIVersion != "2.0.0" {
				t.Errorf("stamp CLI version = %q, want 2.0.0", stamp.CLIVersion)
			}
			if tt.wantReport && !strings.Contains(out.String(), tt.stampPath) {
				t.Errorf("output = %q, want failed path %s reported", out.String(), tt.stampPath)
			}
		})
	}
}

func TestRunDocsUpdateStampReadFails(t *testing.T) {
	templates := docsTestTemplates()
	base := docsTestDisk(t, templates)
	stampPath := "/repo/.simple/docs.json"
	base.Files[stampPath] = docsStampJSON("unreadable", "1.0.0")
	disk := &docsFailureFS{MockFileSystem: base, readFailurePath: stampPath}
	err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{
		fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0",
	}, false)
	if err == nil || !strings.Contains(err.Error(), "failed to read documentation stamp") {
		t.Fatalf("runDocsUpdate() error = %v, want the stamp read failure reported", err)
	}
}

func TestRunDocsUpdateFilesystemErrors(t *testing.T) {
	tests := []struct {
		name string
		fs   func(*testing.T, *fstest.MapFS) docsUpdateFS
		want string
	}{
		{
			name: "shipped doc read fails",
			fs: func(t *testing.T, templates *fstest.MapFS) docsUpdateFS {
				return &docsFailureFS{MockFileSystem: docsTestDisk(t, templates), readFailurePath: "/repo/.simple/context/guide.md"}
			},
			want: "failed to read documentation .simple/context/guide.md",
		},
		{
			name: "documentation directory creation fails",
			fs: func(_ *testing.T, _ *fstest.MapFS) docsUpdateFS {
				return &fsx.MockFileSystem{Files: map[string][]byte{"/repo/simple.scl": []byte("workspace")}, MkdirAllErr: fs.ErrPermission}
			},
			want: "failed to create documentation directory",
		},
		{
			name: "stamp directory creation fails",
			fs: func(t *testing.T, templates *fstest.MapFS) docsUpdateFS {
				base := docsTestDisk(t, templates)
				base.MkdirAllErr = fs.ErrPermission
				return base
			},
			want: "failed to create documentation stamp directory",
		},
		{
			name: "stamp write fails",
			fs: func(t *testing.T, templates *fstest.MapFS) docsUpdateFS {
				base := docsTestDisk(t, templates)
				base.WriteFileErr = fs.ErrPermission
				return base
			},
			want: "failed to write documentation stamp",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templates := docsTestTemplates()
			disk := tt.fs(t, templates)
			err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0"}, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("runDocsUpdate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunDocsUpdateMalformedStampDoesNotRemoveFiles(t *testing.T) {
	disk := &fsx.MockFileSystem{Files: map[string][]byte{
		"/repo/simple.scl":                           []byte("workspace"),
		"/repo/.simple/docs.json":                    []byte("not json"),
		"/repo/.agent/team.md":                       []byte("team file"),
		"/repo/AGENTS.md":                            []byte("current agents"),
		"/repo/.cursorrules":                         []byte("current cursor rules"),
		"/repo/.simple/context/guide.md":             []byte("current guidance"),
		"/repo/.simple/context/nested/reference.txt": []byte("nested guidance"),
		"/repo/.agent/skills/tool/SKILL.md":          []byte("current skill"),
	}}
	if err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{fsys: disk, templates: docsTestTemplates(), cwd: "/repo", cliVersion: "2.0.0"}, false); err != nil {
		t.Fatalf("runDocsUpdate() error = %v", err)
	}
	if got := string(disk.Files["/repo/.agent/team.md"]); got != "team file" {
		t.Errorf("team file = %q, want unchanged", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDocsUpdateErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		deps docsUpdateDeps
		want string
	}{
		{
			name: "cannot read shipped docs",
			deps: docsUpdateDeps{fsys: &fsx.MockFileSystem{Files: map[string][]byte{"/repo/simple.scl": []byte("workspace")}}, templates: &fsx.MockTemplateFS{ReadDirErr: errors.New("templates unavailable")}, cwd: "/repo"},
			want: "failed to read documentation templates",
		},
		{
			name: "write failure is returned",
			deps: docsUpdateDeps{fsys: &fsx.MockFileSystem{Files: map[string][]byte{"/repo/simple.scl": []byte("workspace")}, WriteFileErr: fs.ErrPermission}, templates: docsTestTemplates(), cwd: "/repo"},
			want: "failed to write",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runDocsUpdate(new(bytes.Buffer), tt.deps, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("runDocsUpdate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestInitStampMakesDocsReminderSilent(t *testing.T) {
	disk := &fsx.MockFileSystem{Files: make(map[string][]byte)}
	if err := scaffold.CreateMonorepoStructure(disk, scaffold.TemplatesFS, "/repo", scaffold.MonorepoConfig{
		ProjectName: "example", TenantName: "acme", CLIVersion: "2.0.0",
	}); err != nil {
		t.Fatalf("CreateMonorepoStructure() error = %v", err)
	}
	fingerprint, err := scaffold.DocumentationFingerprint()
	if err != nil {
		t.Fatalf("DocumentationFingerprint() error = %v", err)
	}
	out := new(bytes.Buffer)
	announceDocsUpdateTo(out, "2.0.0", false, true, "", "simple build", disk,
		func() (string, error) { return "/repo", nil }, func() (string, error) { return fingerprint, nil })
	if out.Len() != 0 {
		t.Errorf("docs reminder after init = %q, want nothing", out.String())
	}
}

func TestDocsUpdateWritesStampAndStopsReminder(t *testing.T) {
	templates := docsTestTemplates()
	(*templates)["templates/context/guide.md"] = &fstest.MapFile{Data: []byte("changed shipped guidance")}
	previousFingerprint := "previous"
	documentationFiles, err := scaffold.DocumentationFiles(templates)
	if err != nil {
		t.Fatalf("DocumentationFiles() error = %v", err)
	}
	fingerprint := scaffold.FingerprintDocumentationFiles(documentationFiles)
	disk := &fsx.MockFileSystem{Files: map[string][]byte{
		"/repo/simple.scl":        []byte("workspace"),
		"/repo/AGENTS.md":         []byte("current agents"),
		"/repo/.cursorrules":      []byte("current cursor rules"),
		"/repo/.simple/docs.json": docsStampJSON(previousFingerprint, "1.0.0"),
	}}
	reminder := new(bytes.Buffer)
	announceDocsUpdateTo(reminder, "2.0.0", false, true, "", "simple build", disk,
		func() (string, error) { return "/repo", nil }, func() (string, error) { return fingerprint, nil })
	if reminder.String() != docsUpdateNotice {
		t.Fatalf("stale docs reminder = %q, want notice", reminder.String())
	}
	if err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{
		fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0",
	}, false); err != nil {
		t.Fatalf("runDocsUpdate() error = %v", err)
	}
	reminder.Reset()
	announceDocsUpdateTo(reminder, "2.0.0", false, true, "", "simple build", disk,
		func() (string, error) { return "/repo", nil }, func() (string, error) { return fingerprint, nil })
	if reminder.Len() != 0 {
		t.Errorf("reminder remained after docs update: %q", reminder.String())
	}
}

func TestAnnounceDocsUpdateTo(t *testing.T) {
	tests := []struct {
		name        string
		cwd         string
		json        bool
		terminal    bool
		ci          string
		commandPath string
		version     string
		stamp       []byte
		fingerprint string
		stampErr    bool
		statErr     bool
		cwdErr      error
		outside     bool
		fingerErr   error
		want        string
	}{
		{name: "stale docs", terminal: true, commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0"), fingerprint: "current", want: docsUpdateNotice},
		{name: "nested working directory", cwd: "/repo/apps/app1", terminal: true, commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0"), fingerprint: "current", want: docsUpdateNotice},
		{name: "missing stamp", terminal: true, commandPath: "simple build", version: "2.0.0", fingerprint: "current", want: docsUpdateNotice},
		{name: "malformed stamp", terminal: true, commandPath: "simple build", version: "2.0.0", stamp: []byte("not json"), fingerprint: "current", want: docsUpdateNotice},
		{name: "unreadable stamp", terminal: true, commandPath: "simple build", version: "2.0.0", stampErr: true, fingerprint: "current", want: docsUpdateNotice},
		{name: "matching fingerprint", terminal: true, commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("current", "1.0.0"), fingerprint: "current"},
		{name: "fingerprint failure means stale", terminal: true, commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("current", "1.0.0"), fingerErr: errors.New("fingerprint failed"), want: docsUpdateNotice},
		{name: "JSON output", json: true, terminal: true, commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "stderr is not a terminal", commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "CI", terminal: true, ci: "true", commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "false CI", terminal: true, ci: "false", commandPath: "simple build", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0"), want: docsUpdateNotice},
		{name: "unreleased build", terminal: true, commandPath: "simple build", version: "dev", stamp: docsStampJSON("old", "1.0.0")},
		{name: "quiet version command", terminal: true, commandPath: "simple version", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "quiet help command", terminal: true, commandPath: "simple help", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "quiet completion command", terminal: true, commandPath: "simple completion zsh", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "init is quiet", terminal: true, commandPath: "simple init", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "docs is quiet", terminal: true, commandPath: "simple docs update", version: "2.0.0", stamp: docsStampJSON("old", "1.0.0")},
		{name: "working directory failure", terminal: true, commandPath: "simple build", version: "2.0.0", cwdErr: errors.New("cwd failed")},
		{name: "repository marker read error is silent", terminal: true, commandPath: "simple build", version: "2.0.0", statErr: true},
		{name: "outside repository", terminal: true, commandPath: "simple build", version: "2.0.0", outside: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{}
			if tt.stamp != nil {
				files["/repo/.simple/docs.json"] = tt.stamp
			}
			if !tt.outside {
				files["/repo/simple.scl"] = []byte("workspace")
			}
			disk := &fsx.MockFileSystem{Files: files}
			if tt.stampErr {
				disk.ReadFileErr = fs.ErrPermission
			}
			if tt.statErr {
				disk.StatErr = errors.New("marker unavailable")
			}
			out := new(bytes.Buffer)
			cwd := tt.cwd
			if cwd == "" {
				cwd = "/repo"
			}
			announceDocsUpdateTo(out, tt.version, tt.json, tt.terminal, tt.ci, tt.commandPath, disk,
				func() (string, error) { return cwd, tt.cwdErr },
				func() (string, error) { return tt.fingerprint, tt.fingerErr })
			if out.String() != tt.want {
				t.Errorf("notice = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestDocsUpdateHelp(t *testing.T) {
	if strings.Count(docsUpdateCmd.Short, " ") < 3 || !strings.Contains(docsUpdateCmd.Short, "guidance") || !strings.Contains(docsUpdateCmd.Short, "never touch packages") {
		t.Errorf("docs update help = %q, want one plain line describing guidance and package boundary", docsUpdateCmd.Short)
	}
}

func TestManagedDocumentationPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: ".agent/skills/tool/SKILL.md", want: true},
		{path: ".simple/context/guide.md", want: true},
		{path: "../x"},
		{path: "AGENTS.md"},
		{path: ".agent/../AGENTS.md"},
		{path: ".simple/context/../../simple.scl"},
		{path: "/outside.md"},
		{path: ".agent\\team.md"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := managedDocumentationPath(tt.path); got != tt.want {
				t.Errorf("managedDocumentationPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestDocsUpdateSummary(t *testing.T) {
	tests := []struct {
		name    string
		updated int
		removed int
		want    string
	}{
		{
			name:    "count 0 for all fields",
			updated: 0,
			removed: 0,
			want:    "Summary: updated 0 files, removed 0 files.",
		},
		{
			name:    "count 1 for all fields",
			updated: 1,
			removed: 1,
			want:    "Summary: updated 1 file, removed 1 file.",
		},
		{
			name:    "count 2 for all fields",
			updated: 2,
			removed: 2,
			want:    "Summary: updated 2 files, removed 2 files.",
		},
		{
			name:    "singular update with zero removal",
			updated: 1,
			removed: 0,
			want:    "Summary: updated 1 file, removed 0 files.",
		},
		{
			name:    "plural update with singular removal",
			updated: 2,
			removed: 1,
			want:    "Summary: updated 2 files, removed 1 file.",
		},
		{
			name:    "zero update with plural removal",
			updated: 0,
			removed: 2,
			want:    "Summary: updated 0 files, removed 2 files.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := docsUpdateSummary(tt.updated, tt.removed)
			if got != tt.want {
				t.Errorf("docsUpdateSummary(%d, %d) = %q, want %q", tt.updated, tt.removed, got, tt.want)
			}
		})
	}
}

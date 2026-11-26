package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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
	statFailurePath string
	readFailurePath string
}

func (f *docsFailureFS) Stat(name string) (fs.FileInfo, error) {
	if name == f.statFailurePath {
		return nil, fs.ErrPermission
	}
	return f.MockFileSystem.Stat(name)
}

func (f *docsFailureFS) ReadFile(name string) ([]byte, error) {
	if name == f.readFailurePath {
		return nil, fs.ErrPermission
	}
	return f.MockFileSystem.ReadFile(name)
}

func TestRunDocsUpdate(t *testing.T) {
	tests := []struct {
		name        string
		stamp       bool
		wantDiffers []string
	}{
		{name: "writes changed docs, removes only recorded obsolete docs, and preserves additions", stamp: true, wantDiffers: []string{"AGENTS.md"}},
		{name: "missing stamp removes nothing and reports both changed root files", wantDiffers: []string{"AGENTS.md", ".cursorrules"}},
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
			for _, path := range tt.wantDiffers {
				if !strings.Contains(out.String(), path+" differs from the current template. Compare it by hand; it was not changed.") {
					t.Errorf("output = %q, missing difference for %s", out.String(), path)
				}
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
		{name: "JSON", json: true, want: "{\n  \"updated\": [],\n  \"removed\": [],\n  \"differs\": [],\n  \"upToDate\": true\n}\n"},
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

func TestRunDocsUpdateStampAndRemovalErrors(t *testing.T) {
	tests := []struct {
		name string
		fs   *fsx.MockFileSystem
		want string
	}{
		{name: "stamp read fails", fs: &fsx.MockFileSystem{Files: map[string][]byte{"/repo/simple.scl": []byte("workspace")}, ReadFileErr: fs.ErrPermission}, want: "failed to read documentation stamp"},
		{name: "obsolete file removal fails", fs: &fsx.MockFileSystem{Files: map[string][]byte{"/repo/simple.scl": []byte("workspace"), "/repo/.simple/docs.json": docsStampJSON("old", "1.0.0", ".agent/old.md"), "/repo/.agent/old.md": []byte("old")}, RemoveErr: fs.ErrPermission}, want: "failed to remove obsolete documentation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runDocsUpdate(new(bytes.Buffer), docsUpdateDeps{fsys: tt.fs, templates: docsTestTemplates(), cwd: "/repo"}, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("runDocsUpdate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunDocsUpdateFilesystemErrors(t *testing.T) {
	tests := []struct {
		name string
		fs   func(*testing.T, *fstest.MapFS) fsx.FileSystem
		want string
	}{
		{
			name: "obsolete stat fails",
			fs: func(_ *testing.T, _ *fstest.MapFS) fsx.FileSystem {
				base := &fsx.MockFileSystem{Files: map[string][]byte{
					"/repo/simple.scl": []byte("workspace"), "/repo/.simple/docs.json": docsStampJSON("old", "1.0.0", ".agent/old.md"),
					"/repo/.agent/old.md": []byte("old"),
				}}
				return &docsFailureFS{MockFileSystem: base, statFailurePath: "/repo/.agent/old.md"}
			},
			want: "failed to check obsolete documentation",
		},
		{
			name: "shipped doc read fails",
			fs: func(t *testing.T, templates *fstest.MapFS) fsx.FileSystem {
				return &docsFailureFS{MockFileSystem: docsTestDisk(t, templates), readFailurePath: "/repo/.simple/context/guide.md"}
			},
			want: "failed to read documentation .simple/context/guide.md",
		},
		{
			name: "root guidance read fails",
			fs: func(t *testing.T, templates *fstest.MapFS) fsx.FileSystem {
				return &docsFailureFS{MockFileSystem: docsTestDisk(t, templates), readFailurePath: "/repo/AGENTS.md"}
			},
			want: "failed to read root guidance AGENTS.md",
		},
		{
			name: "documentation directory creation fails",
			fs: func(_ *testing.T, _ *fstest.MapFS) fsx.FileSystem {
				return &fsx.MockFileSystem{Files: map[string][]byte{"/repo/simple.scl": []byte("workspace")}, MkdirAllErr: fs.ErrPermission}
			},
			want: "failed to create documentation directory",
		},
		{
			name: "stamp directory creation fails",
			fs: func(t *testing.T, templates *fstest.MapFS) fsx.FileSystem {
				base := docsTestDisk(t, templates)
				base.MkdirAllErr = fs.ErrPermission
				return base
			},
			want: "failed to create documentation stamp directory",
		},
		{
			name: "stamp write fails",
			fs: func(t *testing.T, templates *fstest.MapFS) fsx.FileSystem {
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

func TestRunDocsUpdateMissingRootFilesDiffer(t *testing.T) {
	templates := docsTestTemplates()
	disk := docsTestDisk(t, templates)
	delete(disk.Files, "/repo/AGENTS.md")
	delete(disk.Files, "/repo/.cursorrules")
	out := new(bytes.Buffer)
	if err := runDocsUpdate(out, docsUpdateDeps{fsys: disk, templates: templates, cwd: "/repo", cliVersion: "2.0.0"}, false); err != nil {
		t.Fatalf("runDocsUpdate() error = %v", err)
	}
	if !strings.Contains(out.String(), "AGENTS.md differs") || !strings.Contains(out.String(), ".cursorrules differs") {
		t.Errorf("output = %q, want both missing root files reported", out.String())
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
		differs int
		want    string
	}{
		{
			name:    "count 0 for all fields",
			updated: 0,
			removed: 0,
			differs: 0,
			want:    "Summary: updated 0 files, removed 0 files, 0 root files differ.",
		},
		{
			name:    "count 1 for all fields",
			updated: 1,
			removed: 1,
			differs: 1,
			want:    "Summary: updated 1 file, removed 1 file, 1 root file differs.",
		},
		{
			name:    "count 2 for all fields",
			updated: 2,
			removed: 2,
			differs: 2,
			want:    "Summary: updated 2 files, removed 2 files, 2 root files differ.",
		},
		{
			name:    "singular update with zero removal and singular differs",
			updated: 1,
			removed: 0,
			differs: 1,
			want:    "Summary: updated 1 file, removed 0 files, 1 root file differs.",
		},
		{
			name:    "plural update with singular removal and plural differs",
			updated: 2,
			removed: 1,
			differs: 2,
			want:    "Summary: updated 2 files, removed 1 file, 2 root files differ.",
		},
		{
			name:    "zero update with plural removal and singular differs",
			updated: 0,
			removed: 2,
			differs: 1,
			want:    "Summary: updated 0 files, removed 2 files, 1 root file differs.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := docsUpdateSummary(tt.updated, tt.removed, tt.differs)
			if got != tt.want {
				t.Errorf("docsUpdateSummary(%d, %d, %d) = %q, want %q", tt.updated, tt.removed, tt.differs, got, tt.want)
			}
		})
	}
}

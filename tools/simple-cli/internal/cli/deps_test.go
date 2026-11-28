package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
)

func TestDepsUpdateUsesBuildTargetFormsAndReportsChangedLocks(t *testing.T) {
	for _, test := range []struct {
		name          string
		target        string
		want          int
		unchangedLock bool
	}{
		{name: "app target", target: "com.acme.demo", want: 2},
		{name: "app action shorthand", target: "com.acme.demo/lookup", want: 1},
		{name: "unchanged lock is not reported", target: "com.acme.demo", want: 2, unchangedLock: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			action := filepath.Join(root, "apps", "com.acme.demo", "actions", "lookup")
			space := filepath.Join(root, "apps", "com.acme.demo", "spaces", "admin")
			for _, dir := range []string{action, space} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"thing"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if test.unchangedLock {
				for _, dir := range []string{action, space} {
					if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("unchanged"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(action, "action.scl"), []byte("action lookup\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(space, "vite.config.ts"), []byte("export default {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(root)
			depsUpdateAll = false
			original := updateDependenciesFunc
			calls := 0
			updateDependenciesFunc = func(dir string) error {
				calls++
				if test.unchangedLock {
					return nil
				}
				return os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("updated"), 0o644)
			}
			t.Cleanup(func() { updateDependenciesFunc = original; depsUpdateAll = false })

			var out bytes.Buffer
			if err := runDepsUpdate(&out, fsx.OSFileSystem{}, []string{test.target}); err != nil {
				t.Fatalf("runDepsUpdate() error = %v", err)
			}
			if calls != test.want {
				t.Errorf("updated %d folders, want %d", calls, test.want)
			}
			wantReports := test.want
			if test.unchangedLock {
				wantReports = 0
			}
			if got := strings.Count(out.String(), "commit this lock file"); got != wantReports {
				t.Errorf("reported %d changed lock files, want %d: %s", got, wantReports, out.String())
			}
		})
	}
}

func TestDepsUpdateAllVisitsNPMFoldersAcrossEveryApp(t *testing.T) {
	for _, test := range []struct {
		name        string
		apps        []string
		withNPM     bool
		wantFolders []string
		wantErr     string
	}{
		{
			name:        "npm folders across every app, skipping Rust",
			apps:        []string{"com.acme.demo", "com.acme.other"},
			withNPM:     true,
			wantFolders: []string{"apps/com.acme.demo/actions/lookup", "apps/com.acme.demo/spaces/admin", "apps/com.acme.other/actions/lookup", "apps/com.acme.other/spaces/admin"},
		},
		{
			name:    "Rust-only app has no npm folders",
			apps:    []string{"com.acme.rust"},
			wantErr: "no npm action or space folders found",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for _, app := range test.apps {
				appDir := filepath.Join("apps", app)
				action := filepath.Join(appDir, "actions", "lookup")
				space := filepath.Join(appDir, "spaces", "admin")
				rustAction := filepath.Join(appDir, "actions", "rust")
				for _, dir := range []string{action, space, rustAction} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				if test.withNPM {
					for _, dir := range []string{action, space} {
						if err := os.WriteFile(filepath.Join(root, dir, "package.json"), []byte(`{"name":"thing"}`), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(filepath.Join(root, space, "vite.config.ts"), []byte("export default {}\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, action, "action.scl"), []byte("action lookup\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(root, rustAction, "action.scl"), []byte("action rust\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(root)
			depsUpdateAll = true
			original := updateDependenciesFunc
			var got []string
			updateDependenciesFunc = func(dir string) error {
				got = append(got, dir)
				return os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("updated"), 0o644)
			}
			t.Cleanup(func() { updateDependenciesFunc = original; depsUpdateAll = false })

			err := runDepsUpdate(&bytes.Buffer{}, fsx.OSFileSystem{}, nil)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("runDepsUpdate(--all) error = %v, want containing %q", err, test.wantErr)
				}
				if len(got) != 0 {
					t.Errorf("updated folders = %v, want none", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("runDepsUpdate(--all) error = %v", err)
			}
			slices.Sort(got)
			slices.Sort(test.wantFolders)
			if !slices.Equal(got, test.wantFolders) {
				t.Errorf("updated folders = %v, want npm folders from every app %v", got, test.wantFolders)
			}
		})
	}
}

func TestDepsUpdateRequiresTargetOrAll(t *testing.T) {
	for _, test := range []struct {
		name string
		all  bool
		args []string
	}{
		{name: "missing both"},
		{name: "both supplied", all: true, args: []string{"com.acme.demo"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			depsUpdateAll = test.all
			t.Cleanup(func() { depsUpdateAll = false })
			if err := runDepsUpdate(&bytes.Buffer{}, fsx.OSFileSystem{}, test.args); err == nil {
				t.Fatal("runDepsUpdate() error = nil")
			}
		})
	}
}

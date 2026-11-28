package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
)

func TestCheckDependencyLocks(t *testing.T) {
	for _, test := range []struct {
		name        string
		git         bool
		lock        bool
		tracked     bool
		committed   bool
		unavailable bool
		state       dependencyLockState
		wantErr     string
	}{
		{name: "missing lock", git: true, state: dependencyLockMissing, wantErr: "missing package-lock.json"},
		{name: "outside git", lock: true, state: dependencyLockNotRepository, wantErr: "not a git repository"},
		{name: "git unavailable", lock: true, unavailable: true, state: dependencyLockGitUnavailable, wantErr: "git is unavailable"},
		{name: "untracked lock", git: true, lock: true, state: dependencyLockUntracked, wantErr: "is not tracked"},
		{name: "changed lock", git: true, lock: true, tracked: true, committed: true, state: dependencyLockUncommitted, wantErr: "uncommitted changes"},
		{name: "committed lock", git: true, lock: true, tracked: true, committed: true, state: dependencyLockCommitted},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			app := filepath.Join(root, "apps", "com.acme.demo")
			action := filepath.Join(app, "actions", "lookup")
			if err := os.MkdirAll(filepath.Join(action, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(action, "action.scl"), "action lookup\n")
			write(filepath.Join(action, "src", "index.ts"), "export const run = () => 1\n")
			write(filepath.Join(action, "package.json"), `{"name":"lookup"}`)
			lockPath := filepath.Join(action, "package-lock.json")
			if test.lock {
				write(lockPath, "{\"lockfileVersion\":3}\n")
			}
			if test.git {
				runGit(t, root, "init", "-q")
				runGit(t, root, "add", filepath.Join("apps", "com.acme.demo", "actions", "lookup", "action.scl"), filepath.Join("apps", "com.acme.demo", "actions", "lookup", "src", "index.ts"))
				if test.tracked {
					runGit(t, root, "add", filepath.Join("apps", "com.acme.demo", "actions", "lookup", "package-lock.json"))
				}
				if test.committed {
					runGit(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "seed")
					if test.name == "changed lock" {
						write(lockPath, "changed\n")
					}
				}
			}
			t.Chdir(root)
			if test.unavailable {
				t.Setenv("PATH", "")
			}
			lock, err := checkDependencyLock(action)
			if err != nil || lock.state != test.state {
				t.Fatalf("checkDependencyLock() = %+v, %v, want state %v", lock, err, test.state)
			}
			var out bytes.Buffer
			err = checkDependencyLocks(filepath.Join("apps", "com.acme.demo"), &out)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("checkDependencyLocks() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) || !strings.Contains(err.Error(), filepath.Join("apps", "com.acme.demo", "actions", "lookup")) {
				t.Fatalf("checkDependencyLocks() error = %v, want folder and %q", err, test.wantErr)
			}
		})
	}
}

func TestCheckDependencyLocksUsesDevStudioMarkerForNoGitExemption(t *testing.T) {
	base := t.TempDir()
	instanceRoot := filepath.Join(base, "inst_acme_dev")
	app := filepath.Join(instanceRoot, "apps", "com.acme.demo")
	action := filepath.Join(app, "actions", "lookup")
	if err := os.MkdirAll(filepath.Join(action, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(action, "action.scl"), []byte("action lookup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(action, "src", "index.ts"), []byte("export const run = () => 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(action, "package.json"), []byte(`{"name":"lookup"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(action, "package-lock.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := checkDependencyLocks(app, &out); err == nil || !strings.Contains(err.Error(), "git init") {
		t.Fatalf("unmarked workspace deploy error = %v, want git init instructions", err)
	}
	if err := ensureDevStudioWorkspaceMarker(instanceRoot); err != nil {
		t.Fatal(err)
	}
	if err := checkDependencyLocks(app, &out); err != nil {
		t.Fatalf("checkDependencyLocks() error = %v", err)
	}
	if got := strings.Count(out.String(), "Dependency lock files could not be checked"); got != 1 {
		t.Fatalf("unchecked lock notice count = %d, output %q", got, out.String())
	}
	if err := os.Remove(filepath.Join(action, "package-lock.json")); err != nil {
		t.Fatal(err)
	}
	if err := checkDependencyLocks(app, &out); err == nil || !strings.Contains(err.Error(), "missing package-lock.json") {
		t.Fatalf("missing lock deploy error = %v, want missing lock refusal", err)
	}
}

func TestRunDeployWritesDevStudioLockNoticeToStderrInJSONMode(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "apps", "com.acme.demo")
	action := filepath.Join(app, "actions", "lookup")
	if err := os.MkdirAll(action, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(action, "action.scl"):        "action lookup\n",
		filepath.Join(action, "package.json"):      `{"name":"lookup"}`,
		filepath.Join(action, "package-lock.json"): "{}\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureDevStudioWorkspaceMarker(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	originalJSON, originalEnv, originalProgress := jsonOutput, deployEnv, deployProgress
	t.Cleanup(func() {
		jsonOutput, deployEnv, deployProgress = originalJSON, originalEnv, originalProgress
	})
	jsonOutput = true
	deployEnv = "dev"
	deployProgress = "plain"

	var stdout, stderr bytes.Buffer
	_ = runDeploy(context.Background(), fsx.OSFileSystem{}, &stdout, &stderr, []string{"apps/com.acme.demo"})
	if strings.Contains(stdout.String(), "Dependency lock files could not be checked") {
		t.Fatalf("JSON stdout contains the lock notice: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Dependency lock files could not be checked") {
		t.Fatalf("JSON stderr is missing the lock notice: %s", stderr.String())
	}
}

func TestNpmFoldersSelectsOnlyPackageJsonFolders(t *testing.T) {
	root := t.TempDir()
	actions := []string{filepath.Join(root, "actions", "typescript"), filepath.Join(root, "actions", "rust")}
	spaces := []string{filepath.Join(root, "spaces", "dashboard"), filepath.Join(root, "spaces", "other")}
	for _, folder := range append(append([]string(nil), actions...), spaces...) {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, folder := range []string{actions[0], spaces[0]} {
		if err := os.WriteFile(filepath.Join(folder, "package.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := npmFolders(fsx.OSFileSystem{}, actions, spaces)
	if err != nil {
		t.Fatalf("npmFolders() error = %v", err)
	}
	want := []string{actions[0], spaces[0]}
	if !slices.Equal(got, want) {
		t.Errorf("npmFolders() = %v, want %v", got, want)
	}
}

func TestPrintBuildLockNotice(t *testing.T) {
	for _, test := range []struct {
		name      string
		git       bool
		tracked   bool
		committed bool
		changed   bool
		want      bool
	}{
		{name: "untracked lock", git: true, want: true},
		{name: "changed lock", git: true, tracked: true, committed: true, changed: true, want: true},
		{name: "committed lock", git: true, tracked: true, committed: true},
		{name: "outside git"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			action := filepath.Join("apps", "com.acme.demo", "actions", "lookup")
			absAction := filepath.Join(root, action)
			if err := os.MkdirAll(absAction, 0o755); err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(absAction, "package-lock.json")
			for path, content := range map[string]string{
				filepath.Join(absAction, "package.json"): `{"name":"lookup"}`,
				lockPath:                                 "original\n",
			} {
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if test.git {
				runGit(t, root, "init", "-q")
				runGit(t, root, "add", filepath.Join(action, "package.json"))
				if test.tracked {
					runGit(t, root, "add", filepath.Join(action, "package-lock.json"))
				}
				if test.committed {
					runGit(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "seed")
				}
				if test.changed {
					if err := os.WriteFile(lockPath, []byte("changed\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Chdir(root)
			var out bytes.Buffer
			printBuildLockNotice([]string{action}, nil, &out)
			if got := strings.Contains(out.String(), filepath.ToSlash(filepath.Join(action, "package-lock.json"))); got != test.want {
				t.Errorf("notice output %q contains target lock = %v, want %v", out.String(), got, test.want)
			}
		})
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

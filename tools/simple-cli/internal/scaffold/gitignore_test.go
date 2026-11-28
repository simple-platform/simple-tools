package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The instance .gitignore keeps the Simple CLI's local state out of git: key
// files, unpacked tool runtimes, build caches and local secrets. The platform
// docs in .simple/context are part of the repository and stay in it.
//
// It also names two folders that other tools fill: devbox's environment,
// which holds the Rust install, and Cargo's build folder inside a Rust action.
// The workspace's linter skips what this one file ignores and reads no ignore
// file below the root, so without these two lines it walks into both: 151
// findings in a new workspace, and 48 more after the first build of a Rust
// action, none of them in a file anyone wrote.
func TestInstanceGitignoreKeepsLocalStateOutOfGit(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	template, err := TemplatesFS.ReadFile("templates/gitignore")
	if err != nil {
		t.Fatalf("read the gitignore template: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), template, 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if output, err := exec.Command(gitPath, "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}

	tests := []struct {
		path    string
		ignored bool
	}{
		{".simple/keys/acme/devstudio/000002/private.pem", true},
		{".simple/home/Library/Application Support/.burrito/runtime", true},
		{".simple/bin/scl-parser", true},
		{".simple/cache/npm/index", true},
		{".simple/tokens.json", true},
		{".env", true},
		{".devbox/virtenv/rustup/toolchains/1.100.0/share/doc/cargo/README.md", true},
		{"apps/com.acme.crm/actions/close-lead/target/.rustc_info.json", true},
		{"docs/target/notes.md", false},
		{"apps/com.acme.crm/actions/close-lead/src/main.rs", false},
		{".simple/context/01-platform-overview.md", false},
		{"apps/com.acme.crm/app.scl", false},
		{"simple.scl", false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			// check-ignore exits 0 when the path is ignored and 1 when not.
			err := exec.Command(gitPath, "-C", root, "check-ignore", "-q", "--no-index", test.path).Run()
			if ignored := err == nil; ignored != test.ignored {
				t.Fatalf("ignored = %t, want %t (%v)", ignored, test.ignored, err)
			}
		})
	}
}

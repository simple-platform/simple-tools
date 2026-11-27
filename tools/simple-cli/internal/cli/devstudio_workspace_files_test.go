package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simple-cli/internal/scaffold"
)

// devStudioTestWorkspace is an empty workspace whose CLI home, where the
// bridge keeps its records, is a folder of the test's own.
func devStudioTestWorkspace(t *testing.T) *devStudioWorkspace {
	t.Helper()
	t.Setenv("SIMPLE_CLI_HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &devStudioWorkspace{instanceID: "inst_workspace", instanceRoot: root}
}

func TestDevStudioWorkspaceGetsTheFilesItLacks(t *testing.T) {
	tests := []struct {
		name      string
		existing  map[string][]byte
		wantFiles []string
	}{
		{
			name:      "empty root gets all setup files",
			wantFiles: []string{"devbox.json", "package.json", "eslint.config.js", "pnpm-workspace.yaml"},
		},
		{
			name: "existing package gets devbox only",
			existing: map[string][]byte{
				"package.json": []byte("{\"name\":\"mine\"}\n"),
			},
			wantFiles: []string{"devbox.json"},
		},
		{
			name: "existing workspace file is kept",
			existing: map[string][]byte{
				"pnpm-workspace.yaml": []byte("packages:\n  - custom/*\n"),
			},
			wantFiles: []string{"devbox.json", "package.json", "eslint.config.js"},
		},
		{
			name: "existing lint setup is kept",
			existing: map[string][]byte{
				"eslint.config.js": []byte("export default []\n"),
			},
			wantFiles: []string{"devbox.json", "package.json", "pnpm-workspace.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := devStudioTestWorkspace(t)
			root := workspace.instanceRoot
			for name, contents := range tt.existing {
				if err := os.WriteFile(filepath.Join(root, name), contents, 0o644); err != nil {
					t.Fatalf("write existing %s: %v", name, err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n.env"), 0o644); err != nil {
				t.Fatal(err)
			}

			for range 2 {
				if err := ensureDevStudioWorkspaceFiles(workspace); err != nil {
					t.Fatalf("ensureDevStudioWorkspaceFiles: %v", err)
				}
			}

			want := make(map[string]bool, len(tt.wantFiles))
			for _, name := range tt.wantFiles {
				want[name] = true
			}
			for name := range tt.existing {
				want[name] = true
			}
			for _, name := range []string{"devbox.json", "package.json", "eslint.config.js", "pnpm-workspace.yaml"} {
				contents, err := os.ReadFile(filepath.Join(root, name))
				if want[name] {
					if err != nil {
						t.Fatalf("read expected %s: %v", name, err)
					}
					if original, exists := tt.existing[name]; exists {
						if string(contents) != string(original) {
							t.Errorf("existing %s changed: %q", name, contents)
						}
					} else {
						template, err := scaffold.TemplatesFS.ReadFile("templates/" + name)
						if err != nil {
							t.Fatalf("read %s template: %v", name, err)
						}
						if string(contents) != string(template) {
							t.Errorf("%s = %q, want the scaffold's", name, contents)
						}
					}
				} else if err == nil {
					t.Errorf("unexpected file %s was added", name)
				} else if !os.IsNotExist(err) {
					t.Errorf("inspect %s: %v", name, err)
				}
			}

			gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
			if err != nil {
				t.Fatalf("read .gitignore: %v", err)
			}
			for _, line := range []string{".simple/*", "!.simple/context/"} {
				if strings.Count(string(gitignore), "\n"+line+"\n") != 1 {
					t.Errorf(".gitignore does not hold %q exactly once:\n%s", line, gitignore)
				}
			}
			if strings.Count(string(gitignore), ".env") != 1 || !strings.HasPrefix(string(gitignore), "node_modules/\n.env\n") {
				t.Errorf(".gitignore lost or repeated its own lines:\n%s", gitignore)
			}
		})
	}
}

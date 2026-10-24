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
	workspace := devStudioTestWorkspace(t)
	root := workspace.instanceRoot
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"mine\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n.env"), 0o644); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := ensureDevStudioWorkspaceFiles(workspace); err != nil {
			t.Fatalf("ensureDevStudioWorkspaceFiles: %v", err)
		}
	}
	devbox, _ := os.ReadFile(filepath.Join(root, "devbox.json"))
	template, _ := scaffold.TemplatesFS.ReadFile("templates/devbox.json")
	if len(devbox) == 0 || string(devbox) != string(template) {
		t.Fatalf("devbox.json = %q, want the scaffold's", devbox)
	}
	if mine, _ := os.ReadFile(filepath.Join(root, "package.json")); string(mine) != "{\"name\":\"mine\"}\n" {
		t.Fatalf("an existing package.json was changed: %q", mine)
	}
	gitignore, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	for _, line := range []string{".simple/*", "!.simple/context/"} {
		if strings.Count(string(gitignore), "\n"+line+"\n") != 1 {
			t.Fatalf(".gitignore does not hold %q exactly once:\n%s", line, gitignore)
		}
	}
	if strings.Count(string(gitignore), ".env") != 1 || !strings.HasPrefix(string(gitignore), "node_modules/\n.env\n") {
		t.Fatalf(".gitignore lost or repeated its own lines:\n%s", gitignore)
	}
}

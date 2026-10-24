package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
)

type workspaceDevbox struct {
	Packages map[string]string `json:"packages"`
	Env      map[string]string `json:"env"`
}

type workspaceManifest struct {
	Private        bool              `json:"private"`
	PackageManager string            `json:"packageManager"`
	Engines        map[string]string `json:"engines"`
}

// A new workspace runs its tools through devbox and pins pnpm for corepack.
// Without the pin, corepack fetches the newest pnpm, and pnpm 12.15.0 ships no
// bin/pnpm.cjs for it to start, so `devbox run -- pnpm` fails at once.
func TestNewWorkspaceGetsDevboxAndPackageManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme")
	if err := CreateMonorepoStructure(fsx.OSFileSystem{}, TemplatesFS, root, MonorepoConfig{ProjectName: "acme", TenantName: "acme"}); err != nil {
		t.Fatalf("CreateMonorepoStructure() error = %v", err)
	}

	var devbox workspaceDevbox
	readWorkspaceJSON(t, filepath.Join(root, "devbox.json"), &devbox)
	if devbox.Packages["nodejs"] == "" || devbox.Env["DEVBOX_COREPACK_ENABLED"] != "true" || devbox.Env["COREPACK_INTEGRITY_KEYS"] != "0" {
		t.Fatalf("devbox.json = %+v, want node through devbox with corepack enabled", devbox)
	}
	var manifest workspaceManifest
	readWorkspaceJSON(t, filepath.Join(root, "package.json"), &manifest)
	if !manifest.Private || !strings.HasPrefix(manifest.PackageManager, "pnpm@") || manifest.Engines["node"] == "" {
		t.Fatalf("package.json = %+v, want a private manifest pinning pnpm", manifest)
	}
	// The workspace has no member packages to list.
	if _, err := os.Stat(filepath.Join(root, "pnpm-workspace.yaml")); !os.IsNotExist(err) {
		t.Fatalf("pnpm-workspace.yaml was written: %v", err)
	}
}

// The versions are the ones Simple's own app repositories pin in their
// devbox.json and package.json, so an instance workspace builds with the same
// node and pnpm. Change them together with those repositories.
func TestWorkspaceToolVersionsArePinned(t *testing.T) {
	var devbox workspaceDevbox
	readTemplateJSON(t, "templates/devbox.json", &devbox)
	if devbox.Packages["nodejs"] != "25.9.0" {
		t.Errorf("devbox.json nodejs = %q, want 25.9.0", devbox.Packages["nodejs"])
	}
	var manifest workspaceManifest
	readTemplateJSON(t, "templates/package.json", &manifest)
	if manifest.PackageManager != "pnpm@10.33.4" || manifest.Engines["node"] != ">=25" {
		t.Errorf("package.json packageManager = %q, engines.node = %q, want pnpm@10.33.4 and >=25", manifest.PackageManager, manifest.Engines["node"])
	}
}

func readWorkspaceJSON(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(contents, value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func readTemplateJSON(t *testing.T, name string, value any) {
	t.Helper()
	contents, err := TemplatesFS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := json.Unmarshal(contents, value); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

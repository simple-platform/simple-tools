package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"simple-cli/internal/fsx"
	"simple-cli/internal/testsupport"
)

// A behavior is generated as a script and a test for it, and the two have to
// agree as generated: the test once expected the script to touch the form in
// every event while the script, an empty starter, touched nothing, so a new
// behavior failed 3 of 3 before its author had written a line. Reading the two
// templates does not show that; running the generated test does, so this
// generates a behavior the way `simple new behavior` does and runs its test.
//
// The behavior is written under this module rather than under t.TempDir(),
// because the generated test imports vitest and Node finds a package by walking
// up from the importing file: from here it reaches this module's node_modules.
// The folder is named tmp, which the repository's .gitignore already covers,
// so a run that is killed leaves nothing for git to pick up.
func TestGeneratedBehaviorTestPassesWithVitest(t *testing.T) {
	moduleRoot := testsupport.ModuleRoot(t)
	tempParent := filepath.Join(moduleRoot, "tmp")
	if err := os.MkdirAll(tempParent, fsx.DirPerm); err != nil {
		t.Fatalf("create %s: %v", tempParent, err)
	}
	tempRoot, err := os.MkdirTemp(tempParent, "behavior-vitest-")
	if err != nil {
		t.Fatalf("create temporary behavior workspace: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tempRoot); err != nil {
			t.Errorf("remove temporary behavior workspace: %v", err)
		}
		// Removes tmp only when nothing else is in it; when something is,
		// the refusal is the right outcome.
		_ = os.Remove(tempParent)
	})

	appDir := filepath.Join(tempRoot, "apps", "com.test")
	if err := os.MkdirAll(appDir, fsx.DirPerm); err != nil {
		t.Fatalf("create temporary app: %v", err)
	}
	if err := CreateBehaviorStructure(fsx.OSFileSystem{}, TemplatesFS, tempRoot, BehaviorConfig{
		AppID:     "com.test",
		TableName: "orders",
	}); err != nil {
		t.Fatalf("generate behavior: %v", err)
	}

	vitest := filepath.Join(moduleRoot, "node_modules", "vitest", "vitest.mjs")
	if _, err := os.Stat(vitest); err != nil {
		t.Fatalf("vitest is not installed at %s; run pnpm install at the repository root: %v", vitest, err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatalf("node is required to run the generated behavior test: %v", err)
	}

	// vitest looks for its settings in the root it is given and then in every
	// folder above it. An empty settings file here ends that search, so a
	// settings file somewhere above this checkout cannot change the run.
	if err := os.WriteFile(filepath.Join(tempRoot, "vitest.config.mjs"), []byte("export default {}\n"), fsx.FilePerm); err != nil {
		t.Fatalf("write vitest settings: %v", err)
	}

	command := exec.Command("node", vitest, "run", "--root", tempRoot)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated behavior test failed: %v\n%s", err, output)
	}
}

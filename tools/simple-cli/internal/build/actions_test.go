package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindActions(t *testing.T) {
	appDir := t.TempDir()
	actionsDir := filepath.Join(appDir, "actions")

	// Structure:
	// - actions/action1/action.scl
	// - actions/action2/package.json
	// - actions/not_action/file.txt
	// - actions/nested/action3/action.scl (should be ignored by current FindActions which is not recursive)

	dirs := []string{
		"action1",
		"action2",
		"not_action",
		"nested/action3",
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(actionsDir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	createFile(t, filepath.Join(actionsDir, "action1", "action.scl"))
	createFile(t, filepath.Join(actionsDir, "action2", "package.json"))
	createFile(t, filepath.Join(actionsDir, "not_action", "file.txt"))
	createFile(t, filepath.Join(actionsDir, "nested", "action3", "action.scl"))

	actions, err := FindActions(appDir)
	if err != nil {
		t.Fatalf("FindActions() error = %v", err)
	}

	expected := []string{
		filepath.Join(actionsDir, "action1"),
		filepath.Join(actionsDir, "action2"),
	}

	// FindActions returns absolute paths (or joined paths).
	// Order might vary, so use map or check containment.
	if len(actions) != len(expected) {
		t.Errorf("got %d actions, want %d", len(actions), len(expected))
	}

	m := make(map[string]bool)
	for _, a := range actions {
		m[a] = true
	}

	for _, e := range expected {
		if !m[e] {
			t.Errorf("missing action: %s", e)
		}
	}
}

func TestIsActionDir(t *testing.T) {
	tmpDir := t.TempDir()

	createFile(t, filepath.Join(tmpDir, "action.scl"))
	if !IsActionDir(tmpDir) {
		t.Error("IsActionDir() = false for dir with action.scl")
	}

	tmpDir2 := t.TempDir()
	createFile(t, filepath.Join(tmpDir2, "package.json"))
	if !IsActionDir(tmpDir2) {
		t.Error("IsActionDir() = false for dir with package.json")
	}

	tmpDir3 := t.TempDir()
	createFile(t, filepath.Join(tmpDir3, "other.txt"))
	if IsActionDir(tmpDir3) {
		t.Error("IsActionDir() = true for dir without action files")
	}
}

func TestIsRustActionDir(t *testing.T) {
	// A crate: a manifest and the binary it names.
	rustDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rustDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	createFile(t, filepath.Join(rustDir, "Cargo.toml"))
	createFile(t, filepath.Join(rustDir, "src", "main.rs"))
	if !IsRustActionDir(rustDir) {
		t.Error("IsRustActionDir() = false for a dir with Cargo.toml and src/main.rs")
	}

	// A TypeScript action, which must keep going to its own toolchain.
	tsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tsDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	createFile(t, filepath.Join(tsDir, "package.json"))
	createFile(t, filepath.Join(tsDir, "src", "index.ts"))
	if IsRustActionDir(tsDir) {
		t.Error("IsRustActionDir() = true for a TypeScript action")
	}

	// A manifest with no binary is not a crate cargo can build or test, and
	// guessing that it is would hand the action to a toolchain that cannot run
	// it.
	partialDir := t.TempDir()
	createFile(t, filepath.Join(partialDir, "Cargo.toml"))
	if IsRustActionDir(partialDir) {
		t.Error("IsRustActionDir() = true for a dir with no src/main.rs")
	}
}

// TestIsActionDir_Rust pins the reason a Rust action has to be recognised even
// though nothing here can compile one: an unrecognised directory is not skipped
// with a warning, it is never counted, and the build then reports success
// having produced no artifact for it.
func TestIsActionDir_Rust(t *testing.T) {
	rustDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rustDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	createFile(t, filepath.Join(rustDir, "Cargo.toml"))
	createFile(t, filepath.Join(rustDir, "src", "main.rs"))

	if !IsActionDir(rustDir) {
		t.Error("IsActionDir() = false for a Rust action")
	}
}

// TestFindActions_IncludesRust checks the same thing through the discovery an
// app-wide build actually uses.
func TestFindActions_IncludesRust(t *testing.T) {
	appDir := t.TempDir()
	rustAction := filepath.Join(appDir, "actions", "greet-user")
	if err := os.MkdirAll(filepath.Join(rustAction, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	createFile(t, filepath.Join(rustAction, "Cargo.toml"))
	createFile(t, filepath.Join(rustAction, "src", "main.rs"))

	actions, err := FindActions(appDir)
	if err != nil {
		t.Fatalf("FindActions() error = %v", err)
	}
	if len(actions) != 1 || actions[0] != rustAction {
		t.Errorf("FindActions() = %v, want [%s]", actions, rustAction)
	}
}

func createFile(t *testing.T, path string) {
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestIsActionDir_SourceWithoutManifest pins that an action missing its
// manifest is still an action.
//
// It is the difference between a build that refuses by name and a build that
// never mentions the action at all. Measured with the manifest check deciding
// recognition: `simple build --all` over a repo whose one Rust action had lost
// its Cargo.toml answered `{"status": "success", "actions": []}` and exit 0,
// having compiled nothing — the worst shape a build failure can take, because
// the next thing that notices is a deploy.
func TestIsActionDir_SourceWithoutManifest(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
	}{
		{"rust action with no Cargo.toml", filepath.Join("src", "main.rs")},
		{"typescript action with no package.json", filepath.Join("src", "index.ts")},
		{"go action with no package.json", "main.go"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(tt.source)), 0755); err != nil {
				t.Fatal(err)
			}
			createFile(t, filepath.Join(dir, tt.source))

			if !IsActionDir(dir) {
				t.Errorf("IsActionDir() = false for a directory holding %s", tt.source)
			}
		})
	}
}

// TestIsActionDir_AmbiguousIsStillAnAction covers the directory the build
// refuses for a different reason. It has to be recognised first, or the
// refusal never runs.
func TestIsActionDir_AmbiguousIsStillAnAction(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	createFile(t, filepath.Join(dir, "src", "main.rs"))
	createFile(t, filepath.Join(dir, "main.go"))

	if !IsActionDir(dir) {
		t.Error("IsActionDir() = false for a directory holding two languages")
	}
}

// TestIsActionDir_SpaceIsNotAnAction guards the edge the source check walks
// close to: a Space carries a package.json and a src/index.ts, which is an
// action's evidence exactly. Its Vite files are what tell the two apart, and
// they are read before the source is.
func TestIsActionDir_SpaceIsNotAnAction(t *testing.T) {
	for _, marker := range []string{"vite.config.ts", "index.html"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
				t.Fatal(err)
			}
			createFile(t, filepath.Join(dir, "package.json"))
			createFile(t, filepath.Join(dir, "src", "index.ts"))
			createFile(t, filepath.Join(dir, marker))

			if IsActionDir(dir) {
				t.Errorf("IsActionDir() = true for a Space carrying %s", marker)
			}
			if !IsSpaceDir(dir) {
				t.Errorf("IsSpaceDir() = false for a Space carrying %s", marker)
			}
		})
	}
}

// TestFindActions_SeesAnActionMissingItsManifest checks the same thing through
// the discovery an app-wide build actually uses, which is where the silence
// was observed.
func TestFindActions_SeesAnActionMissingItsManifest(t *testing.T) {
	appDir := t.TempDir()
	action := filepath.Join(appDir, "actions", "greet-user")
	if err := os.MkdirAll(filepath.Join(action, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	createFile(t, filepath.Join(action, "src", "main.rs"))

	actions, err := FindActions(appDir)
	if err != nil {
		t.Fatalf("FindActions() error = %v", err)
	}
	if len(actions) != 1 || actions[0] != action {
		t.Errorf("FindActions() = %v, want [%s]", actions, action)
	}
}

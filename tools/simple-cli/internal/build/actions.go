package build

import (
	"os"
	"path/filepath"
)

// IsRustActionDir reports whether a directory holds a *complete* Rust action:
// the crate manifest and the binary that manifest names, both present, which is
// what cargo needs before it can build or test anything.
//
// It is not what decides whether a directory counts as an action — that is
// IsActionDir, and it asks about the source alone. The difference is the point:
// an action whose Cargo.toml has gone missing is incomplete, not absent, and it
// has to reach the build so the build can say which file is missing. Answering
// that question with this one is what let such an action be skipped in silence.
func IsRustActionDir(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "Cargo.toml")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(path, "src", "main.rs"))
	return err == nil
}

// FindActions searches for action directories within an app directory.
// It looks for directories containing 'action.scl' or 'package.json' inside the 'actions' subdirectory.
func FindActions(appDir string) ([]string, error) {
	var actions []string
	actionsDir := filepath.Join(appDir, "actions")

	if _, err := os.Stat(actionsDir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(actionsDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			path := filepath.Join(actionsDir, entry.Name())
			if IsActionDir(path) {
				actions = append(actions, path)
			}
		}
	}

	return actions, nil
}

func IsActionDir(path string) bool {
	// Check for action.scl - this is the definitive indicator of an action
	if _, err := os.Stat(filepath.Join(path, "action.scl")); err == nil {
		return true
	}

	// A Space is not an Action, however much of one it looks like. This is
	// asked before anything below because the two share their evidence: a
	// Space has a package.json, and its entry point is a src/index.ts.
	if _, err := os.Stat(filepath.Join(path, "vite.config.ts")); err == nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
		return false
	}

	// A directory holding an action's source is an action, whether or not the
	// manifest that belongs beside that source is there.
	//
	// The manifest cannot be the evidence, because the case worth catching is
	// the one where it is missing. A Rust action with no Cargo.toml, or a
	// TypeScript action with no package.json, is broken in a way its author
	// wants told: recognised, it is handed to the build and refused by name.
	// Unrecognised, it is not skipped loudly — it is not counted at all, and
	// the build reports success while producing nothing for it. That was
	// measurable: `simple build --all` over a repo whose one Rust action had
	// lost its Cargo.toml answered "status": "success" with an empty action
	// list and exit 0. Being seen and refused is the whole point.
	if hasActionSource(path) {
		return true
	}

	// A package.json with no source beside it is still an action directory —
	// this is what recognised a TypeScript action before any of the above, and
	// the build says what is wrong with it.
	if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
		return true
	}

	return false
}

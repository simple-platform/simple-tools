package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

// A WORKSPACE PUTS THE PACKAGES AT THE ROOT AND THE CODE FAR BELOW IT.
//
// That is the case a one-directory lookup gets wrong, and it is the normal
// arrangement rather than an unusual one: pnpm and npm workspaces both hoist,
// so a member commonly has no `node_modules` of its own and every import in it
// still resolves.
func TestResolveUpward_FindsAPackageHoistedToTheWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	member := filepath.Join(root, "apps", "demo.app", "actions", "thing")

	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatalf("could not build the member directory: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "node_modules", "vitest"), 0o755); err != nil {
		t.Fatalf("could not hoist the package: %v", err)
	}

	found, ok := ResolveUpward(OSFileSystem{}, member, "node_modules", "vitest")
	if !ok {
		t.Fatal("a package hoisted to the workspace root was reported unreachable")
	}

	if want := filepath.Join(root, "node_modules", "vitest"); found != want {
		t.Errorf("found %q, want the hoisted copy at %q", found, want)
	}
}

// The same walk answers for an executable a package installed, which is the
// second thing this replaced: the runner used to be looked for in two fixed
// directories and fell through to a network-resolving fallback when it was in
// neither.
func TestResolveUpward_FindsAnExecutableTheWorkspaceInstalled(t *testing.T) {
	root := t.TempDir()
	member := filepath.Join(root, "apps", "demo.app", "actions", "thing")

	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatalf("could not build the member directory: %v", err)
	}

	bin := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("could not build the bin directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(bin, "vitest"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("could not write the runner: %v", err)
	}

	found, ok := ResolveUpward(OSFileSystem{}, member, "node_modules", ".bin", "vitest")
	if !ok {
		t.Fatal("a runner installed at the workspace root was reported unreachable")
	}

	if want := filepath.Join(bin, "vitest"); found != want {
		t.Errorf("found %q, want %q", found, want)
	}
}

// A DIRECTORY NAMED `node_modules` IS NOT ASKED, because Node does not ask it.
//
// Asking it would look under `node_modules/node_modules/<name>`, which is not
// where anything is installed — so a caller standing inside a dependency would
// be told a package is present that no import there can reach.
func TestResolveUpward_DoesNotLookInsideANodeModulesDirectory(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "node_modules", "some-package")

	if err := os.MkdirAll(filepath.Join(inside, "node_modules", "node_modules", "trap"), 0o755); err != nil {
		t.Fatalf("could not build the trap: %v", err)
	}

	// Reachable from inside the dependency, by the walk that skips the
	// `node_modules` directory itself and carries on upward.
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "trap"), 0o755); err != nil {
		t.Fatalf("could not place the real copy: %v", err)
	}

	found, ok := ResolveUpward(OSFileSystem{}, inside, "node_modules", "trap")
	if !ok {
		t.Fatal("the walk stopped instead of carrying on past a node_modules directory")
	}

	if want := filepath.Join(root, "node_modules", "trap"); found != want {
		t.Errorf("found %q, want the copy Node would reach at %q", found, want)
	}
}

func TestResolveUpward_AnswersNoWhenNothingInTheChainHasIt(t *testing.T) {
	root := t.TempDir()
	member := filepath.Join(root, "apps", "demo.app", "actions", "thing")

	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatalf("could not build the member directory: %v", err)
	}

	if _, ok := ResolveUpward(OSFileSystem{}, member, "node_modules", "absent"); ok {
		t.Error("reported a package present that nothing in the chain installs")
	}
}

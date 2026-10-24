package cli

import (
	"os"
	"path/filepath"
	"testing"

	"simple-cli/internal/build"
)

// AN ACTION WHOSE PACKAGES ARE HOISTED IS NOT MISSING THEM.
//
// The install used to be gated on the action carrying a `node_modules` of its
// own. A workspace member does not: pnpm and npm both hoist to the root, so the
// common arrangement looked to this command exactly like a fresh checkout, and
// every run paid for a full dependency install it did not need.
//
// This is asserted rather than timed. The install is fast when everything is
// already cached, so a stopwatch reports noise and says nothing about whether
// the work happened — the only honest question is whether the install was
// called at all.
func TestTest_DoesNotInstallWhenThePackagesAreHoisted(t *testing.T) {
	root := t.TempDir()
	action := filepath.Join(root, "apps", "demo.app", "actions", "thing")

	if err := os.MkdirAll(action, 0o755); err != nil {
		t.Fatalf("could not build the action directory: %v", err)
	}

	// A package.json with no test script, so the command stops before running
	// anything: what is under test is the decision to install, not the runner.
	if err := os.WriteFile(filepath.Join(action, "package.json"), []byte(`{"name":"thing"}`), 0o644); err != nil {
		t.Fatalf("could not write the manifest: %v", err)
	}

	// Hoisted to the root, exactly as a workspace installs them, and absent
	// from the action itself.
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "vitest"), 0o755); err != nil {
		t.Fatalf("could not hoist the packages: %v", err)
	}

	installed := 0
	original := build.EnsureDependenciesFunc
	build.EnsureDependenciesFunc = func(string) error {
		installed++

		return nil
	}

	defer func() { build.EnsureDependenciesFunc = original }()

	oldWd, _ := os.Getwd()
	_ = os.Chdir(root)

	defer func() { _ = os.Chdir(oldWd) }()

	_, _, _ = invokeTestCmd("test", "demo.app", "-a", "thing")

	if installed != 0 {
		t.Errorf("installed dependencies %d time(s) for an action whose packages are already reachable", installed)
	}
}

// The other half, so the first is not passing by refusing to install at all.
func TestTest_InstallsWhenThePackagesAreReachableFromNowhere(t *testing.T) {
	root := t.TempDir()
	action := filepath.Join(root, "apps", "demo.app", "actions", "thing")

	if err := os.MkdirAll(action, 0o755); err != nil {
		t.Fatalf("could not build the action directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(action, "package.json"), []byte(`{"name":"thing"}`), 0o644); err != nil {
		t.Fatalf("could not write the manifest: %v", err)
	}

	installed := 0
	original := build.EnsureDependenciesFunc
	build.EnsureDependenciesFunc = func(string) error {
		installed++

		return nil
	}

	defer func() { build.EnsureDependenciesFunc = original }()

	oldWd, _ := os.Getwd()
	_ = os.Chdir(root)

	defer func() { _ = os.Chdir(oldWd) }()

	_, _, _ = invokeTestCmd("test", "demo.app", "-a", "thing")

	if installed != 1 {
		t.Errorf("installed dependencies %d time(s) when nothing in the chain carries them, want once", installed)
	}
}

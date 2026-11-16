package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
)

// A manifest that lists vitest, the one package every action's tests need.
const actionManifest = `{"name":"thing","devDependencies":{"vitest":"^4.0.0"}}`

// writeAction creates apps/demo.app/actions/<name> under root with the given
// package.json, and answers with its path.
func writeAction(t *testing.T, root, name, manifest string) string {
	t.Helper()

	action := filepath.Join(root, "apps", "demo.app", "actions", name)
	if err := os.MkdirAll(action, 0o755); err != nil {
		t.Fatalf("could not build the action directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(action, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("could not write the manifest: %v", err)
	}

	return action
}

// countInstalls replaces the install with a counter for the rest of the test.
func countInstalls(t *testing.T) *int {
	t.Helper()

	installed := 0
	original := build.EnsureDependenciesFunc
	build.EnsureDependenciesFunc = func(string) error {
		installed++

		return nil
	}

	t.Cleanup(func() { build.EnsureDependenciesFunc = original })

	return &installed
}

// noopCommand is a command that succeeds and does nothing, on any platform.
func noopCommand() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "exit", "0")
	}

	return exec.Command("true")
}

// recordTests replaces each target's test command with one that passes and
// runs nothing, and answers with every command handed out. The command's Dir
// is set after it is handed out, so read it only once the run is over.
func recordTests(t *testing.T, onRun func()) *[]*exec.Cmd {
	t.Helper()

	var mu sync.Mutex
	var cmds []*exec.Cmd

	original := testCommandFunc
	testCommandFunc = func(string, ...string) *exec.Cmd {
		if onRun != nil {
			onRun()
		}

		cmd := noopCommand()

		mu.Lock()
		defer mu.Unlock()

		cmds = append(cmds, cmd)

		return cmd
	}

	t.Cleanup(func() { testCommandFunc = original })

	return &cmds
}

// runFrom runs `simple test` from root, the way a developer runs it from the
// top of a repository.
func runFrom(t *testing.T, root string, args ...string) error {
	t.Helper()

	oldWd, _ := os.Getwd()
	_ = os.Chdir(root)

	defer func() { _ = os.Chdir(oldWd) }()

	_, _, err := invokeTestCmd(append([]string{"test"}, args...)...)

	return err
}

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
	writeAction(t, root, "thing", actionManifest)

	// Hoisted to the root, exactly as a workspace installs them, and absent
	// from the action itself.
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "vitest"), 0o755); err != nil {
		t.Fatalf("could not hoist the packages: %v", err)
	}

	installed := countInstalls(t)
	recordTests(t, nil)
	_ = runFrom(t, root, "demo.app", "-a", "thing")

	if *installed != 0 {
		t.Errorf("installed dependencies %d time(s) for an action whose packages are already reachable", *installed)
	}
}

// The other half, so the first is not passing by refusing to install at all.
func TestTest_InstallsWhenThePackagesAreReachableFromNowhere(t *testing.T) {
	root := t.TempDir()
	writeAction(t, root, "thing", actionManifest)

	installed := countInstalls(t)
	recordTests(t, nil)
	_ = runFrom(t, root, "demo.app", "-a", "thing")

	if *installed != 1 {
		t.Errorf("installed dependencies %d time(s) when nothing in the chain carries them, want once", *installed)
	}
}

// A node_modules ABOVE THE ACTION IS NOT THE ACTION'S PACKAGES.
//
// The root of a repository is installed for its own tools, which puts a
// `node_modules` above every action and space in it, including those outside
// any workspace that have never been installed. Asking only whether some
// `node_modules` exists skipped their install, and their tests then failed on
// a missing module.
func TestTest_InstallsWhenOnlyOtherPackagesAreAbove(t *testing.T) {
	root := t.TempDir()
	writeAction(t, root, "thing", actionManifest)

	if err := os.MkdirAll(filepath.Join(root, "node_modules", "eslint"), 0o755); err != nil {
		t.Fatalf("could not install the root's own tools: %v", err)
	}

	installed := countInstalls(t)
	recordTests(t, nil)
	_ = runFrom(t, root, "demo.app", "-a", "thing")

	if *installed != 1 {
		t.Errorf("installed dependencies %d time(s) for an action whose packages are nowhere, want once", *installed)
	}
}

// Manifests npm accepts that must not read as missing everything: one saved
// with a byte-order mark, and one whose package is also listed as optional,
// which npm may skip.
func TestTest_DoesNotInstallForAByteOrderMarkOrAnOptionalPackage(t *testing.T) {
	root := t.TempDir()
	writeAction(t, root, "bom", "\xef\xbb\xbf"+actionManifest)
	writeAction(t, root, "optional", `{"name":"thing","dependencies":{"vitest":"^4.0.0","fsevents":"^2.3.0"},"optionalDependencies":{"fsevents":"^2.3.0"}}`)

	if err := os.MkdirAll(filepath.Join(root, "node_modules", "vitest"), 0o755); err != nil {
		t.Fatalf("could not hoist the packages: %v", err)
	}

	installed := countInstalls(t)
	recordTests(t, nil)
	_ = runFrom(t, root, "demo.app")

	if *installed != 0 {
		t.Errorf("installed dependencies %d time(s) for manifests whose packages are all reachable", *installed)
	}
}

// EVERY INSTALL FINISHES BEFORE THE FIRST TEST STARTS.
//
// A folder of shared contract tests borrows the SDK from an action beside it,
// so its test can only pass once that action's install is done. Side by side,
// which got there first decided the result.
//
// One install is slow on purpose, and each is recorded as it finishes: had a
// target gone on to its test straight after its own install, the quick one's
// test would land before the slow install and the order would show it.
func TestTest_InstallsEverythingBeforeAnyTestRuns(t *testing.T) {
	root := t.TempDir()
	writeAction(t, root, "first", actionManifest)
	writeAction(t, root, "second", actionManifest)

	var mu sync.Mutex
	var events []string

	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()

		events = append(events, event)
	}

	originalInstall := build.EnsureDependenciesFunc
	build.EnsureDependenciesFunc = func(dir string) error {
		if filepath.Base(dir) == "first" {
			time.Sleep(100 * time.Millisecond)
		}

		record("install")

		return nil
	}

	t.Cleanup(func() { build.EnsureDependenciesFunc = originalInstall })

	recordTests(t, func() { record("test") })

	if err := runFrom(t, root, "demo.app"); err != nil {
		t.Fatalf("simple test failed: %v", err)
	}

	if got := strings.Join(events, " "); got != "install install test test" {
		t.Errorf("events ran in the order %q, want both installs before either test", got)
	}
}

// A TARGET WHOSE INSTALL FAILED IS NOT TESTED, AND COUNTS AS FAILED.
func TestTest_SkipsTheTestOfATargetWhoseInstallFailed(t *testing.T) {
	root := t.TempDir()
	writeAction(t, root, "broken", actionManifest)
	writeAction(t, root, "fine", actionManifest)

	originalInstall := build.EnsureDependenciesFunc
	build.EnsureDependenciesFunc = func(dir string) error {
		// By name: the command may see the temporary folder through a symlink
		// (/var is /private/var on macOS), so a full path need not match.
		if filepath.Base(dir) == "broken" {
			return errors.New("registry unreachable")
		}

		return nil
	}

	t.Cleanup(func() { build.EnsureDependenciesFunc = originalInstall })

	cmds := recordTests(t, nil)

	err := runFrom(t, root, "demo.app")
	if err == nil || !strings.Contains(err.Error(), "1/2 test suites failed") {
		t.Errorf("simple test answered %v, want 1/2 test suites failed", err)
	}

	var ran []string
	for _, cmd := range *cmds {
		ran = append(ran, filepath.Base(cmd.Dir))
	}

	if strings.Join(ran, " ") != "fine" {
		t.Errorf("ran the tests of %v, want only fine, whose install succeeded", ran)
	}
}

// recordInstalls replaces the install with one that counts itself and notes
// what it installed from, as the real install does once npm has finished.
func recordInstalls(t *testing.T) *int {
	t.Helper()

	installed := 0
	original := build.EnsureDependenciesFunc
	build.EnsureDependenciesFunc = func(dir string) error {
		installed++

		return build.RecordInstall(fsx.OSFileSystem{}, dir)
	}

	t.Cleanup(func() { build.EnsureDependenciesFunc = original })

	return &installed
}

// A TARGET IS INSTALLED AGAIN WHEN ITS LOCKFILE MOVES.
//
// Its packages were installed only when one was missing. A dependency update
// moves the lockfile under packages already installed, so the tests kept
// running against the old versions while `simple build` bundled the new ones.
func TestTest_InstallsAgainWhenTheLockfileMoves(t *testing.T) {
	root := t.TempDir()
	action := writeAction(t, root, "thing", actionManifest)

	// vitest is installed throughout, so a missing package never decides.
	if err := os.MkdirAll(filepath.Join(action, "node_modules", "vitest"), 0o755); err != nil {
		t.Fatalf("could not install vitest: %v", err)
	}

	lock := func(version string) {
		content := `{"lockfileVersion":3,"packages":{"node_modules/vitest":{"version":"` + version + `"}}}`
		if err := os.WriteFile(filepath.Join(action, "package-lock.json"), []byte(content), 0o644); err != nil {
			t.Fatalf("could not write the lockfile: %v", err)
		}
	}

	installed := recordInstalls(t)
	recordTests(t, nil)

	run := func(step string, want int) {
		*installed = 0
		_ = runFrom(t, root, "demo.app", "-a", "thing")

		if *installed != want {
			t.Errorf("%s: installed %d time(s), want %d", step, *installed, want)
		}
	}

	lock("4.0.1")
	run("first run, with nothing noted about the install", 1)
	run("second run, nothing changed", 0)

	lock("4.0.2")
	run("the lockfile moved", 1)
	run("the run after", 0)
}

// THE REPOSITORY ROOT'S VITEST RUNS FROM INSIDE THE TARGET.
//
// Record behaviours and folders of shared code carry no package.json, so their
// tests run on the vitest installed at the repository root. It was found by a
// path relative to the root, then started from inside the target, where that
// path leads nowhere: each of those suites failed at once and printed nothing.
func TestTest_RunsTheRootsVitestFromInsideTheTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in vitest is a shell script")
	}

	root := t.TempDir()
	behaviors := filepath.Join(root, "apps", "demo.app", "scripts", "record-behaviors")
	if err := os.MkdirAll(behaviors, 0o755); err != nil {
		t.Fatalf("could not build the behaviours folder: %v", err)
	}

	if err := os.WriteFile(filepath.Join(behaviors, "order.test.js"), nil, 0o644); err != nil {
		t.Fatalf("could not write the test: %v", err)
	}

	// A stand-in for the root's vitest that writes down the folder it runs in.
	ranIn := filepath.Join(root, "ran-in")
	bin := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("could not build the root's bin folder: %v", err)
	}

	script := "#!/bin/sh\npwd -P > '" + ranIn + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "vitest"), []byte(script), 0o755); err != nil {
		t.Fatalf("could not write the stand-in vitest: %v", err)
	}

	if err := runFrom(t, root, "demo.app"); err != nil {
		t.Fatalf("the behaviours suite failed: %v", err)
	}

	got, err := os.ReadFile(ranIn)
	if err != nil {
		t.Fatalf("the root's vitest never ran: %v", err)
	}

	want, err := filepath.EvalSymlinks(behaviors)
	if err != nil {
		t.Fatalf("could not resolve the behaviours folder: %v", err)
	}

	if strings.TrimSpace(string(got)) != want {
		t.Errorf("vitest ran in %q, want %q", strings.TrimSpace(string(got)), want)
	}
}

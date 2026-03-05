package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindSpaces(t *testing.T) {
	appDir := t.TempDir()
	spacesDir := filepath.Join(appDir, "spaces")

	// Structure:
	// - spaces/space1/package.json
	// - spaces/space2/package.json
	// - spaces/not_space/file.txt
	// - spaces/nested/space3/package.json (ignored since not recursive)

	dirs := []string{
		"space1",
		"space2",
		"not_space",
		"nested/space3",
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(spacesDir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	createFile(t, filepath.Join(spacesDir, "space1", "package.json"))
	createFile(t, filepath.Join(spacesDir, "space2", "package.json"))
	createFile(t, filepath.Join(spacesDir, "not_space", "file.txt"))
	createFile(t, filepath.Join(spacesDir, "nested", "space3", "package.json"))

	spaces, err := FindSpaces(appDir)
	if err != nil {
		t.Fatalf("FindSpaces() error = %v", err)
	}

	expected := []string{
		filepath.Join(spacesDir, "space1"),
		filepath.Join(spacesDir, "space2"),
	}

	if len(spaces) != len(expected) {
		t.Errorf("got %d spaces, want %d", len(spaces), len(expected))
	}

	m := make(map[string]bool)
	for _, s := range spaces {
		m[s] = true
	}

	for _, e := range expected {
		if !m[e] {
			t.Errorf("missing space: %s", e)
		}
	}
}

func TestFindSpaces_NoSpacesDir(t *testing.T) {
	appDir := t.TempDir() // No spaces dir created
	spaces, err := FindSpaces(appDir)
	if err != nil {
		t.Fatalf("Expected nil error if dir doesn't exist, got %v", err)
	}
	if len(spaces) != 0 {
		t.Errorf("Expected 0 spaces, got %d", len(spaces))
	}
}

func TestIsSpaceDir(t *testing.T) {
	tmpDir := t.TempDir()

	createFile(t, filepath.Join(tmpDir, "package.json"))
	if !IsSpaceDir(tmpDir) {
		t.Error("IsSpaceDir() = false for dir with package.json")
	}

	tmpDir2 := t.TempDir()
	createFile(t, filepath.Join(tmpDir2, "other.txt"))
	if IsSpaceDir(tmpDir2) {
		t.Error("IsSpaceDir() = true for dir without package.json")
	}
}

func TestBuildSpace_DependencyError(t *testing.T) {
	origEnsure := EnsureDependenciesFunc
	defer func() { EnsureDependenciesFunc = origEnsure }()

	EnsureDependenciesFunc = func(dir string) error {
		return errors.New("mock npm error")
	}

	manager := NewBuildManager(DefaultBuildOptions())
	ctx := context.Background()

	result := manager.BuildSpace(ctx, "/mock/space", nil)
	if result.Error == nil {
		t.Error("Expected error from BuildSpace due to EnsureDependencies failure")
	}
	if result.SpaceName != "space" {
		t.Errorf("Expected space name 'space', got '%s'", result.SpaceName)
	}
}

func fakeExecCommandSuccess(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "MOCK_FAIL=0"}
	return cmd
}

func fakeExecCommandFail(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "MOCK_FAIL=1"}
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("MOCK_FAIL") == "1" {
		_, _ = fmt.Fprint(os.Stdout, "mock error output")
		os.Exit(1)
	}
	_, _ = fmt.Fprint(os.Stdout, "mock success output")
	os.Exit(0)
}

func TestBuildSpace_Quiet_Success(t *testing.T) {
	origEnsure := EnsureDependenciesFunc
	origExec := ExecCommandFunc
	defer func() {
		EnsureDependenciesFunc = origEnsure
		ExecCommandFunc = origExec
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExecCommandFunc = fakeExecCommandSuccess

	opts := DefaultBuildOptions()
	opts.Verbose = false
	manager := NewBuildManager(opts)
	ctx := context.Background()

	spaceDir := filepath.Join(t.TempDir(), "space")
	_ = os.MkdirAll(spaceDir, 0755)

	result := manager.BuildSpace(ctx, spaceDir, nil)
	if result.Error != nil {
		t.Errorf("Expected success, got error: %v", result.Error)
	}
}

func TestBuildSpace_Quiet_Fail(t *testing.T) {
	origEnsure := EnsureDependenciesFunc
	origExec := ExecCommandFunc
	defer func() {
		EnsureDependenciesFunc = origEnsure
		ExecCommandFunc = origExec
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExecCommandFunc = fakeExecCommandFail

	opts := DefaultBuildOptions()
	opts.Verbose = false
	manager := NewBuildManager(opts)
	ctx := context.Background()

	spaceDir := filepath.Join(t.TempDir(), "space")
	_ = os.MkdirAll(spaceDir, 0755)

	result := manager.BuildSpace(ctx, spaceDir, nil)
	if result.Error == nil {
		t.Error("Expected error, got success")
	}
	if !strings.Contains(result.Error.Error(), "mock error output") {
		t.Errorf("Expected mocked output in error, got: %v", result.Error)
	}
}

func TestBuildSpace_Verbose_Success(t *testing.T) {
	origEnsure := EnsureDependenciesFunc
	origExec := ExecCommandFunc
	defer func() {
		EnsureDependenciesFunc = origEnsure
		ExecCommandFunc = origExec
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExecCommandFunc = fakeExecCommandSuccess

	opts := DefaultBuildOptions()
	opts.Verbose = true
	manager := NewBuildManager(opts)
	ctx := context.Background()

	spaceDir := filepath.Join(t.TempDir(), "space")
	_ = os.MkdirAll(spaceDir, 0755)

	result := manager.BuildSpace(ctx, spaceDir, nil)
	if result.Error != nil {
		t.Errorf("Expected success, got error: %v", result.Error)
	}
}

func TestBuildSpace_Verbose_Fail(t *testing.T) {
	origEnsure := EnsureDependenciesFunc
	origExec := ExecCommandFunc
	defer func() {
		EnsureDependenciesFunc = origEnsure
		ExecCommandFunc = origExec
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExecCommandFunc = fakeExecCommandFail

	opts := DefaultBuildOptions()
	opts.Verbose = true
	manager := NewBuildManager(opts)
	ctx := context.Background()

	spaceDir := filepath.Join(t.TempDir(), "space")
	_ = os.MkdirAll(spaceDir, 0755)

	result := manager.BuildSpace(ctx, spaceDir, nil)
	if result.Error == nil {
		t.Error("Expected error, got success")
	}
}

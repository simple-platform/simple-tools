package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestEnsureTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Save original functions and restore after test
	origVerify := EnsureSCLParserFunc
	origJavy := EnsureJavyFunc
	origWasmOpt := EnsureWasmOptFunc
	defer func() {
		EnsureSCLParserFunc = origVerify
		EnsureJavyFunc = origJavy
		EnsureWasmOptFunc = origWasmOpt
	}()

	// Mock ensure functions
	EnsureSCLParserFunc = func(func(string)) (string, error) { return "/path/to/scl", nil }
	EnsureJavyFunc = func(func(string)) (string, error) { return "/path/to/javy", nil }
	EnsureWasmOptFunc = func(func(string)) (string, error) { return "/path/to/wasm-opt", nil }

	m := NewBuildManager(DefaultBuildOptions())

	// Track progress
	var progress []string
	var mu sync.Mutex
	reporter := func(item, status string, done bool, err error) {
		mu.Lock()
		defer mu.Unlock()
		progress = append(progress, item+":"+status)
	}

	if err := m.EnsureTools(reporter); err != nil {
		t.Errorf("EnsureTools() error = %v", err)
	}

	if m.tools.SCLParser != "/path/to/scl" {
		t.Errorf("SCLParser path mismatch: got %s", m.tools.SCLParser)
	}
}

func TestEnsureTools_Error(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Save original functions
	origVerify := EnsureSCLParserFunc
	origJavy := EnsureJavyFunc
	origWasmOpt := EnsureWasmOptFunc
	defer func() {
		EnsureSCLParserFunc = origVerify
		EnsureJavyFunc = origJavy
		EnsureWasmOptFunc = origWasmOpt
	}()

	mockError := errors.New("mock error")
	EnsureSCLParserFunc = func(func(string)) (string, error) { return "", mockError }
	EnsureJavyFunc = func(func(string)) (string, error) { return "/path/to/javy", nil }
	EnsureWasmOptFunc = func(func(string)) (string, error) { return "/path/to/wasm-opt", nil }

	m := NewBuildManager(DefaultBuildOptions())

	if err := m.EnsureTools(nil); !errors.Is(err, mockError) {
		t.Errorf("EnsureTools() error = %v, want %v", err, mockError)
	}
}

func TestBuildActions_Concurrency(t *testing.T) {
	// Mock dependencies
	origDeps := EnsureDependenciesFunc
	origBundle := BundleJSFunc
	origAsync := BundleAsyncFunc
	origCompile := CompileToWasmFunc
	origOpt := OptimizeWasmFunc
	origValidate := ValidateLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		ValidateLanguageFunc = origValidate
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
	ValidateLanguageFunc = func(dir string) error { return nil }
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	m := NewBuildManager(BuildOptions{Concurrency: 2})
	m.tools.Javy = "javy"
	m.tools.WasmOpt = "wasm-opt"

	tmpDir := t.TempDir()
	actions := []string{
		filepath.Join(tmpDir, "dir1"),
		filepath.Join(tmpDir, "dir2"),
		filepath.Join(tmpDir, "dir3"),
		filepath.Join(tmpDir, "dir4"),
	}

	for _, d := range actions {
		// Mock creation is enough as we mock functions, but BuildAction creates 'build' dir inside.
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	results := m.BuildActions(context.Background(), actions, nil)

	if len(results) != 4 {
		t.Errorf("got %d results, want 4", len(results))
	}

	for _, res := range results {
		if res.Error != nil {
			t.Errorf("unexpected error for %s: %v", res.ActionName, res.Error)
		}
	}
}

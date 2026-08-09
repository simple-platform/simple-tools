package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"simple-cli/internal/fsx"
	"strings"
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
	origExtract := ExtractMetadataFunc
	origBundle := BundleJSFunc
	origAsync := BundleAsyncFunc
	origCompile := CompileToWasmFunc
	origOpt := OptimizeWasmFunc
	origDetect := DetectActionLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		DetectActionLanguageFunc = origDetect
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error { return nil }
	BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
	DetectActionLanguageFunc = func(dir string) (ActionLanguage, error) { return LanguageTypeScript, nil }
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

// TestBuildAction_MetadataExtractionCalled verifies that metadata extraction is called during build
func TestBuildAction_MetadataExtractionCalled(t *testing.T) {
	// Mock all dependencies
	origDeps := EnsureDependenciesFunc
	origExtract := ExtractMetadataFunc
	origBundle := BundleJSFunc
	origAsync := BundleAsyncFunc
	origCompile := CompileToWasmFunc
	origOpt := OptimizeWasmFunc
	origDetect := DetectActionLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		DetectActionLanguageFunc = origDetect
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	// Track metadata extraction calls
	var metadataCallCount int
	var metadataActionDir string
	var metadataFS fsx.FileSystem

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error {
		metadataCallCount++
		metadataActionDir = actionDir
		metadataFS = fs
		return nil
	}
	BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
	DetectActionLanguageFunc = func(dir string) (ActionLanguage, error) { return LanguageTypeScript, nil }
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	m := NewBuildManager(DefaultBuildOptions())
	m.tools.Javy = "javy"
	m.tools.WasmOpt = "wasm-opt"

	tmpDir := t.TempDir()
	actionDir := filepath.Join(tmpDir, "test-action")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatal(err)
	}

	result := m.BuildAction(context.Background(), actionDir, nil)

	// Verify metadata extraction was called
	if metadataCallCount != 1 {
		t.Errorf("ExtractMetadataFunc called %d times, want 1", metadataCallCount)
	}

	if metadataActionDir != actionDir {
		t.Errorf("ExtractMetadataFunc called with actionDir %s, want %s", metadataActionDir, actionDir)
	}

	if metadataFS == nil {
		t.Error("ExtractMetadataFunc called with nil FileSystem")
	}

	// Verify build succeeded
	if result.Error != nil {
		t.Errorf("BuildAction() error = %v, want nil", result.Error)
	}

	if result.ActionName != "test-action" {
		t.Errorf("BuildAction() ActionName = %s, want test-action", result.ActionName)
	}
}

// AN ACTION THAT CANNOT BE DESCRIBED FROM ITS OWN SOURCE DOES NOT BUILD, and
// nothing downstream of the failure runs.
//
// The refusal used to be reported into the progress view and the build carried
// on, which is a gate that never closes twice over: the row was overwritten by
// the next step milliseconds later so no author ever read it, and the artifacts
// were bundled and shipped beside an action.json describing an earlier source.
// So this asserts the error comes back AND that the steps after it were never
// reached — a build that fails at the end has still done the work of a build
// that succeeded.
func TestBuildAction_MetadataExtractionFailureStopsTheBuild(t *testing.T) {
	// Mock all dependencies
	origDeps := EnsureDependenciesFunc
	origExtract := ExtractMetadataFunc
	origBundle := BundleJSFunc
	origAsync := BundleAsyncFunc
	origCompile := CompileToWasmFunc
	origOpt := OptimizeWasmFunc
	origDetect := DetectActionLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		DetectActionLanguageFunc = origDetect
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	metadataError := &AnnotationRefusal{
		Refusal: `test-action: @effects names an unknown effect "sideways"`,
	}

	var metadataCallCount, bundleCount, compileCount, optimizeCount int

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error {
		metadataCallCount++
		return metadataError
	}
	BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error {
		bundleCount++
		return nil
	}
	BundleAsyncFunc = func(dir, entry, out string) error {
		bundleCount++
		return nil
	}
	CompileToWasmFunc = func(javy, js, plugin, out string) error {
		compileCount++
		return nil
	}
	OptimizeWasmFunc = func(opt, in, out string, flags []string) error {
		optimizeCount++
		return nil
	}
	DetectActionLanguageFunc = func(dir string) (ActionLanguage, error) { return LanguageTypeScript, nil }
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	m := NewBuildManager(DefaultBuildOptions())
	m.tools.Javy = "javy"
	m.tools.WasmOpt = "wasm-opt"

	tmpDir := t.TempDir()
	actionDir := filepath.Join(tmpDir, "test-action")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Track progress reports to verify warning is logged
	var progressReports []string
	var mu sync.Mutex
	reporter := func(item, status string, done bool, err error) {
		mu.Lock()
		defer mu.Unlock()
		progressReports = append(progressReports, status)
	}

	result := m.BuildAction(context.Background(), actionDir, reporter)

	// Verify metadata extraction was called
	if metadataCallCount != 1 {
		t.Errorf("ExtractMetadataFunc called %d times, want 1", metadataCallCount)
	}

	if result.Error == nil {
		t.Fatal("BuildAction() error = nil, want the refusal (a malformed annotation must fail the build)")
	}

	// The refusal reaches the caller as the sentence its author has to act on,
	// not as a category the caller then has to go looking for the detail of.
	if !errors.Is(result.Error, error(metadataError)) {
		t.Errorf("BuildAction() error = %v, want it to carry the refusal", result.Error)
	}

	if !strings.Contains(result.Error.Error(), `@effects names an unknown effect "sideways"`) {
		t.Errorf("BuildAction() error = %q, want the refusal text an author can act on", result.Error)
	}

	// Nothing after the failed gate ran.
	if bundleCount != 0 || compileCount != 0 || optimizeCount != 0 {
		t.Errorf("build continued past the refusal: bundle=%d compile=%d optimize=%d",
			bundleCount, compileCount, optimizeCount)
	}

	// The progress view is an in-place repaint that is torn down at the end of a
	// run, so it is where a refusal goes to be unread. The build's failure has
	// to be carried out of here in the result, not left in a row.
	for _, report := range progressReports {
		if strings.Contains(report, "sideways") {
			t.Errorf("the refusal was written into the progress view: %q", report)
		}
	}
}

// TestBuildAction_MetadataExtractionProgressReporting verifies that progress is reported for metadata extraction
func TestBuildAction_MetadataExtractionProgressReporting(t *testing.T) {
	// Mock all dependencies
	origDeps := EnsureDependenciesFunc
	origExtract := ExtractMetadataFunc
	origBundle := BundleJSFunc
	origAsync := BundleAsyncFunc
	origCompile := CompileToWasmFunc
	origOpt := OptimizeWasmFunc
	origDetect := DetectActionLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		DetectActionLanguageFunc = origDetect
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error { return nil }
	BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
	DetectActionLanguageFunc = func(dir string) (ActionLanguage, error) { return LanguageTypeScript, nil }
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	m := NewBuildManager(DefaultBuildOptions())
	m.tools.Javy = "javy"
	m.tools.WasmOpt = "wasm-opt"

	tmpDir := t.TempDir()
	actionDir := filepath.Join(tmpDir, "test-action")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Track progress reports
	var progressReports []string
	var mu sync.Mutex
	reporter := func(item, status string, done bool, err error) {
		mu.Lock()
		defer mu.Unlock()
		progressReports = append(progressReports, status)
	}

	result := m.BuildAction(context.Background(), actionDir, reporter)

	// Verify build succeeded
	if result.Error != nil {
		t.Errorf("BuildAction() error = %v, want nil", result.Error)
	}

	// Verify "Extracting metadata..." progress was reported
	foundMetadataProgress := false
	for _, report := range progressReports {
		if report == "Extracting metadata..." {
			foundMetadataProgress = true
			break
		}
	}
	if !foundMetadataProgress {
		t.Errorf("Expected 'Extracting metadata...' in progress reports, got: %v", progressReports)
	}

	// Verify expected build phases are present
	expectedPhases := []string{
		"Installing dependencies...",
		"Extracting metadata...",
		"Bundling (Sync)...",
		"Compiling (Sync)...",
		"Optimizing (Sync)...",
		"Done",
	}

	for _, expectedPhase := range expectedPhases {
		found := false
		for _, report := range progressReports {
			if report == expectedPhase {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected progress phase '%s' not found in reports: %v", expectedPhase, progressReports)
		}
	}
}

// TestBuildAction_MetadataExtractionIntegration verifies the complete integration with mocked ExtractMetadataFunc
func TestBuildAction_MetadataExtractionIntegration(t *testing.T) {
	tests := []struct {
		name               string
		metadataError      error
		expectBuildSuccess bool
	}{
		{
			name:               "metadata extraction succeeds",
			metadataError:      nil,
			expectBuildSuccess: true,
		},
		{
			name:               "metadata extraction fails",
			metadataError:      errors.New("payload interface not found"),
			expectBuildSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock all dependencies
			origDeps := EnsureDependenciesFunc
			origExtract := ExtractMetadataFunc
			origBundle := BundleJSFunc
			origAsync := BundleAsyncFunc
			origCompile := CompileToWasmFunc
			origOpt := OptimizeWasmFunc
			origDetect := DetectActionLanguageFunc
			origParseEnv := ParseExecutionEnvironmentFunc
			defer func() {
				EnsureDependenciesFunc = origDeps
				ExtractMetadataFunc = origExtract
				BundleJSFunc = origBundle
				BundleAsyncFunc = origAsync
				CompileToWasmFunc = origCompile
				OptimizeWasmFunc = origOpt
				DetectActionLanguageFunc = origDetect
				ParseExecutionEnvironmentFunc = origParseEnv
			}()

			var metadataCallCount int
			var capturedFS fsx.FileSystem
			var capturedActionDir string

			EnsureDependenciesFunc = func(dir string) error { return nil }
			ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error {
				metadataCallCount++
				capturedFS = fs
				capturedActionDir = actionDir
				return tt.metadataError
			}
			BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
			BundleAsyncFunc = func(dir, entry, out string) error { return nil }
			CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
			OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
			DetectActionLanguageFunc = func(dir string) (ActionLanguage, error) { return LanguageTypeScript, nil }
			ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

			m := NewBuildManager(DefaultBuildOptions())
			m.tools.Javy = "javy"
			m.tools.WasmOpt = "wasm-opt"

			tmpDir := t.TempDir()
			actionDir := filepath.Join(tmpDir, "test-action")
			if err := os.MkdirAll(actionDir, 0755); err != nil {
				t.Fatal(err)
			}

			// Track progress reports
			var progressReports []string
			var mu sync.Mutex
			reporter := func(item, status string, done bool, err error) {
				mu.Lock()
				defer mu.Unlock()
				progressReports = append(progressReports, status)
			}

			result := m.BuildAction(context.Background(), actionDir, reporter)

			// Verify metadata extraction was called exactly once
			if metadataCallCount != 1 {
				t.Errorf("ExtractMetadataFunc called %d times, want 1", metadataCallCount)
			}

			// Verify correct parameters were passed
			if capturedActionDir != actionDir {
				t.Errorf("ExtractMetadataFunc called with actionDir %s, want %s", capturedActionDir, actionDir)
			}

			if capturedFS == nil {
				t.Error("ExtractMetadataFunc called with nil FileSystem")
			}

			// Verify build outcome
			if tt.expectBuildSuccess {
				if result.Error != nil {
					t.Errorf("Expected build success, got error: %v", result.Error)
				}
			} else {
				if result.Error == nil {
					t.Error("Expected build failure, got success")
				}
			}

			// A failure is never left in the progress view, which is repainted in
			// place and cleared when the run ends.
			for _, report := range progressReports {
				if strings.Contains(report, "warning") {
					t.Errorf("a build failure was reported as a progress warning: %q", report)
				}
			}
		})
	}
}

// TestBuildAction_UnrecognisedExecutionEnvironment pins the one failure a
// developer cannot see for themselves.
//
// Every step of every language's pipeline is guarded by needsSync or needsAsync,
// so an execution_environment that is neither server, client nor both used to
// walk the whole build doing nothing and report success over an empty build/
// directory — exit 0, no artifact, and nothing said until a deploy went looking
// for a module that was never written. Measured before the guard existed: a
// single mistyped word in the SCL produced `"status": "complete", "failed": 0`.
//
// The language is varied because the guard sits above the language branch: the
// same typo has to be refused whatever the action is written in, and neither
// compiler may be reached.
func TestBuildAction_UnrecognisedExecutionEnvironment(t *testing.T) {
	for _, lang := range []struct {
		name   string
		source string
		body   string
	}{
		{"typescript", filepath.Join("src", "index.ts"), "export default {}\n"},
		{"rust", filepath.Join("src", "main.rs"), "fn main() {}\n"},
	} {
		for _, execEnv := range []string{"Server", "serverr", "", "sever"} {
			t.Run(lang.name+"/"+execEnv, func(t *testing.T) {
				origParseEnv := ParseExecutionEnvironmentFunc
				origDeps := EnsureDependenciesFunc
				origCargo := EnsureCargoFunc
				t.Cleanup(func() {
					ParseExecutionEnvironmentFunc = origParseEnv
					EnsureDependenciesFunc = origDeps
					EnsureCargoFunc = origCargo
				})
				ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) {
					return execEnv, nil
				}
				// Neither toolchain may be asked for anything: the refusal is
				// about the record, and nothing on disk needs consulting.
				EnsureDependenciesFunc = func(dir string) error {
					t.Error("npm install ran for an action with no artifact to build")
					return nil
				}
				EnsureCargoFunc = func() (string, error) {
					t.Error("the Rust toolchain was consulted for an action with no artifact to build")
					return "", nil
				}

				actionDir := filepath.Join(t.TempDir(), "greet-user")
				if err := os.MkdirAll(filepath.Join(actionDir, "src"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(actionDir, lang.source), []byte(lang.body), 0644); err != nil {
					t.Fatal(err)
				}

				m := NewBuildManager(DefaultBuildOptions())
				result := m.BuildAction(context.Background(), actionDir, nil)

				if result.Error == nil {
					t.Fatalf("BuildAction() reported success for execution_environment %q", execEnv)
				}
				if !strings.Contains(result.Error.Error(), "execution_environment") {
					t.Errorf("the refusal should name the field, got: %v", result.Error)
				}
				for _, want := range []string{"server", "client", "both"} {
					if !strings.Contains(result.Error.Error(), want) {
						t.Errorf("the refusal should name %q as a valid value, got: %v", want, result.Error)
					}
				}
				if fileExists(filepath.Join(actionDir, "build")) {
					t.Error("an empty build directory was left behind by a refused build")
				}
			})
		}
	}
}

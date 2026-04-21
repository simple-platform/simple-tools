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
	origValidate := ValidateLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		ValidateLanguageFunc = origValidate
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error { return nil }
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

// TestBuildAction_MetadataExtractionCalled verifies that metadata extraction is called during build
func TestBuildAction_MetadataExtractionCalled(t *testing.T) {
	// Mock all dependencies
	origDeps := EnsureDependenciesFunc
	origExtract := ExtractMetadataFunc
	origBundle := BundleJSFunc
	origAsync := BundleAsyncFunc
	origCompile := CompileToWasmFunc
	origOpt := OptimizeWasmFunc
	origValidate := ValidateLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		ValidateLanguageFunc = origValidate
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
	ValidateLanguageFunc = func(dir string) error { return nil }
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

// TestBuildAction_MetadataExtractionFailure verifies that build continues when metadata extraction fails
func TestBuildAction_MetadataExtractionFailure(t *testing.T) {
	// Mock all dependencies
	origDeps := EnsureDependenciesFunc
	origExtract := ExtractMetadataFunc
	origBundle := BundleJSFunc
	origAsync := BundleAsyncFunc
	origCompile := CompileToWasmFunc
	origOpt := OptimizeWasmFunc
	origValidate := ValidateLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		ValidateLanguageFunc = origValidate
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	metadataError := errors.New("metadata extraction failed")
	var metadataCallCount int

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error {
		metadataCallCount++
		return metadataError
	}
	BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
	ValidateLanguageFunc = func(dir string) error { return nil }
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

	// Verify build succeeded despite metadata extraction failure
	if result.Error != nil {
		t.Errorf("BuildAction() error = %v, want nil (build should continue)", result.Error)
	}

	// Verify warning was logged in progress reports
	foundWarning := false
	for _, report := range progressReports {
		if strings.Contains(report, "Metadata extraction warning") && strings.Contains(report, "metadata extraction failed") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("Expected metadata extraction warning in progress reports, got: %v", progressReports)
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
	origValidate := ValidateLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		EnsureDependenciesFunc = origDeps
		ExtractMetadataFunc = origExtract
		BundleJSFunc = origBundle
		BundleAsyncFunc = origAsync
		CompileToWasmFunc = origCompile
		OptimizeWasmFunc = origOpt
		ValidateLanguageFunc = origValidate
		ParseExecutionEnvironmentFunc = origParseEnv
	}()

	EnsureDependenciesFunc = func(dir string) error { return nil }
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error { return nil }
	BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
	ValidateLanguageFunc = func(dir string) error { return nil }
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
		name                string
		metadataError       error
		expectBuildSuccess  bool
		expectWarningInLogs bool
	}{
		{
			name:                "metadata extraction succeeds",
			metadataError:       nil,
			expectBuildSuccess:  true,
			expectWarningInLogs: false,
		},
		{
			name:                "metadata extraction fails",
			metadataError:       errors.New("payload interface not found"),
			expectBuildSuccess:  true, // Build should continue
			expectWarningInLogs: true,
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
			origValidate := ValidateLanguageFunc
			origParseEnv := ParseExecutionEnvironmentFunc
			defer func() {
				EnsureDependenciesFunc = origDeps
				ExtractMetadataFunc = origExtract
				BundleJSFunc = origBundle
				BundleAsyncFunc = origAsync
				CompileToWasmFunc = origCompile
				OptimizeWasmFunc = origOpt
				ValidateLanguageFunc = origValidate
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
			ValidateLanguageFunc = func(dir string) error { return nil }
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

			// Verify warning logging
			foundWarning := false
			for _, report := range progressReports {
				if strings.Contains(report, "Metadata extraction warning") {
					foundWarning = true
					break
				}
			}

			if tt.expectWarningInLogs && !foundWarning {
				t.Errorf("Expected metadata extraction warning in logs, but not found. Reports: %v", progressReports)
			}

			if !tt.expectWarningInLogs && foundWarning {
				t.Errorf("Did not expect metadata extraction warning in logs, but found one. Reports: %v", progressReports)
			}
		})
	}
}

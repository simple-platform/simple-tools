package cli

import (
	"os"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"strings"
	"testing"
)

// TestRunBuild verifies the `simple build` command logic.
// It uses extensive mocking of the `build` package functions to avoid invalidating
// the test environment or taking excessive time with actual builds only to test CLI parsing.
func TestRunBuild(t *testing.T) {
	// === MOCKING START ===
	// Store original functions to restore them after the test
	origDeps := build.EnsureDependenciesFunc
	origBundle := build.BundleJSFunc
	origAsync := build.BundleAsyncFunc
	origCompile := build.CompileToWasmFunc
	origOpt := build.OptimizeWasmFunc

	defer func() {
		build.EnsureDependenciesFunc = origDeps
		build.BundleJSFunc = origBundle
		build.BundleAsyncFunc = origAsync
		build.CompileToWasmFunc = origCompile
		build.OptimizeWasmFunc = origOpt
	}()

	// Inject no-op mocks that simulate success
	build.EnsureDependenciesFunc = func(dir string) error { return nil }
	build.BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	build.BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	build.CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	build.OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }

	// Mock tool-check functions to avoid needing actual binaries (scl-parser, javy, etc.) in the test environment
	origSCL := build.EnsureSCLParserFunc
	origJavy := build.EnsureJavyFunc
	origWasm := build.EnsureWasmOptFunc
	defer func() {
		build.EnsureSCLParserFunc = origSCL
		build.EnsureJavyFunc = origJavy
		build.EnsureWasmOptFunc = origWasm
	}()
	build.EnsureSCLParserFunc = func(f func(string)) (string, error) { return "scl", nil }
	build.EnsureJavyFunc = func(f func(string)) (string, error) { return "javy", nil }
	build.EnsureWasmOptFunc = func(f func(string)) (string, error) { return "wasm-opt", nil }

	// The per-action steps are mocked too, so a case named for a successful
	// build is one. Left unmocked, the fixture action here has no source and
	// fails on the first step — which used to read as success because a failed
	// build reported as JSON returned no error at all.
	origValidate := build.ValidateLanguageFunc
	origParseEnv := build.ParseExecutionEnvironmentFunc
	origExtract := build.ExtractMetadataFunc
	defer func() {
		build.ValidateLanguageFunc = origValidate
		build.ParseExecutionEnvironmentFunc = origParseEnv
		build.ExtractMetadataFunc = origExtract
	}()
	build.ValidateLanguageFunc = func(dir string) error { return nil }
	build.ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }
	build.ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error {
		if strings.HasSuffix(actionDir, "refused") {
			return &build.AnnotationRefusal{
				Refusal: `refused: @effects names an unknown effect "sideways"`,
			}
		}

		return nil
	}
	// === MOCKING END ===

	// Define test cases covering various usage scenarios
	tests := []struct {
		name     string
		args     []string
		buildAll bool
		wantErr  bool
		errCheck func(error) bool
	}{
		{
			name:     "build all success",
			args:     []string{},
			buildAll: true, // User passed --all
			wantErr:  false,
		},
		{
			name:     "build all with args error",
			args:     []string{"target"},
			buildAll: true,
			wantErr:  true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "cannot use --all with a target argument")
			},
		},
		{
			name:     "build target success",
			args:     []string{"myapp/action"},
			buildAll: false,
			wantErr:  false,
		},
		{
			name:     "build no args error",
			args:     []string{},
			buildAll: false,
			wantErr:  true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "requires a target argument")
			},
		},
		// A BUILD FAILS THE SAME WAY IN BOTH OUTPUT MODES. These cases run with
		// the JSON flag set, which is the mode a pipeline uses — and the mode
		// that used to print a failure summary and then return success, so the
		// only reader that could act on the outcome was the one that could not
		// see it.
		{
			name:     "a refused annotation fails the build under --json",
			args:     []string{"myapp/refused"},
			buildAll: false,
			wantErr:  true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "1 action(s) failed to build")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Update global flag state for this run
			buildAll = tt.buildAll

			// Bypass UI output during tests to clean up logs and avoid TTY checks
			oldJSON := jsonOutput
			jsonOutput = true
			defer func() { jsonOutput = oldJSON }()

			// Isolate filesystem changes
			tmpDir := t.TempDir()
			oldWd, _ := os.Getwd()
			_ = os.Chdir(tmpDir)
			defer func() { _ = os.Chdir(oldWd) }()

			// Create dummy targets so the builder finds them
			switch tt.name {
			case "build target success":
				_ = os.MkdirAll("myapp/action", 0755)
				_ = os.WriteFile("myapp/action/action.scl", []byte{}, 0644)
			case "build all success":
				_ = os.MkdirAll("apps/myapp/action", 0755)
				_ = os.WriteFile("apps/myapp/action/action.scl", []byte{}, 0644)
			case "a refused annotation fails the build under --json":
				_ = os.MkdirAll("myapp/refused", 0755)
				_ = os.WriteFile("myapp/refused/action.scl", []byte{}, 0644)
			}

			// Execution
			err := runBuild(fsx.OSFileSystem{}, tt.args)

			// Verify results
			if (err != nil) != tt.wantErr {
				t.Errorf("runBuild() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errCheck != nil {
				if !tt.errCheck(err) {
					t.Errorf("runBuild() unexpected error = %v", err)
				}
			}
		})
	}

	// Reset global state
	buildAll = false
}

package cli

import (
	"os"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"strings"
	"testing"
)

func TestRunBuild(t *testing.T) {
	// Global Mocks for Build Package
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

	// No-op mocks
	build.EnsureDependenciesFunc = func(dir string) error { return nil }
	build.BundleJSFunc = func(dir, entry, out string, min bool, defs map[string]string) error { return nil }
	build.BundleAsyncFunc = func(dir, entry, out string) error { return nil }
	build.CompileToWasmFunc = func(javy, js, plugin, out string) error { return nil }
	build.OptimizeWasmFunc = func(opt, in, out string, flags []string) error { return nil }
	// Also mock tools ensuring
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
			buildAll: true,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			buildAll = tt.buildAll
			// Bypass UI in tests to avoid TTY error
			oldJSON := jsonOutput
			jsonOutput = true
			defer func() { jsonOutput = oldJSON }()
			tmpDir := t.TempDir()
			oldWd, _ := os.Getwd()
			_ = os.Chdir(tmpDir)
			defer func() { _ = os.Chdir(oldWd) }()

			// Create dummy targets for success cases
			if !tt.wantErr {
				if tt.name == "build target success" {
					_ = os.MkdirAll("myapp/action", 0755)
					_ = os.WriteFile("myapp/action/action.scl", []byte{}, 0644)
				}
				if tt.name == "build all success" {
					_ = os.MkdirAll("apps/myapp/action", 0755)
					_ = os.WriteFile("apps/myapp/action/action.scl", []byte{}, 0644)
				}
			}

			// Capture output
			err := runBuild(fsx.OSFileSystem{}, tt.args)
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
	// Reset global
	buildAll = false
}

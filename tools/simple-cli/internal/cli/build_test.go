package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"strings"
	"testing"
)

func TestProgressInputIsTerminalRejectsPipe(t *testing.T) {
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() { _ = input.Close() }()
	defer func() { _ = output.Close() }()

	if progressInputIsTerminal(input) {
		t.Fatal("pipe input was detected as an interactive terminal")
	}
}

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
	origDetect := build.DetectActionLanguageFunc
	origParseEnv := build.ParseExecutionEnvironmentFunc
	origExtract := build.ExtractMetadataFunc
	defer func() {
		build.DetectActionLanguageFunc = origDetect
		build.ParseExecutionEnvironmentFunc = origParseEnv
		build.ExtractMetadataFunc = origExtract
	}()
	build.DetectActionLanguageFunc = func(dir string) (build.ActionLanguage, error) {
		return build.LanguageTypeScript, nil
	}
	build.ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }
	build.ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) (string, error) {
		if strings.HasSuffix(actionDir, "noted") {
			return "noted: `link` format \"uri\" is left out", nil
		}
		if strings.HasSuffix(actionDir, "refused") {
			return "", &build.AnnotationRefusal{
				Refusal: `refused: @shortdesc is written with nothing after it`,
			}
		}

		return "", nil
	}
	// === MOCKING END ===

	// Define test cases covering various usage scenarios
	tests := []struct {
		name     string
		args     []string
		buildAll bool
		wantErr  bool
		errCheck func(error) bool
		// When set, the JSON document the run prints must carry exactly
		// these warnings.
		wantWarnings map[string]string
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
		// A note from the metadata step reaches the author without changing
		// the outcome: the build succeeds, and the note is in "warnings".
		{
			name:         "a metadata note is a warning under --json",
			args:         []string{"myapp/noted"},
			buildAll:     false,
			wantErr:      false,
			wantWarnings: map[string]string{"noted": "noted: `link` format \"uri\" is left out"},
		},
		{
			name:         "a build with no notes has no warnings under --json",
			args:         []string{"myapp/action"},
			buildAll:     false,
			wantErr:      false,
			wantWarnings: map[string]string{},
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

			// Create dummy targets so the builder finds them.
			//
			// A "success" case has to be an action the builder can reach the
			// end of, which means a source file in a language it compiles:
			// language detection is deliberately not mocked, so an action
			// directory with nothing in it fails, and a case named "success"
			// that builds nothing would prove nothing.
			switch tt.name {
			case "build target success", "a build with no notes has no warnings under --json":
				_ = os.MkdirAll("myapp/action/src", 0755)
				_ = os.WriteFile("myapp/action/action.scl", []byte{}, 0644)
				_ = os.WriteFile("myapp/action/src/index.ts", []byte("export {}\n"), 0644)
			case "build all success":
				_ = os.MkdirAll("apps/myapp/action", 0755)
				_ = os.WriteFile("apps/myapp/action/action.scl", []byte{}, 0644)
			case "a metadata note is a warning under --json":
				_ = os.MkdirAll("myapp/noted/src", 0755)
				_ = os.WriteFile("myapp/noted/action.scl", []byte{}, 0644)
				_ = os.WriteFile("myapp/noted/src/index.ts", []byte("export {}\n"), 0644)
			case "a refused annotation fails the build under --json":
				_ = os.MkdirAll("myapp/refused", 0755)
				_ = os.WriteFile("myapp/refused/action.scl", []byte{}, 0644)
			}

			// Execution
			var err error
			stdout := captureStdout(t, func() { err = runBuild(fsx.OSFileSystem{}, tt.args) })

			if tt.wantWarnings != nil {
				var summary struct {
					Warnings map[string]string `json:"warnings"`
				}
				if jsonErr := json.Unmarshal([]byte(stdout), &summary); jsonErr != nil {
					t.Fatalf("output is not one JSON document: %v\n%s", jsonErr, stdout)
				}
				if summary.Warnings == nil {
					t.Fatalf("output has no warnings member:\n%s", stdout)
				}
				if !maps.Equal(summary.Warnings, tt.wantWarnings) {
					t.Errorf("warnings = %v, want %v", summary.Warnings, tt.wantWarnings)
				}
			}

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

// captureStdout returns what run writes to the process's standard output.
func captureStdout(t *testing.T, run func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open a pipe: %v", err)
	}

	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	captured := make(chan string)
	go func() {
		data, _ := io.ReadAll(reader)
		captured <- string(data)
	}()

	run()
	_ = writer.Close()

	return <-captured
}

// Notes are written one action after another, in the order given, under the
// action's name; nothing is written when no action has notes.
func TestPrintBuildNotes(t *testing.T) {
	tests := []struct {
		name    string
		results []buildResult
		want    string
	}{
		{
			name:    "no notes prints nothing",
			results: []buildResult{{name: "a"}, {name: "b", err: errors.New("failed")}, {name: "s", isSpace: true}},
			want:    "",
		},
		{
			name: "notes from several actions stay grouped by action",
			results: []buildResult{
				{name: "first", notes: "first: one\nfirst: two"},
				{name: "quiet"},
				{name: "second", notes: "second: three"},
			},
			want: "\n⚠️  [Action] first:\n   first: one\n   first: two\n" +
				"\n⚠️  [Action] second:\n   second: three\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printBuildNotes(&out, tt.results)
			if out.String() != tt.want {
				t.Errorf("printed %q, want %q", out.String(), tt.want)
			}
		})
	}
}

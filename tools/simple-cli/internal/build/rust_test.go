package build

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"simple-cli/internal/fsx"
	"strings"
	"sync"
	"testing"
)

// cargoArtifactLine writes one of cargo's compiler-artifact messages, so the
// tests below read the same shape the real cargo emits rather than a shape
// invented here.
func cargoArtifactLine(t *testing.T, name string, kinds []string, filenames []string) string {
	t.Helper()
	msg := map[string]any{
		"reason": "compiler-artifact",
		"target": map[string]any{"name": name, "kind": kinds},
		// Real messages carry a dozen more fields; the parser has to ignore
		// them, so a couple of them are here to prove it does.
		"profile":   map[string]any{"opt_level": "z"},
		"fresh":     false,
		"filenames": filenames,
	}
	line, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

// TestRustModuleFromCargoOutput_PicksTheBinary pins that the module is read out
// of cargo's report and not assembled from the crate name: cargo leaves a
// binary's hyphens alone where it would replace a library's, so `greet-user`
// produces `greet-user.wasm` and a guessed `greet_user.wasm` names nothing.
func TestRustModuleFromCargoOutput_PicksTheBinary(t *testing.T) {
	want := "/w/greet-user/target/wasm32-wasip1/release/greet-user.wasm"

	output := strings.Join([]string{
		cargoArtifactLine(t, "serde", []string{"lib"}, []string{"/w/greet-user/target/wasm32-wasip1/release/deps/libserde-af47.rlib"}),
		cargoArtifactLine(t, "serde_derive", []string{"proc-macro"}, []string{"/w/greet-user/target/release/deps/libserde_derive-da7e.dylib"}),
		cargoArtifactLine(t, "greet-user", []string{"bin"}, []string{want}),
		`{"reason":"build-finished","success":true}`,
		"",
	}, "\n")

	got, err := rustModuleFromCargoOutput(output)
	if err != nil {
		t.Fatalf("rustModuleFromCargoOutput() error = %v", err)
	}
	if got != want {
		t.Errorf("rustModuleFromCargoOutput() = %s, want %s", got, want)
	}
}

func TestRustModuleFromCargoOutput_NoModule(t *testing.T) {
	output := cargoArtifactLine(t, "serde", []string{"lib"}, []string{"/w/target/deps/libserde.rlib"})

	_, err := rustModuleFromCargoOutput(output)
	if err == nil {
		t.Fatal("Expected an error when cargo reported no wasm binary, got nil")
	}
	if !strings.Contains(err.Error(), "no wasm module") {
		t.Errorf("Error should say no module was reported, got: %v", err)
	}
}

// TestRustModuleFromCargoOutput_TwoModules pins that several binaries are
// refused by name. Picking one of two would deploy an action nobody chose, and
// nothing downstream would report the substitution.
func TestRustModuleFromCargoOutput_TwoModules(t *testing.T) {
	output := strings.Join([]string{
		cargoArtifactLine(t, "one", []string{"bin"}, []string{"/w/target/wasm32-wasip1/release/one.wasm"}),
		cargoArtifactLine(t, "two", []string{"bin"}, []string{"/w/target/wasm32-wasip1/release/two.wasm"}),
	}, "\n")

	_, err := rustModuleFromCargoOutput(output)
	if err == nil {
		t.Fatal("Expected an error when cargo reported two wasm binaries, got nil")
	}
	if !strings.Contains(err.Error(), "one.wasm") || !strings.Contains(err.Error(), "two.wasm") {
		t.Errorf("Error should name both modules, got: %v", err)
	}
}

// rustAction lays out the smallest thing DetectActionLanguage calls Rust and
// buildRustAction agrees to build.
func rustAction(t *testing.T, dir string) string {
	t.Helper()
	actionDir := filepath.Join(dir, "greet-user")
	if err := os.MkdirAll(filepath.Join(actionDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionDir, "Cargo.toml"), []byte("[package]\nname = \"greet-user\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionDir, "src", "main.rs"), []byte("fn main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return actionDir
}

// cargoCall records one CargoBuildWasmFunc invocation.
type cargoCall struct {
	dir      string
	features []string
}

// rustBuildHarness swaps in the whole Rust toolchain seam and reports what the
// build path did with it. The stub cargo writes a module where a real one
// would, so the copy into build/ is exercised rather than mocked away.
type rustBuildHarness struct {
	mu        sync.Mutex
	cargo     []cargoCall
	optimized []string
	optFlags  []string
}

func withRustBuildHarness(t *testing.T, cargoErr error) *rustBuildHarness {
	t.Helper()
	h := &rustBuildHarness{}

	origDetect := DetectActionLanguageFunc
	origExtract := ExtractMetadataFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	origCargo := EnsureCargoFunc
	origTarget := EnsureRustWasmTargetFunc
	origBuild := CargoBuildWasmFunc
	origOpt := OptimizeWasmFunc
	t.Cleanup(func() {
		DetectActionLanguageFunc = origDetect
		ExtractMetadataFunc = origExtract
		ParseExecutionEnvironmentFunc = origParseEnv
		EnsureCargoFunc = origCargo
		EnsureRustWasmTargetFunc = origTarget
		CargoBuildWasmFunc = origBuild
		OptimizeWasmFunc = origOpt
	})

	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error { return nil }
	EnsureCargoFunc = func() (string, error) { return "/usr/bin/cargo", nil }
	EnsureRustWasmTargetFunc = func() error { return nil }

	CargoBuildWasmFunc = func(cargoPath, actionDir string, features []string) (string, error) {
		h.mu.Lock()
		h.cargo = append(h.cargo, cargoCall{dir: actionDir, features: features})
		h.mu.Unlock()
		if cargoErr != nil {
			return "", cargoErr
		}
		// Cargo writes both feature sets to the same path inside target/, which
		// is exactly why the build path has to copy each module out before
		// starting the next build. Writing to that one path here is what makes
		// a regression to parallel builds visible.
		module := filepath.Join(actionDir, "target", RustWasmTarget, "release", "greet-user.wasm")
		if err := os.MkdirAll(filepath.Dir(module), 0755); err != nil {
			return "", err
		}
		contents := "server-module"
		if len(features) > 0 {
			contents = "browser-module"
		}
		if err := os.WriteFile(module, []byte(contents), 0644); err != nil {
			return "", err
		}
		return module, nil
	}

	OptimizeWasmFunc = func(wasmOpt, in, out string, flags []string) error {
		h.mu.Lock()
		h.optimized = append(h.optimized, out)
		h.optFlags = flags
		h.mu.Unlock()
		return os.WriteFile(out, []byte("optimized"), 0644)
	}

	return h
}

func TestBuildAction_Rust(t *testing.T) {
	tests := []struct {
		name          string
		execEnv       string
		wantFeatures  [][]string
		wantArtifacts []string
		absentArtifat string
	}{
		{
			name:          "server builds only the sync artifact",
			execEnv:       "server",
			wantFeatures:  [][]string{nil},
			wantArtifacts: []string{"release.wasm"},
			absentArtifat: "release.async.wasm",
		},
		{
			name:          "client builds only the browser artifact",
			execEnv:       "client",
			wantFeatures:  [][]string{{RustAsyncFeature}},
			wantArtifacts: []string{"release.ori.async.wasm", "release.async.wasm"},
			absentArtifat: "release.wasm",
		},
		{
			name:          "both builds two",
			execEnv:       "both",
			wantFeatures:  [][]string{nil, {RustAsyncFeature}},
			wantArtifacts: []string{"release.wasm", "release.ori.async.wasm", "release.async.wasm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := withRustBuildHarness(t, nil)
			ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return tt.execEnv, nil }

			actionDir := rustAction(t, t.TempDir())
			m := NewBuildManager(DefaultBuildOptions())
			m.tools.WasmOpt = "wasm-opt"

			result := m.BuildAction(context.Background(), actionDir, nil)
			if result.Error != nil {
				t.Fatalf("BuildAction() error = %v", result.Error)
			}

			if len(h.cargo) != len(tt.wantFeatures) {
				t.Fatalf("cargo ran %d times, want %d", len(h.cargo), len(tt.wantFeatures))
			}
			for i, want := range tt.wantFeatures {
				if strings.Join(h.cargo[i].features, ",") != strings.Join(want, ",") {
					t.Errorf("cargo run %d used features %v, want %v", i, h.cargo[i].features, want)
				}
				if h.cargo[i].dir != actionDir {
					t.Errorf("cargo run %d ran in %s, want %s", i, h.cargo[i].dir, actionDir)
				}
			}

			for _, name := range tt.wantArtifacts {
				if !fileExists(filepath.Join(actionDir, "build", name)) {
					t.Errorf("build/%s was not written", name)
				}
			}
			if tt.absentArtifat != "" && fileExists(filepath.Join(actionDir, "build", tt.absentArtifat)) {
				t.Errorf("build/%s was written for execution environment %q", tt.absentArtifat, tt.execEnv)
			}
		})
	}
}

// TestBuildAction_Rust_ServerArtifactSurvivesTheBrowserBuild pins the ordering
// the two builds need. Cargo writes both feature sets to one path in target/,
// so a browser build that ran alongside the server build — or before its module
// was copied out — would leave build/release.wasm holding the browser module,
// and the server would run an action compiled against imports it does not bind.
func TestBuildAction_Rust_ServerArtifactSurvivesTheBrowserBuild(t *testing.T) {
	withRustBuildHarness(t, nil)
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "both", nil }

	actionDir := rustAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())
	m.tools.WasmOpt = "wasm-opt"

	if result := m.BuildAction(context.Background(), actionDir, nil); result.Error != nil {
		t.Fatalf("BuildAction() error = %v", result.Error)
	}

	got, err := os.ReadFile(filepath.Join(actionDir, "build", "release.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "server-module" {
		t.Errorf("build/release.wasm holds %q, want the server module", string(got))
	}

	got, err = os.ReadFile(filepath.Join(actionDir, "build", "release.ori.async.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "browser-module" {
		t.Errorf("build/release.ori.async.wasm holds %q, want the browser module", string(got))
	}
}

// TestBuildAction_Rust_BrowserFlags pins the flags the browser artifact is
// optimised with. Rust's wasm32-wasip1 output uses bulk memory and sign
// extension, and binaryen validates a module only against the features it has
// been told to enable, so dropping one of these turns every browser build into
// "memory.copy operations require bulk memory operations".
func TestBuildAction_Rust_BrowserFlags(t *testing.T) {
	h := withRustBuildHarness(t, nil)
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "client", nil }

	actionDir := rustAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())
	m.tools.WasmOpt = "wasm-opt"

	if result := m.BuildAction(context.Background(), actionDir, nil); result.Error != nil {
		t.Fatalf("BuildAction() error = %v", result.Error)
	}

	required := []string{
		"-Oz",
		"--disable-gc",
		"--enable-bulk-memory",
		"--enable-bulk-memory-opt",
		"--enable-sign-ext",
		"--enable-nontrapping-float-to-int",
		"--enable-multivalue",
		"--enable-reference-types",
		"--asyncify",
		"--pass-arg=asyncify-imports@simple.__call",
	}
	for _, flag := range required {
		found := false
		for _, got := range h.optFlags {
			if got == flag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("wasm-opt was not given %s; flags were %v", flag, h.optFlags)
		}
	}
}

// TestBuildAction_Rust_MissingToolchain pins that a machine without Rust reads
// as a machine without Rust, and that nothing is compiled on the strength of a
// toolchain that is not there.
func TestBuildAction_Rust_MissingToolchain(t *testing.T) {
	tests := []struct {
		name    string
		arrange func()
		want    string
	}{
		{
			name: "no cargo",
			arrange: func() {
				EnsureCargoFunc = func() (string, error) {
					return "", errors.New("cargo was not found on PATH, and this action is written in Rust. Install a Rust toolchain (https://rustup.rs), then build again")
				}
			},
			want: "cargo was not found",
		},
		{
			name: "no wasm target",
			arrange: func() {
				EnsureRustWasmTargetFunc = func() error {
					return errors.New("the wasm32-wasip1 target is not installed. Add it with 'rustup target add wasm32-wasip1', then build again")
				}
			},
			want: "rustup target add",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := withRustBuildHarness(t, nil)
			ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }
			tt.arrange()

			actionDir := rustAction(t, t.TempDir())
			m := NewBuildManager(DefaultBuildOptions())
			m.tools.WasmOpt = "wasm-opt"

			result := m.BuildAction(context.Background(), actionDir, nil)
			if result.Error == nil {
				t.Fatal("Expected an error for a missing toolchain, got nil")
			}
			if !strings.Contains(result.Error.Error(), tt.want) {
				t.Errorf("Error should say how to fix it (%q), got: %v", tt.want, result.Error)
			}
			if len(h.cargo) != 0 {
				t.Errorf("cargo ran %d times despite the missing toolchain", len(h.cargo))
			}
		})
	}
}

// TestBuildAction_Rust_MissingManifest pins that a Rust source with no manifest
// is named. Left to cargo, the build would climb out of the action directory
// and find whichever Cargo.toml sits above it.
func TestBuildAction_Rust_MissingManifest(t *testing.T) {
	h := withRustBuildHarness(t, nil)
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	actionDir := rustAction(t, t.TempDir())
	if err := os.Remove(filepath.Join(actionDir, "Cargo.toml")); err != nil {
		t.Fatal(err)
	}

	m := NewBuildManager(DefaultBuildOptions())
	m.tools.WasmOpt = "wasm-opt"

	result := m.BuildAction(context.Background(), actionDir, nil)
	if result.Error == nil {
		t.Fatal("Expected an error for a Rust action with no Cargo.toml, got nil")
	}
	if !strings.Contains(result.Error.Error(), "Cargo.toml") {
		t.Errorf("Error should name the missing manifest, got: %v", result.Error)
	}
	if len(h.cargo) != 0 {
		t.Errorf("cargo ran %d times despite the missing manifest", len(h.cargo))
	}
}

// A RUST ACTION THAT CANNOT BE DESCRIBED DOES NOT BUILD, WHICH IS WHAT THE
// TYPESCRIPT PATH ALREADY DID.
//
// This pinned the opposite, from when the generator had no Rust branch and the
// step could not produce a description for a Rust action at all. The branch has
// landed, and TestBuildAction_MetadataExtractionFailureStopsTheBuild holds the
// TypeScript path to returning the failure — so what the old expectation was
// preserving was one CLI answering a single malformed exposure statement two
// ways depending on what the action was written in.
//
// Measured through the built binary, on a Rust action whose only defect was
// `@tool true`: the progress row naming the refusal was overwritten by the next
// step, the build printed Done and exited 0, `--json` reported `"failed": 0`,
// and the refusal reached the developer nowhere. The same source in TypeScript
// came back as `"failed": 1` carrying the generator's sentence.
//
// The refusal had also DISCARDED the action.json generated from the earlier
// source, so what shipped was a module with no description, no input schema and
// no statement about whether an agent may call it — from a build that reported
// success. That is what makes swallowing it worse here than it ever was on the
// TypeScript path.
//
// The two halves are asserted together: the failure comes back whole, AND the
// compile after it was never reached, because a build that fails at the end has
// still done the work of a build that succeeded.
func TestBuildAction_Rust_MetadataFailureStopsTheBuild(t *testing.T) {
	h := withRustBuildHarness(t, nil)
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	refusal := &AnnotationRefusal{
		Refusal: `greet-user: @tool is a modifier tag and takes no value, and this one carries "true"`,
	}
	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error { return refusal }

	actionDir := rustAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())
	m.tools.WasmOpt = "wasm-opt"

	result := m.BuildAction(context.Background(), actionDir, nil)

	if result.Error == nil {
		t.Fatal("BuildAction() error = nil, want the refusal (a malformed annotation must fail the build)")
	}

	// The refusal reaches the caller as the sentence its author has to act on,
	// not as a category the caller then has to go looking for the detail of.
	if !errors.Is(result.Error, error(refusal)) {
		t.Errorf("BuildAction() error = %v, want it to carry the refusal", result.Error)
	}

	if !strings.Contains(result.Error.Error(), "modifier tag and takes no value") {
		t.Errorf("BuildAction() error = %q, want the refusal text an author can act on", result.Error)
	}

	// Nothing after the failed gate ran.
	if len(h.cargo) != 0 {
		t.Errorf("cargo ran %d times past the refusal", len(h.cargo))
	}
	if fileExists(filepath.Join(actionDir, "build", "release.wasm")) {
		t.Error("a module was written for an action the build could not describe")
	}
}

func TestBuildAction_Rust_CargoFailureReported(t *testing.T) {
	withRustBuildHarness(t, errors.New("cargo build failed: error[E0308]: mismatched types"))
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	actionDir := rustAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())
	m.tools.WasmOpt = "wasm-opt"

	result := m.BuildAction(context.Background(), actionDir, nil)
	if result.Error == nil {
		t.Fatal("Expected an error when cargo fails, got nil")
	}
	if !strings.Contains(result.Error.Error(), "E0308") {
		t.Errorf("Error should carry what cargo said, got: %v", result.Error)
	}
}

// TestBuildAction_Go pins that a Go action is refused rather than reported as
// built. It is discovered like any other action, and a build that says nothing
// about it leaves a developer waiting on an artifact this CLI was never going
// to write.
func TestBuildAction_Go(t *testing.T) {
	origDetect := DetectActionLanguageFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	defer func() {
		DetectActionLanguageFunc = origDetect
		ParseExecutionEnvironmentFunc = origParseEnv
	}()
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	actionDir := filepath.Join(t.TempDir(), "sync-orders")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionDir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewBuildManager(DefaultBuildOptions())
	result := m.BuildAction(context.Background(), actionDir, nil)

	if result.Error == nil {
		t.Fatal("Expected an error for a Go action, got nil")
	}
	if !strings.Contains(result.Error.Error(), "Go") || !strings.Contains(result.Error.Error(), "platform") {
		t.Errorf("Error should name the language and where the artifact comes from, got: %v", result.Error)
	}
	if fileExists(filepath.Join(actionDir, "build")) {
		t.Error("A build directory was created for an action this CLI does not compile")
	}
}

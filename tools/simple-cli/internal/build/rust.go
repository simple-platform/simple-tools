package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"simple-cli/internal/fsx"
)

const (
	// RustWasmTarget is the only target a Rust action is compiled for. The
	// platform's runtime binds WASI preview 1 imports, so a module built for
	// anything else would load and then fail to find the host.
	RustWasmTarget = "wasm32-wasip1"

	// RustAsyncFeature is the cargo feature that selects the browser import
	// set. It is a feature of the action's own crate, which hands the flag on
	// to the SDK: cargo resolves --features against the crate being built, so
	// the action is where the name has to exist.
	RustAsyncFeature = "async"
)

// rustBrowserWasmOptFlags is what wasm-opt is given for the browser artifact.
//
// The --enable-* flags are not decoration. Rust's wasm32-wasip1 target emits
// bulk memory, sign extension and the rest by default, and binaryen validates a
// module only against the features it has been told to enable — so without them
// wasm-opt rejects the module it was just handed with "memory.copy operations
// require bulk memory operations" and the browser artifact cannot be produced
// at all. --asyncify and its argument are what park and resume the module
// across a host call, which is the whole reason the browser build exists.
var rustBrowserWasmOptFlags = []string{
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

// buildRustAction compiles a Rust action into the same two artifacts, under the
// same two names, that the TypeScript path writes: build/release.wasm for the
// server and build/release.async.wasm for the browser. Which of them are
// produced is the action's execution environment, decided by the caller, so
// `server`, `client` and `both` mean the same thing whatever the action is
// written in.
//
// A Rust action is a crate, so there is no dependency install and no bundling
// step: cargo resolves and fetches what the manifest names as part of building
// it, and the module comes out of the crate itself.
func (m *BuildManager) buildRustAction(actionDir, actionName string, needsSync, needsAsync bool, report func(string)) ActionBuildResult {
	fail := func(err error) ActionBuildResult {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: err}
	}

	// The toolchain is checked before anything is read or written, so that a
	// machine without Rust on it reads as a machine without Rust on it rather
	// than as an action that will not compile.
	report("Checking Rust toolchain...")
	cargoPath, err := EnsureCargoFunc()
	if err != nil {
		return fail(err)
	}
	if err := EnsureRustWasmTargetFunc(); err != nil {
		return fail(err)
	}
	if needsAsync && m.tools.WasmOpt == "" {
		return fail(fmt.Errorf("wasm-opt is not available, and this action's execution environment needs the browser artifact"))
	}

	// src/main.rs is what said this action is Rust; the manifest is what cargo
	// needs to act on it. Naming the missing file beats letting cargo answer
	// from whichever directory above this one happens to hold a Cargo.toml.
	if !fileExists(filepath.Join(actionDir, "Cargo.toml")) {
		return fail(fmt.Errorf("this action has src/main.rs but no Cargo.toml: a Rust action is a crate, and cargo has nothing to build without its manifest"))
	}

	// AN ACTION THAT CANNOT BE DESCRIBED FROM ITS OWN SOURCE DOES NOT BUILD,
	// WHICH IS THE SAME SENTENCE THE TYPESCRIPT PATH ALREADY ENFORCES.
	//
	// This reported the failure into a progress row and carried on, because
	// there was no Rust branch in the generator yet: nothing here could produce
	// a description, and failing a build over a step that was not going to run
	// was a worse trade than saying so and compiling the module. That branch has
	// since landed, so the reason is gone and what is left is one CLI answering
	// a single malformed exposure statement two different ways depending on the
	// language the action happens to be written in.
	//
	// Measured through the built binary, on an action whose only defect was
	// `@tool true`: the row naming the refusal was overwritten by "Compiling
	// (Sync)..." milliseconds later, the build printed Done and exited 0, and
	// `--json` reported `"failed": 0`. The same defect in TypeScript came back
	// as `"failed": 1` carrying the generator's sentence — the same generator,
	// the same sentence, delivered to nobody.
	//
	// Swallowing it costs more here than the equivalent ever cost there, because
	// a refusal DISCARDS the action.json generated from the earlier source. A
	// TypeScript action that swallowed its refusal at least shipped a stale
	// description; a Rust one ships a module with no description, no input
	// schema and no statement about whether an agent may call it at all, from a
	// build that reported success.
	//
	// The failure is carried out whole rather than summarised: a refusal is the
	// exact sentence its author has to read to fix their source, and every other
	// failure this step raises already names what could not be done.
	report("Extracting metadata...")
	if err := ExtractMetadataFunc(fsx.OSFileSystem{}, actionDir); err != nil {
		return fail(err)
	}

	buildDir := filepath.Join(actionDir, "build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fail(fmt.Errorf("failed to create build directory: %w", err))
	}

	// The two builds run one after the other, where the TypeScript path runs
	// its two in parallel. They differ only by a feature flag, so cargo writes
	// both of them to the same path inside target/ — running them together
	// would have each overwrite the other's module, and cargo would in any case
	// block on its own package lock. Each module is therefore copied out of
	// target/ before the next build starts.
	if needsSync {
		report("Compiling (Sync)...")
		module, err := CargoBuildWasmFunc(cargoPath, actionDir, nil)
		if err != nil {
			return fail(fmt.Errorf("sync compile: %w", err))
		}
		// No wasm-opt for the server artifact: the release profile an action
		// declares already optimises for size and strips the symbols, and there
		// is no asyncify transform to apply.
		if err := copyFile(module, filepath.Join(buildDir, "release.wasm")); err != nil {
			return fail(fmt.Errorf("sync compile: failed to copy the built module into build/: %w", err))
		}
	}

	if needsAsync {
		report("Compiling (Async)...")
		module, err := CargoBuildWasmFunc(cargoPath, actionDir, []string{RustAsyncFeature})
		if err != nil {
			return fail(fmt.Errorf("async compile: %w", err))
		}

		asyncOriginal := filepath.Join(buildDir, "release.ori.async.wasm")
		if err := copyFile(module, asyncOriginal); err != nil {
			return fail(fmt.Errorf("async compile: failed to copy the built module into build/: %w", err))
		}

		report("Optimizing (Async)...")
		if err := OptimizeWasmFunc(m.tools.WasmOpt, asyncOriginal,
			filepath.Join(buildDir, "release.async.wasm"), rustBrowserWasmOptFlags); err != nil {
			return fail(fmt.Errorf("async optimize: %w", err))
		}
	}

	report("Done")
	return ActionBuildResult{ActionName: actionName, Error: nil}
}

// CargoBuildWasm builds the crate in actionDir for RustWasmTarget and answers
// with the path of the module cargo wrote.
//
// The path is read out of cargo's own report rather than assembled from the
// crate name, because the two do not agree: cargo renames a library's file to
// use underscores but leaves a binary's alone, so a crate called `greet-user`
// produces `greet-user.wasm` and a guess at `greet_user.wasm` finds nothing.
// --message-format=json-render-diagnostics is what makes cargo report it, and
// the "render-diagnostics" half keeps compile errors as the text a developer
// reads instead of turning them into JSON this would then have to unpack.
func CargoBuildWasm(cargoPath, actionDir string, features []string) (string, error) {
	args := []string{
		"build",
		"--target", RustWasmTarget,
		"--release",
		"--message-format=json-render-diagnostics",
	}
	if len(features) > 0 {
		args = append(args, "--features", strings.Join(features, ","))
	}

	cmd := exec.Command(cargoPath, args...)
	cmd.Dir = actionDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cargo build failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}

	return rustModuleFromCargoOutput(stdout.String())
}

// cargoArtifact is the part of a cargo `compiler-artifact` message this reads.
// Everything else in the message is ignored on purpose, so a new field in a
// later cargo cannot break the build.
type cargoArtifact struct {
	Reason string `json:"reason"`
	Target struct {
		Name string   `json:"name"`
		Kind []string `json:"kind"`
	} `json:"target"`
	Filenames []string `json:"filenames"`
}

// rustModuleFromCargoOutput picks the action's module out of cargo's report.
//
// Every dependency reports an artifact too, so the module is the one that is a
// binary and a .wasm: dependencies are libraries and proc-macros, and the
// proc-macros are built for the host. An action is one module, so a crate that
// produced several is refused by name rather than resolved by picking one —
// deploying the wrong binary of two would be silent, and a build that shipped
// the wrong action is worse than a build that stopped.
func rustModuleFromCargoOutput(output string) (string, error) {
	var modules []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	// A cargo message carries every output filename of an artifact, so the
	// longest lines grow with the crate rather than staying under the scanner's
	// default 64 KiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var artifact cargoArtifact
		if err := json.Unmarshal([]byte(line), &artifact); err != nil {
			continue
		}
		if artifact.Reason != "compiler-artifact" || !slices.Contains(artifact.Target.Kind, "bin") {
			continue
		}

		for _, name := range artifact.Filenames {
			if strings.HasSuffix(name, ".wasm") {
				modules = append(modules, name)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("could not read cargo's build report: %w", err)
	}

	switch len(modules) {
	case 1:
		return modules[0], nil
	case 0:
		return "", fmt.Errorf("cargo reported no wasm module for this action: a Rust action's crate has to produce a binary, which is what src/main.rs is")
	default:
		return "", fmt.Errorf("cargo produced %d wasm modules (%s), and an action is one module: give the crate a single binary target",
			len(modules), strings.Join(modules, ", "))
	}
}

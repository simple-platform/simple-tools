package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	internalRuntime "simple-cli/internal/runtime"
	"sync"
)

// Mockable dependencies
var (
	EnsureSCLParserFunc           = EnsureSCLParser
	EnsureJavyFunc                = EnsureJavy
	EnsureWasmOptFunc             = EnsureWasmOpt
	EnsureDependenciesFunc        = EnsureDependencies
	BundleJSFunc                  = BundleJS
	BundleAsyncFunc               = BundleAsync
	CompileToWasmFunc             = CompileToWasm
	OptimizeWasmFunc              = OptimizeWasm
	ValidateLanguageFunc          = ValidateLanguage
	ParseExecutionEnvironmentFunc = ParseExecutionEnvironment
)

type ProgressReporter func(item, status string, done bool, err error)

type BuildOptions struct {
	Concurrency int
	Verbose     bool
	JSONOutput  bool
}

func DefaultBuildOptions() BuildOptions {
	return BuildOptions{
		Concurrency: 4,
		Verbose:     true,
		JSONOutput:  false,
	}
}

type BuildManager struct {
	options   BuildOptions
	tools     ToolPaths
	toolsErr  error
	toolsOnce sync.Once
}

type ToolPaths struct {
	SCLParser          string
	Javy               string
	WasmOpt            string
	RuntimePluginSync  string
	RuntimePluginAsync string
}

func NewBuildManager(opts BuildOptions) *BuildManager {
	if opts.Concurrency <= 0 {
		opts.Concurrency = runtime.NumCPU() // Default to number of CPUs for optimal parallelization
	}
	return &BuildManager{
		options: opts,
	}
}

func (m *BuildManager) EnsureTools(onProgress ProgressReporter) error {
	m.toolsOnce.Do(func() {
		var wg sync.WaitGroup
		var mu sync.Mutex
		errors := make([]error, 3)

		wg.Add(3)

		checkTool := func(index int, name string, ensureFn func(func(string)) (string, error)) {
			defer wg.Done()

			onStatus := func(status string) {
				if onProgress != nil {
					onProgress(name, status, false, nil)
				}
			}

			if onProgress != nil {
				onProgress(name, "Checking...", false, nil)
			}
			path, err := ensureFn(onStatus)
			if onProgress != nil {
				onProgress(name, "Done", true, err)
			}
			mu.Lock()
			switch name {
			case "scl-parser":
				m.tools.SCLParser = path
			case "javy":
				m.tools.Javy = path
			case "wasm-opt":
				m.tools.WasmOpt = path
			}
			errors[index] = err
			mu.Unlock()
		}

		go checkTool(0, "scl-parser", EnsureSCLParserFunc)
		go checkTool(1, "javy", EnsureJavyFunc)
		go checkTool(2, "wasm-opt", EnsureWasmOptFunc)

		wg.Wait()

		for _, err := range errors {
			if err != nil {
				m.toolsErr = err
				return
			}
		}

		// Extract runtime plugin
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "."
		}
		runtimeDir := filepath.Join(homeDir, ".simple", "runtime")
		if err := os.MkdirAll(runtimeDir, 0755); err != nil {
			m.toolsErr = fmt.Errorf("failed to create runtime dir: %w", err)
			return
		}

		pluginPathSync, err := internalRuntime.EnsurePlugin(runtimeDir, false)
		if err != nil {
			m.toolsErr = fmt.Errorf("failed to extract sync runtime plugin: %w", err)
			return
		}
		m.tools.RuntimePluginSync = pluginPathSync

		pluginPathAsync, err := internalRuntime.EnsurePlugin(runtimeDir, true)
		if err != nil {
			m.toolsErr = fmt.Errorf("failed to extract async runtime plugin: %w", err)
			return
		}
		m.tools.RuntimePluginAsync = pluginPathAsync
	})
	return m.toolsErr
}

type ActionBuildResult struct {
	ActionName string
	Error      error
}

func (m *BuildManager) BuildActions(ctx context.Context, actionDirs []string, onProgress ProgressReporter) []ActionBuildResult {
	results := make([]ActionBuildResult, len(actionDirs))
	sem := make(chan struct{}, m.options.Concurrency)
	var wg sync.WaitGroup

	for i, dir := range actionDirs {
		wg.Add(1)
		go func(i int, dir string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := m.BuildAction(ctx, dir, onProgress)
			results[i] = res
		}(i, dir)
	}

	wg.Wait()
	return results
}

func (m *BuildManager) BuildAction(ctx context.Context, actionDir string, onProgress ProgressReporter) ActionBuildResult {
	actionName := filepath.Base(actionDir)

	report := func(status string) {
		if onProgress != nil {
			onProgress(actionName, status, false, nil)
		}
	}

	// Validate language (TS only)
	if err := ValidateLanguageFunc(actionDir); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: err}
	}

	// Parse execution environment from SCL
	execEnv, _ := ParseExecutionEnvironmentFunc(m.tools.SCLParser, actionDir)
	needsSync := execEnv == "server" || execEnv == "both"
	needsAsync := execEnv == "client" || execEnv == "both"

	// Install dependencies
	report("Installing dependencies...")
	if err := EnsureDependenciesFunc(actionDir); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("npm install failed: %w", err)}
	}

	// Create build directory
	buildDir := filepath.Join(actionDir, "build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("failed to create build directory: %w", err)}
	}

	// PARALLEL: Bundle
	var wg sync.WaitGroup
	var syncBundleErr, asyncBundleErr error
	syncBundle := filepath.Join(buildDir, "bundle.sync.js")
	asyncBundle := filepath.Join(buildDir, "bundle.async.js")

	if needsSync {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report("Bundling (Sync)...")
			syncBundleErr = BundleJSFunc(actionDir, "index.ts", syncBundle, true,
				map[string]string{"__ASYNC_BUILD__": "false"})
		}()
	}
	if needsAsync {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report("Bundling (Async)...")
			asyncBundleErr = BundleAsyncFunc(actionDir, "index.ts", asyncBundle)
		}()
	}
	wg.Wait()

	if syncBundleErr != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("sync bundle: %w", syncBundleErr)}
	}
	if asyncBundleErr != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("async bundle: %w", asyncBundleErr)}
	}

	// PARALLEL: Compile
	var syncCompileErr, asyncCompileErr error
	syncWasmOri := filepath.Join(actionDir, "build", "release.ori.sync.wasm")
	asyncWasmOri := filepath.Join(actionDir, "build", "release.ori.async.wasm")

	if needsSync {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report("Compiling (Sync)...")
			syncCompileErr = CompileToWasmFunc(m.tools.Javy, syncBundle, m.tools.RuntimePluginSync, syncWasmOri)
		}()
	}
	if needsAsync {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report("Compiling (Async)...")
			asyncCompileErr = CompileToWasmFunc(m.tools.Javy, asyncBundle, m.tools.RuntimePluginAsync, asyncWasmOri)
		}()
	}
	wg.Wait()

	if syncCompileErr != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("sync compile: %w", syncCompileErr)}
	}
	if asyncCompileErr != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("async compile: %w", asyncCompileErr)}
	}

	// PARALLEL: Optimize
	var syncOptErr, asyncOptErr error

	if needsSync {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report("Optimizing (Sync)...")
			syncOptErr = OptimizeWasmFunc(m.tools.WasmOpt, syncWasmOri,
				filepath.Join(actionDir, "build", "release.wasm"),
				[]string{"-Oz", "--disable-gc"})
		}()
	}
	if needsAsync {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report("Optimizing (Async)...")
			asyncOptErr = OptimizeWasmFunc(m.tools.WasmOpt, asyncWasmOri,
				filepath.Join(actionDir, "build", "release.async.wasm"),
				[]string{"-Oz", "--disable-gc", "--asyncify",
					// Enable asyncify for the async build. The asyncify-imports argument declares
					// simple.__call as a host import that can suspend/resume execution, so wasm-opt
					// treats calls through this import as async boundaries when transforming the module.
					"--pass-arg=asyncify-imports@simple.__call"})
		}()
	}
	wg.Wait()

	if syncOptErr != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("sync optimize: %w", syncOptErr)}
	}
	if asyncOptErr != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("async optimize: %w", asyncOptErr)}
	}

	report("Done")
	return ActionBuildResult{ActionName: actionName, Error: nil}
}

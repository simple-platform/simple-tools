package build

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// Mockable dependencies
var (
	EnsureSCLParserFunc    = EnsureSCLParser
	EnsureJavyFunc         = EnsureJavy
	EnsureWasmOptFunc      = EnsureWasmOpt
	EnsureDependenciesFunc = EnsureDependencies
	BundleActionFunc       = BundleAction
	CompileToWasmFunc      = CompileToWasm
	OptimizeWasmFunc       = OptimizeWasm
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
	SCLParser string
	Javy      string
	WasmOpt   string
}

func NewBuildManager(opts BuildOptions) *BuildManager {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
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

	report("Installing dependencies...")
	if err := EnsureDependenciesFunc(actionDir); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("npm install failed: %w", err)}
	}

	report("Bundling...")
	jsBundle, err := BundleActionFunc(actionDir)
	if err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("bundle failed: %w", err)}
	}

	report("Compiling WASM...")
	wasmPath := filepath.Join(actionDir, "build", "index.wasm")
	if err := CompileToWasmFunc(m.tools.Javy, jsBundle, "", wasmPath); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("compilation failed: %w", err)}
	}

	report("Optimizing...")
	optWasmPath := filepath.Join(actionDir, "build", "index.opt.wasm")
	if err := OptimizeWasmFunc(m.tools.WasmOpt, wasmPath, optWasmPath, true); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("optimization failed: %w", err)}
	}

	report("Done")
	return ActionBuildResult{ActionName: actionName, Error: nil}
}

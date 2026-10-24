package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"simple-cli/internal/fsx"
	"simple-cli/internal/home"
	internalRuntime "simple-cli/internal/runtime"
	"sync"
)

// Mockable dependencies
var (
	EnsureSCLParserFunc           = EnsureSCLParser
	EnsureJavyFunc                = EnsureJavy
	EnsureWasmOptFunc             = EnsureWasmOpt
	EnsureDependenciesFunc        = EnsureDependencies
	ExtractMetadataFunc           = ExtractMetadata
	BundleJSFunc                  = BundleJS
	BundleAsyncFunc               = BundleAsync
	CompileToWasmFunc             = CompileToWasm
	OptimizeWasmFunc              = OptimizeWasm
	DetectActionLanguageFunc      = DetectActionLanguage
	ParseExecutionEnvironmentFunc = ParseExecutionEnvironment
	EnsureCargoFunc               = EnsureCargo
	EnsureRustWasmTargetFunc      = EnsureRustWasmTarget
	CargoBuildWasmFunc            = CargoBuildWasm
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

func (m *BuildManager) BuildConcurrency() int {
	return m.options.Concurrency
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
		homeDir, err := home.Dir()
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

	lang, err := DetectActionLanguageFunc(actionDir)
	if err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: err}
	}

	// Parse execution environment from SCL.
	//
	// This is read before the language branch and passed into it, because it is
	// the same decision for every language: `server` needs the sync artifact,
	// `client` the async one, `both` needs two. Only how those artifacts are
	// produced differs below.
	execEnv, _ := ParseExecutionEnvironmentFunc(m.tools.SCLParser, actionDir)
	needsSync := execEnv == "server" || execEnv == "both"
	needsAsync := execEnv == "client" || execEnv == "both"

	// An environment that names neither artifact is refused here, before any
	// compiler is asked for one.
	//
	// The three values are matched exactly, so anything else — a typo, a
	// capital letter, a value some later version of the platform understands
	// and this CLI does not — leaves both flags false. Every step below is
	// guarded by one of those flags, so the build would otherwise walk the
	// whole pipeline doing nothing and report success over an empty build/
	// directory. That is the one failure a developer cannot see: the command
	// said it worked, and the artifact that never appeared is only missed at
	// deploy time. The value read is quoted back because the fix is a single
	// word in the SCL and naming it is what points at that word.
	if !needsSync && !needsAsync {
		report("Failed")
		return ActionBuildResult{
			ActionName: actionName,
			Error: fmt.Errorf("execution_environment is %q, which names no artifact to build: it has to be exactly server, client, or both",
				execEnv),
		}
	}

	switch lang {
	case LanguageTypeScript:
		return m.buildTypeScriptAction(actionDir, actionName, needsSync, needsAsync, report)
	case LanguageRust:
		return m.buildRustAction(actionDir, actionName, needsSync, needsAsync, report)
	case LanguageGo:
		return m.buildGoAction(actionDir, actionName, report)
	default:
		// Every language the detector can answer with is named above, so this is
		// reached only by one added to the detector and not to the build. It is
		// refused by name rather than handed to whichever branch happens to be
		// written last: an action compiled by the wrong toolchain, or described
		// by nothing at all, is a failure its author meets at deploy time.
		report("Failed")
		return ActionBuildResult{
			ActionName: actionName,
			Error:      fmt.Errorf("this action is written in %s, and this build has no path for that language", lang),
		}
	}
}

// buildGoAction describes a Go action, and says where its module comes from.
//
// DESCRIBING AN ACTION IS NOT COMPILING IT, AND ONLY ONE OF THE TWO BELONGS TO
// THE PLATFORM. The module does: a Go action is compiled when the app is
// deployed, and this CLI has no part in that. The description does not — the
// sentences a model reads, the input schema a caller is validated against and
// the action's statement about whether an agent may call it are all generated
// from the source by the same generator every other language goes through, and
// nothing else in a developer's loop generates them.
//
// Saying "the platform compiles this" and returning was therefore an answer to
// a question nobody had asked. A Go action reached this tool, was told where its
// module comes from, and went away with whatever action.json an earlier run had
// left beside it — or with none at all, which the platform's own build reads as
// an action that has never been built. A whole app written in Go could be built
// by this tool with no file describing any of it, and the run said nothing,
// because the step that would have said something was never reached.
//
// AN ACTION THAT CANNOT BE DESCRIBED FROM ITS OWN SOURCE DOES NOT BUILD, which
// is the sentence the other two languages already enforce. The failure is
// carried out whole rather than summarised: a refusal is the exact sentence its
// author has to read to fix their source, and every other failure this step
// raises already names what could not be done.
//
// IT ENDS IN A FAILURE EVEN WHEN EVERY STEP WORKED, because the module is half
// of what a build is asked for and this half is not produced here. The progress
// view repaints each row in place and is torn down when the run ends, so a
// status reported into it is gone before it can be read — the error is the only
// thing that survives to reach a developer, and a Go action reported as built
// leaves one waiting for an artifact that was never going to appear. What the
// build did do is named in the same sentence, so the failure is not read as
// nothing having happened.
func (*BuildManager) buildGoAction(actionDir, actionName string, report func(string)) ActionBuildResult {
	fail := func(err error) ActionBuildResult {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: err}
	}

	report("Extracting metadata...")
	if err := ExtractMetadataFunc(fsx.OSFileSystem{}, actionDir); err != nil {
		return fail(err)
	}

	return fail(fmt.Errorf("this action is written in Go: its action.json was written here from its source, and its module was not, because this CLI does not compile Go — the platform compiles a Go action when the app is deployed"))
}

func (m *BuildManager) buildTypeScriptAction(actionDir, actionName string, needsSync, needsAsync bool, report func(string)) ActionBuildResult {
	// Install dependencies
	report("Installing dependencies...")
	if err := EnsureDependenciesFunc(actionDir); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: fmt.Errorf("npm install failed: %w", err)}
	}

	// AN ACTION THAT CANNOT BE DESCRIBED FROM ITS OWN SOURCE DOES NOT BUILD.
	//
	// This step produces the description a model reads, the input schema a
	// caller is validated against, and the action's statement about whether an
	// agent may call it at all. It was allowed to fail and carry on, which made
	// it a gate that never closed: the failure was reported into a progress row
	// the next step overwrote milliseconds later, the build said Done and exited
	// zero, and the action.json from before the edit stayed on disk and stayed
	// authoritative. An author who mistyped an effect was told nothing and
	// shipped the old exposure statement.
	//
	// It is also the only reason the two build entry points could disagree about
	// the same source. The platform's mix task raises on this, so a source that
	// failed there built cleanly here — and a rule enforced by whichever tool
	// the author happened to run is not a rule.
	//
	// The failure is carried out whole rather than summarised: every error this
	// step produces already names what could not be done, and a refusal is the
	// exact sentence its author has to read to fix their source.
	report("Extracting metadata...")
	if err := ExtractMetadataFunc(fsx.OSFileSystem{}, actionDir); err != nil {
		report("Failed")
		return ActionBuildResult{ActionName: actionName, Error: err}
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
			syncBundleErr = BundleJSFunc(actionDir, "src/index.ts", syncBundle, true,
				map[string]string{"__ASYNC_BUILD__": "false"})
		}()
	}
	if needsAsync {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report("Bundling (Async)...")
			asyncBundleErr = BundleAsyncFunc(actionDir, "src/index.ts", asyncBundle)
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

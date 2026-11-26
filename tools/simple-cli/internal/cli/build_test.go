package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"simple-cli/internal/agents"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"sort"
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

func TestBuildEmptyAppTargetAgentChecks(t *testing.T) {
	originalJSON := jsonOutput
	originalCheck := checkBuildAgents
	originalHasDeclarations := hasBuildAgentDeclarations
	defer func() {
		jsonOutput = originalJSON
		checkBuildAgents = originalCheck
		hasBuildAgentDeclarations = originalHasDeclarations
	}()

	problem := agents.Problem{File: "apps/com.acme.shop/records/71_agents.scl", Line: 8, Rule: "replace the remaining guidance."}
	tests := []struct {
		name         string
		withRecords  bool
		hasAgents    bool
		problems     []agents.Problem
		json         bool
		wantErr      string
		wantStatus   string
		wantProblemN int
		wantText     string
	}{
		{name: "agents only is a valid target", withRecords: true, hasAgents: true, json: true, wantStatus: "complete"},
		{name: "agent problems fail text build", withRecords: true, hasAgents: true, problems: []agents.Problem{problem}, wantErr: "1 agent problems", wantText: "com.acme.shop: 1 things left:\napps/com.acme.shop/records/71_agents.scl:8: replace the remaining guidance.\nBuild failed because of these agent problems.\n"},
		{name: "agent problems fail JSON build", withRecords: true, hasAgents: true, problems: []agents.Problem{problem}, json: true, wantErr: "1 agent problems", wantStatus: "failed", wantProblemN: 1},
		{name: "empty app remains an invalid target", wantErr: "no actions or spaces found", wantStatus: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonOutput = tt.json
			checkCalls := 0
			checkBuildAgents = func(_ fsx.FileSystem, _, _ string) ([]agents.Problem, error) {
				checkCalls++
				return tt.problems, nil
			}
			hasBuildAgentDeclarations = func(_ fsx.FileSystem, _, _ string) (bool, error) {
				return tt.hasAgents, nil
			}
			files := map[string][]byte{}
			if tt.withRecords {
				files["apps/com.acme.shop/records"] = nil
			}
			fsys := &fsx.MockFileSystem{Files: files}
			var stdout, stderr string
			var runErr error
			if tt.json {
				stdout = captureStdout(t, func() {
					runErr = buildEmptyAppTarget(build.NewBuildManager(build.BuildOptions{Concurrency: 1}), fsys, "com.acme.shop", "com.acme.shop")
				})
			} else {
				stderr = captureStderr(t, func() {
					runErr = buildEmptyAppTarget(build.NewBuildManager(build.BuildOptions{Concurrency: 1}), fsys, "com.acme.shop", "com.acme.shop")
				})
			}
			if (runErr != nil) != (tt.wantErr != "") || (runErr != nil && !strings.Contains(runErr.Error(), tt.wantErr)) {
				t.Fatalf("buildEmptyAppTarget() error = %v, want containing %q", runErr, tt.wantErr)
			}
			if checkCalls != 1 {
				t.Errorf("agent checker calls = %d, want 1 for every app target", checkCalls)
			}
			if tt.wantText != "" && stderr != tt.wantText {
				t.Errorf("text = %q, want %q", stderr, tt.wantText)
			}
			if tt.json && tt.wantStatus != "" {
				var result struct {
					Status        string             `json:"status"`
					AgentProblems []agentProblemJSON `json:"agent_problems"`
				}
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatalf("decode build JSON: %v\n%s", err, stdout)
				}
				if result.Status != tt.wantStatus || len(result.AgentProblems) != tt.wantProblemN {
					t.Errorf("JSON status/problems = %q/%d, want %q/%d", result.Status, len(result.AgentProblems), tt.wantStatus, tt.wantProblemN)
				}
			}
		})
	}
}

type buildAppsFS struct {
	*fsx.MockFileSystem
	entries []os.DirEntry
	readErr error
}

func (f *buildAppsFS) ReadDir(name string) ([]os.DirEntry, error) {
	if filepath.Clean(name) == "apps" {
		return f.entries, f.readErr
	}
	return nil, nil
}

type buildTestDirEntry struct {
	name string
	dir  bool
}

func (d buildTestDirEntry) Name() string { return d.name }
func (d buildTestDirEntry) IsDir() bool  { return d.dir }
func (d buildTestDirEntry) Type() fs.FileMode {
	if d.dir {
		return fs.ModeDir
	}
	return 0
}
func (d buildTestDirEntry) Info() (fs.FileInfo, error) {
	return nil, errors.New("directory entry info is not used")
}

type agentCheckFS struct {
	*fsx.MockFileSystem
}

func (f *agentCheckFS) relative(name string) string {
	name = filepath.Clean(name)
	if !filepath.IsAbs(name) {
		return name
	}
	root, err := filepath.Abs(".")
	if err != nil {
		return name
	}
	relative, err := filepath.Rel(root, name)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return relative
	}
	return name
}

func (f *agentCheckFS) Stat(name string) (fs.FileInfo, error) {
	name = f.relative(name)
	if _, ok := f.Files[name]; ok {
		return f.MockFileSystem.Stat(name)
	}
	for path := range f.Files {
		if strings.HasPrefix(path, name+string(filepath.Separator)) {
			return nil, nil
		}
	}
	return nil, os.ErrNotExist
}

func (f *agentCheckFS) ReadFile(name string) ([]byte, error) {
	return f.MockFileSystem.ReadFile(f.relative(name))
}

func (f *agentCheckFS) ReadDir(name string) ([]os.DirEntry, error) {
	name = f.relative(name)
	children := map[string]bool{}
	for path := range f.Files {
		if !strings.HasPrefix(path, name+string(filepath.Separator)) {
			continue
		}
		rest := strings.TrimPrefix(path, name+string(filepath.Separator))
		part := strings.SplitN(rest, string(filepath.Separator), 2)
		children[part[0]] = len(part) > 1
	}
	if len(children) == 0 {
		return nil, os.ErrNotExist
	}
	names := make([]string, 0, len(children))
	for child := range children {
		names = append(names, child)
	}
	sort.Strings(names)
	entries := make([]os.DirEntry, 0, len(names))
	for _, child := range names {
		entries = append(entries, buildTestDirEntry{name: child, dir: children[child]})
	}
	return entries, nil
}

func newAgentCheckFS(actionMarker bool) *agentCheckFS {
	files := map[string][]byte{
		"apps/com.acme.crm/records/unrelated.scl": []byte("not valid SCL\n"),
	}
	if actionMarker {
		files["apps/com.acme.crm/actions/prepare/src/index.ts"] = []byte("// TODO(simple): finish this action\n")
	}
	return &agentCheckFS{MockFileSystem: &fsx.MockFileSystem{Files: files}}
}

func TestBuildAllowsUnrelatedUnparseableRecordsWithoutAgentsAndKeepsMarkerCheck(t *testing.T) {
	oldJSON, oldCheck := jsonOutput, checkBuildAgents
	defer func() { jsonOutput, checkBuildAgents = oldJSON, oldCheck }()
	jsonOutput = true
	checkBuildAgents = agents.Check
	tests := []struct {
		name    string
		marker  bool
		wantErr string
	}{
		{name: "unrelated malformed record builds"},
		{name: "action marker still blocks build", marker: true, wantErr: "1 agent problems"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := newAgentCheckFS(tt.marker)
			declared, declarationErr := appHasAgentDeclarations(fsys, "com.acme.crm")
			if declarationErr != nil || declared {
				t.Fatalf("appHasAgentDeclarations() = %v, %v; want no declarations", declared, declarationErr)
			}
			var runErr error
			stdout := captureStdout(t, func() {
				runErr = runBuildAll(build.NewBuildManager(build.BuildOptions{Concurrency: 1}), nil, nil,
					buildAgentTarget{appID: "com.acme.crm", fsys: fsys})
			})
			if (runErr != nil) != (tt.wantErr != "") || runErr != nil && !strings.Contains(runErr.Error(), tt.wantErr) {
				t.Fatalf("runBuildAll() error = %v, want containing %q", runErr, tt.wantErr)
			}
			if !strings.Contains(stdout, `"agent_problems"`) {
				t.Errorf("build JSON omitted the always-present agent_problems field: %s", stdout)
			}
		})
	}
}

func TestAppHasAgentDeclarationsHandlesMissingRecordsAndScannerResults(t *testing.T) {
	oldDeclarations := hasBuildAgentDeclarations
	defer func() { hasBuildAgentDeclarations = oldDeclarations }()
	tests := []struct {
		name    string
		fsys    fsx.FileSystem
		check   func()
		want    bool
		wantErr string
	}{
		{name: "missing records directory", fsys: &fsx.MockFileSystem{}},
		{name: "unrelated malformed records", fsys: newAgentCheckFS(false)},
		{name: "kind of work declaration", fsys: func() fsx.FileSystem {
			f := newAgentCheckFS(false)
			f.Files["apps/com.acme.crm/records/work.scl"] = []byte("set task_type, task_type_returns {\n")
			return f
		}(), want: true},
		{name: "record stat error", fsys: &fsx.MockFileSystem{StatErr: errors.New("stat denied")}, wantErr: "failed to find records"},
		{name: "declaration scan error", fsys: &fsx.MockFileSystem{Files: map[string][]byte{"apps/com.acme.crm/records": nil}},
			check: func() {
				hasBuildAgentDeclarations = func(fsx.FileSystem, string, string) (bool, error) { return false, errors.New("scan denied") }
			}, wantErr: "failed to read agent declarations"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasBuildAgentDeclarations = oldDeclarations
			if tt.check != nil {
				tt.check()
			}
			got, err := appHasAgentDeclarations(tt.fsys, "com.acme.crm")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("appHasAgentDeclarations() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("appHasAgentDeclarations() = %v, %v; want %v, nil", got, err, tt.want)
			}
		})
	}
}

func TestBuildAllAppsAgentChecks(t *testing.T) {
	oldJSON, oldCheck, oldDeclarations := jsonOutput, checkBuildAgents, hasBuildAgentDeclarations
	oldFindActions, oldFindSpaces, oldRunAll := findBuildActions, findBuildSpaces, runAllBuildTargets
	defer func() {
		jsonOutput, checkBuildAgents, hasBuildAgentDeclarations = oldJSON, oldCheck, oldDeclarations
		findBuildActions, findBuildSpaces, runAllBuildTargets = oldFindActions, oldFindSpaces, oldRunAll
	}()
	problem := agents.Problem{File: "apps/demo/actions/send/src/index.ts", Line: 2, Rule: "replace this line"}
	tests := []struct {
		name          string
		apps          []string
		records       []string
		problems      []agents.Problem
		hasAgents     bool
		actions       map[string][]string
		spaces        map[string][]string
		readErr       error
		wantErr       string
		wantRun       bool
		wantChecks    int
		wantTargets   int
		wantProblems  int
		wantActions   int
		wantSpaceDirs int
	}{
		{name: "apps path missing", wantErr: "apps directory not found"},
		{name: "apps directory read fails", apps: []string{"demo"}, readErr: errors.New("read denied"), wantErr: "failed to read apps directory"},
		{name: "empty apps return JSON success"},
		{name: "marker check runs without records", apps: []string{"demo"}, problems: []agents.Problem{problem}, wantRun: true, wantChecks: 1, wantTargets: 1, wantProblems: 1},
		{name: "agent records make empty app buildable", apps: []string{"demo"}, records: []string{"demo"}, hasAgents: true, wantRun: true, wantChecks: 1, wantTargets: 1},
		{name: "actions and spaces share the same targets", apps: []string{"demo", "other"}, actions: map[string][]string{"apps/demo": {"apps/demo/actions/send"}}, spaces: map[string][]string{"apps/other": {"apps/other/spaces/admin"}}, wantRun: true, wantChecks: 0, wantTargets: 2, wantActions: 1, wantSpaceDirs: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonOutput = true
			files := map[string][]byte{}
			if tt.name != "apps path missing" {
				files["apps"] = nil
			}
			for _, appID := range tt.records {
				files[filepath.Join("apps", appID, "records")] = nil
			}
			entries := make([]os.DirEntry, 0, len(tt.apps))
			for _, appID := range tt.apps {
				entries = append(entries, buildTestDirEntry{name: appID, dir: true})
			}
			fsys := &buildAppsFS{MockFileSystem: &fsx.MockFileSystem{Files: files}, entries: entries, readErr: tt.readErr}
			checkCalls := 0
			checkBuildAgents = func(_ fsx.FileSystem, _, _ string) ([]agents.Problem, error) {
				checkCalls++
				return tt.problems, nil
			}
			hasBuildAgentDeclarations = func(_ fsx.FileSystem, _, _ string) (bool, error) {
				return tt.hasAgents, nil
			}
			findBuildActions = func(appDir string) ([]string, error) { return tt.actions[filepath.Clean(appDir)], nil }
			findBuildSpaces = func(appDir string) ([]string, error) { return tt.spaces[filepath.Clean(appDir)], nil }
			runCalls := 0
			runAllBuildTargets = func(_ *build.BuildManager, actions, spaces []string, targets ...buildAgentTarget) error {
				runCalls++
				if len(targets) != tt.wantTargets {
					t.Errorf("agent targets = %d, want %d", len(targets), tt.wantTargets)
				}
				if tt.wantProblems > 0 && (!targets[0].checked || len(targets[0].problems) != tt.wantProblems) {
					t.Errorf("checked target = %#v, want %d cached problems", targets[0], tt.wantProblems)
				}
				if len(actions) != tt.wantActions || len(spaces) != tt.wantSpaceDirs {
					t.Errorf("build paths = %d actions and %d spaces, want %d and %d", len(actions), len(spaces), tt.wantActions, tt.wantSpaceDirs)
				}
				return nil
			}
			manager := build.NewBuildManager(build.BuildOptions{Concurrency: 1})
			var runErr error
			_ = captureStdout(t, func() { runErr = buildAllApps(manager, fsys) })
			if (runErr != nil) != (tt.wantErr != "") || (runErr != nil && !strings.Contains(runErr.Error(), tt.wantErr)) {
				t.Fatalf("buildAllApps() error = %v, want containing %q", runErr, tt.wantErr)
			}
			if checkCalls != tt.wantChecks || runCalls != btoi(tt.wantRun) {
				t.Errorf("checks/build calls = %d/%d, want %d/%d", checkCalls, runCalls, tt.wantChecks, btoi(tt.wantRun))
			}
		})
	}
}

func TestBuildSingleActionTargetSkipsAgentCheck(t *testing.T) {
	originalJSON := jsonOutput
	originalCheck := checkBuildAgents
	originalDetect := build.DetectActionLanguageFunc
	defer func() {
		jsonOutput = originalJSON
		checkBuildAgents = originalCheck
		build.DetectActionLanguageFunc = originalDetect
	}()
	jsonOutput = true
	checkCalls := 0
	checkBuildAgents = func(_ fsx.FileSystem, _, _ string) ([]agents.Problem, error) {
		checkCalls++
		return nil, nil
	}
	build.DetectActionLanguageFunc = func(string) (build.ActionLanguage, error) {
		return "", errors.New("stop before reading action files")
	}
	var err error
	_ = captureStdout(t, func() {
		err = buildSingleActionTarget(build.NewBuildManager(build.BuildOptions{Concurrency: 1}), "virtual/action")
	})
	if err == nil || !strings.Contains(err.Error(), "1 action(s) failed to build") {
		t.Errorf("buildSingleActionTarget() error = %v, want mocked build failure", err)
	}
	if checkCalls != 0 {
		t.Errorf("agent checker calls = %d, want 0 for a single action", checkCalls)
	}
}

func TestCheckAppAgentsAtUsesSuppliedDirectory(t *testing.T) {
	originalCheck, originalCheckAt := checkBuildAgents, checkBuildAgentsAt
	defer func() { checkBuildAgents, checkBuildAgentsAt = originalCheck, originalCheckAt }()
	tests := []struct {
		name    string
		appID   string
		appDir  string
		checkAt bool
	}{
		{name: "canonical app directory", appID: "com.acme.shop", appDir: filepath.Join("apps", "com.acme.shop")},
		{name: "custom app directory", appID: "com.acme.shop", appDir: filepath.Join("fixtures", "shop"), checkAt: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calledCanonical, calledAt := false, false
			checkBuildAgents = func(_ fsx.FileSystem, repoRoot, appID string) ([]agents.Problem, error) {
				calledCanonical = true
				if repoRoot != "." || appID != tt.appID {
					t.Errorf("Check() arguments = %q, %q; want %q, %q", repoRoot, appID, ".", tt.appID)
				}
				return nil, nil
			}
			checkBuildAgentsAt = func(_ fsx.FileSystem, repoRoot, appID, appDir string) ([]agents.Problem, error) {
				calledAt = true
				if repoRoot != "." || appID != tt.appID || appDir != tt.appDir {
					t.Errorf("CheckAt arguments = %q, %q, %q; want %q, %q, %q", repoRoot, appID, appDir, ".", tt.appID, tt.appDir)
				}
				return nil, nil
			}
			if _, err := checkAppAgentsAt(nil, tt.appID, tt.appDir); err != nil {
				t.Fatalf("checkAppAgentsAt() error = %v", err)
			}
			if calledCanonical == tt.checkAt || calledAt != tt.checkAt {
				t.Fatalf("canonical/at checker calls = %v/%v, want %v/%v", calledCanonical, calledAt, !tt.checkAt, tt.checkAt)
			}
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open a pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	captured := make(chan string)
	go func() {
		data, _ := io.ReadAll(reader)
		captured <- string(data)
	}()
	run()
	_ = writer.Close()
	os.Stderr = original
	return <-captured
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

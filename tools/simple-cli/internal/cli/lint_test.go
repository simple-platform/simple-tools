package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

type lintInvocation struct {
	name string
	args []string
}

func writeLintFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func makeLintWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeLintFile(t, root, "simple.scl", "monorepo acme\n")
	writeLintFile(t, root, "package.json", `{"devDependencies":{"@antfu/eslint-config":"10.0.0","eslint":"10.15.0","eslint-plugin-format":"2.0.1","typescript":"6.0.3"}}`)
	writeLintFile(t, root, "node_modules/.bin/eslint", "")
	for _, pkg := range lintPackages {
		writeLintFile(t, root, filepath.Join("node_modules", pkg.name, "package.json"), "{}")
	}
	writeLintFile(t, root, "apps/com.acme.demo/actions/typescript/action.scl", "action {}\n")
	writeLintFile(t, root, "apps/com.acme.demo/actions/typescript/src/index.ts", "export function run() {}\n")
	writeLintFile(t, root, "apps/com.acme.demo/actions/rust-action/src/main.rs", "fn main() {}\n")
	writeLintFile(t, root, "apps/com.acme.demo/actions/rust-action/Cargo.toml", "[package]\nname = \"rust-action\"\nversion = \"0.1.0\"\n")
	writeLintFile(t, root, "apps/com.acme.demo/actions/go-action/main.go", "package main\nfunc main() {}\n")
	writeLintFile(t, root, "apps/com.acme.demo/spaces/admin/package.json", "{}")
	writeLintFile(t, root, "apps/com.acme.demo/spaces/admin/index.html", "<html></html>\n")
	return root
}

func installLintCommandMock(t *testing.T, run func(string, []string) (string, int)) *[]lintInvocation {
	t.Helper()
	var mu sync.Mutex
	var calls []lintInvocation
	original := lintCommandFunc
	lintCommandFunc = func(name string, args ...string) *exec.Cmd {
		mu.Lock()
		calls = append(calls, lintInvocation{name: name, args: append([]string(nil), args...)})
		mu.Unlock()
		output, code := run(name, args)
		return lintHelperProcess(output, code)
	}
	t.Cleanup(func() { lintCommandFunc = original })
	return &calls
}

func lintHelperProcess(output string, exitCode int) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestLintCommandHelperProcess$")
	command.Env = append(os.Environ(),
		"SIMPLE_LINT_HELPER=1",
		"SIMPLE_LINT_OUTPUT="+output,
		"SIMPLE_LINT_EXIT="+strconv.Itoa(exitCode),
	)
	return command
}

func TestLintCommandHelperProcess(t *testing.T) {
	if os.Getenv("SIMPLE_LINT_HELPER") != "1" {
		return
	}
	_, _ = fmt.Fprint(os.Stdout, os.Getenv("SIMPLE_LINT_OUTPUT"))
	code, _ := strconv.Atoi(os.Getenv("SIMPLE_LINT_EXIT"))
	os.Exit(code)
}

func invokeLintCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	lintFix = false
	_ = lintCmd.Flags().Set("fix", "false")
	return invokeCmd(append([]string{"lint"}, args...)...)
}

func TestLintRunsWorkspaceChecksInStableOrder(t *testing.T) {
	root := makeLintWorkspace(t)
	t.Chdir(root)
	var execMu sync.Mutex
	var envCalls []string
	oldEnv := lintExecutionEnvironmentFunc
	lintExecutionEnvironmentFunc = func(actionDir string) (string, error) {
		execMu.Lock()
		defer execMu.Unlock()
		envCalls = append(envCalls, filepath.Base(actionDir))
		return "both", nil
	}
	t.Cleanup(func() { lintExecutionEnvironmentFunc = oldEnv })
	calls := installLintCommandMock(t, func(string, []string) (string, int) { return "", 0 })

	out, _, err := invokeLintCmd(t)
	if err != nil {
		t.Fatalf("simple lint error = %v\n%s", err, out)
	}
	if len(*calls) != 7 {
		t.Fatalf("commands run = %d, want 7: %#v", len(*calls), *calls)
	}
	commandText := make([]string, 0, len(*calls))
	for _, call := range *calls {
		commandText = append(commandText, strings.Join(append([]string{call.name}, call.args...), " "))
	}
	for _, want := range []string{
		"npx --no -- eslint --no-error-on-unmatched-pattern .",
		"cargo fmt --check",
		"cargo clippy --all-targets -- -D warnings",
		"cargo clippy --target wasm32-wasip1 -- -D warnings",
		"cargo clippy --target wasm32-wasip1 --features async -- -D warnings",
		"gofmt -l .",
		"go vet ./...",
	} {
		if !lintContainsString(commandText, want) {
			t.Errorf("command list %#v does not include %q", commandText, want)
		}
	}
	if strings.Index(out, "ESLint (.) passed") > strings.Index(out, "gofmt -l . (apps/com.acme.demo/actions/go-action)") || strings.Index(out, "gofmt -l .") > strings.Index(out, "Rust fmt --check") {
		t.Errorf("checks were not reported in stable order:\n%s", out)
	}
	if len(envCalls) != 1 || envCalls[0] != "rust-action" {
		t.Errorf("Rust environment calls = %v, want only rust-action", envCalls)
	}
}

func TestLintTargetAndFixFlags(t *testing.T) {
	tests := []struct {
		name                 string
		args                 []string
		startDir             string
		executionEnvironment string
		wantCommands         []string
		wantAbsent           []string
		wantESLintArg        string
		wantError            string
	}{
		{
			name:          "target action",
			args:          []string{"com.acme.demo/rust-action"},
			wantCommands:  []string{"npx --no -- eslint --no-error-on-unmatched-pattern apps/com.acme.demo/actions/rust-action", "cargo clippy --all-targets -- -D warnings", "cargo clippy --target wasm32-wasip1 -- -D warnings", "cargo clippy --target wasm32-wasip1 --features async -- -D warnings"},
			wantAbsent:    []string{"go vet ./...", "eslint ."},
			wantESLintArg: "apps/com.acme.demo/actions/rust-action",
		},
		{
			name:          "format fixes",
			args:          []string{"--fix"},
			wantCommands:  []string{"npx --no -- eslint --no-error-on-unmatched-pattern --fix .", "cargo fmt", "gofmt -w .", "cargo clippy --all-targets -- -D warnings", "cargo clippy --target wasm32-wasip1 -- -D warnings", "cargo clippy --target wasm32-wasip1 --features async -- -D warnings", "go vet ./..."},
			wantAbsent:    []string{"cargo fmt --check", "gofmt -l ."},
			wantESLintArg: ".",
		},
		{
			name:          "app target",
			args:          []string{"com.acme.demo"},
			wantCommands:  []string{"npx --no -- eslint --no-error-on-unmatched-pattern apps/com.acme.demo", "cargo clippy --all-targets -- -D warnings", "gofmt -l ."},
			wantESLintArg: "apps/com.acme.demo",
		},
		{
			name:          "space target",
			args:          []string{"apps/com.acme.demo/spaces/admin"},
			wantCommands:  []string{"npx --no -- eslint --no-error-on-unmatched-pattern apps/com.acme.demo/spaces/admin"},
			wantAbsent:    []string{"cargo fmt --check", "cargo clippy --all-targets -- -D warnings", "go vet ./...", "gofmt -l ."},
			wantESLintArg: "apps/com.acme.demo/spaces/admin",
		},
		{
			name:          "unknown target",
			args:          []string{"missing-target"},
			wantError:     "build target 'missing-target' not found",
			wantESLintArg: "",
		},
		{
			name:          "relative target from subfolder",
			args:          []string{"actions/typescript"},
			startDir:      "apps/com.acme.demo",
			wantCommands:  []string{"npx --no -- eslint --no-error-on-unmatched-pattern apps/com.acme.demo/actions/typescript"},
			wantAbsent:    []string{"cargo fmt --check", "cargo clippy --all-targets -- -D warnings", "go vet ./...", "gofmt -l ."},
			wantESLintArg: "apps/com.acme.demo/actions/typescript",
		},
		{
			name:                 "browser-only Rust action",
			args:                 []string{"com.acme.demo/rust-action"},
			executionEnvironment: "client",
			wantCommands:         []string{"cargo fmt --check", "cargo clippy --all-targets -- -D warnings", "cargo clippy --target wasm32-wasip1 --features async -- -D warnings"},
			wantAbsent:           []string{"cargo clippy --target wasm32-wasip1 -- -D warnings", "go vet ./..."},
			wantESLintArg:        "apps/com.acme.demo/actions/rust-action",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := makeLintWorkspace(t)
			startDir := root
			if tt.startDir != "" {
				startDir = filepath.Join(root, tt.startDir)
			}
			t.Chdir(startDir)
			oldEnv := lintExecutionEnvironmentFunc
			lintExecutionEnvironmentFunc = func(string) (string, error) {
				if tt.executionEnvironment != "" {
					return tt.executionEnvironment, nil
				}
				return "both", nil
			}
			t.Cleanup(func() { lintExecutionEnvironmentFunc = oldEnv })
			calls := installLintCommandMock(t, func(string, []string) (string, int) { return "", 0 })
			out, _, err := invokeLintCmd(t, tt.args...)
			if tt.wantError != "" {
				if err == nil || err.Error() != tt.wantError {
					t.Fatalf("simple lint error = %v, want %q", err, tt.wantError)
				}
				if len(*calls) != 0 {
					t.Errorf("commands run for unknown target: %#v", *calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("simple lint error = %v\n%s", err, out)
			}
			commandText := make([]string, 0, len(*calls))
			for _, call := range *calls {
				commandText = append(commandText, strings.Join(append([]string{call.name}, call.args...), " "))
			}
			for _, want := range tt.wantCommands {
				if !lintContainsString(commandText, want) {
					t.Errorf("commands %#v do not include %q", commandText, want)
				}
			}
			for _, absent := range tt.wantAbsent {
				if lintContainsString(commandText, absent) {
					t.Errorf("commands %#v unexpectedly include %q", commandText, absent)
				}
			}
			if tt.wantESLintArg != "" && !strings.Contains(out, "ESLint ("+tt.wantESLintArg+") passed") {
				t.Errorf("ESLint did not report its target %q:\n%s", tt.wantESLintArg, out)
			}
		})
	}
}

func TestLintFailuresIncludeToolOutputAndContinue(t *testing.T) {
	root := makeLintWorkspace(t)
	t.Chdir(root)
	oldEnv := lintExecutionEnvironmentFunc
	lintExecutionEnvironmentFunc = func(string) (string, error) { return "both", nil }
	t.Cleanup(func() { lintExecutionEnvironmentFunc = oldEnv })
	installLintCommandMock(t, func(name string, args []string) (string, int) {
		switch {
		case name == "npx":
			return "css rule failed\n", 2
		case name == "cargo" && len(args) > 1 && args[0] == "clippy" && args[1] == "--target" && !lintContainsString(args, "--features"):
			return "unused variable: value\n", 1
		case name == "gofmt":
			return "main.go\n", 0
		default:
			return "", 0
		}
	})

	out, _, err := invokeLintCmd(t)
	if err == nil || !strings.Contains(err.Error(), "3 lint checks failed") {
		t.Fatalf("simple lint error = %v, want three failed checks\n%s", err, out)
	}
	for _, want := range []string{"css rule failed", "unused variable: value", "main.go", "3 checks failed."} {
		if !strings.Contains(out, want) {
			t.Errorf("lint output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "ESLint (.) failed") > strings.Index(out, "gofmt -l . (apps/com.acme.demo/actions/go-action)") || strings.Index(out, "gofmt -l .") > strings.Index(out, "Rust clippy --target wasm32-wasip1") {
		t.Errorf("failed check output is not in stable order:\n%s", out)
	}
}

func TestLintChecksInOneFolderRunSequentially(t *testing.T) {
	tests := []struct {
		name   string
		checks []lintCheck
		want   []string
	}{
		{
			name: "Go formatter precedes vet",
			checks: []lintCheck{
				{name: "gofmt -l .", where: "apps/first", dir: "/workspace/apps/first", command: []string{"gofmt", "-l", "."}},
				{name: "go vet ./...", where: "apps/first", dir: "/workspace/apps/first", command: []string{"go", "vet", "./..."}},
			},
			want: []string{"gofmt -l .", "go vet ./..."},
		},
		{
			name: "Rust formatter precedes ESLint and clippy for an action target",
			checks: []lintCheck{
				{name: "ESLint", where: "apps/rust-action", dir: "/workspace", command: []string{"npx", "--no", "--", "eslint", "apps/rust-action"}},
				{name: "Rust fmt --check", where: "apps/rust-action", dir: "/workspace/apps/rust-action", command: []string{"cargo", "fmt", "--check"}},
				{name: "Rust clippy --all-targets", where: "apps/rust-action", dir: "/workspace/apps/rust-action", command: []string{"cargo", "clippy", "--all-targets"}},
				{name: "Rust clippy wasm", where: "apps/rust-action", dir: "/workspace/apps/rust-action", command: []string{"cargo", "clippy", "--target", "wasm32-wasip1"}},
			},
			want: []string{"cargo fmt --check", "npx --no -- eslint apps/rust-action", "cargo clippy --all-targets", "cargo clippy --target wasm32-wasip1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldRunner := lintCommandFunc
			lintCommandFunc = func(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }
			t.Cleanup(func() { lintCommandFunc = oldRunner })
			oldCombinedOutput := lintCombinedOutputFunc
			var mu sync.Mutex
			var completed []string
			lintCombinedOutputFunc = func(cmd *exec.Cmd) ([]byte, error) {
				mu.Lock()
				defer mu.Unlock()
				completed = append(completed, strings.Join(cmd.Args, " "))
				return nil, nil
			}
			t.Cleanup(func() { lintCombinedOutputFunc = oldCombinedOutput })

			runLintChecks(tt.checks)
			if strings.Join(completed, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("checks completed as %v, want %v", completed, tt.want)
			}
		})
	}
}

func TestLintEmptyTargetsMatchBuildError(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{target: "apps", want: "no actions or spaces found in apps"},
		{target: "apps/com.acme.demo/actions", want: "no actions or spaces found in apps/com.acme.demo/actions"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			root := makeLintWorkspace(t)
			t.Chdir(root)
			_, _, err := invokeLintCmd(t, tt.target)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("simple lint error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLintMissingNpxAndPnpmGiveDevEnvironmentHint(t *testing.T) {
	t.Run("npx", func(t *testing.T) {
		oldRunner := lintCommandFunc
		lintCommandFunc = func(name string, args ...string) *exec.Cmd { return exec.Command("missing-npx-for-lint-test", args...) }
		t.Cleanup(func() { lintCommandFunc = oldRunner })
		checks := []lintCheck{{name: "ESLint", dir: t.TempDir(), command: []string{"npx", "--no", "--", "eslint"}}}
		runLintChecks(checks)
		if !strings.Contains(checks[0].output, "npx is not available; run this command inside the workspace's dev environment") || checks[0].err == nil {
			t.Errorf("missing npx result = %+v", checks[0])
		}
	})

	t.Run("pnpm", func(t *testing.T) {
		root := makeLintWorkspace(t)
		if err := os.RemoveAll(filepath.Join(root, "node_modules", "eslint")); err != nil {
			t.Fatal(err)
		}
		oldInstall := ensureLintWorkspaceInstall
		ensureLintWorkspaceInstall = func() error { return &exec.Error{Name: "pnpm", Err: exec.ErrNotFound} }
		t.Cleanup(func() { ensureLintWorkspaceInstall = oldInstall })
		err := prepareLintESLint(root)
		if err == nil || !strings.Contains(err.Error(), "pnpm is not available; run this command inside the workspace's dev environment") {
			t.Errorf("missing pnpm error = %v", err)
		}
	})
}

func TestLintInstallsRootPackagesWhenESLintIsMissing(t *testing.T) {
	root := makeLintWorkspace(t)
	if err := os.RemoveAll(filepath.Join(root, "node_modules", "eslint-plugin-format")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldInstall := ensureLintWorkspaceInstall
	installs := 0
	ensureLintWorkspaceInstall = func() error {
		installs++
		writeLintFile(t, root, "node_modules/eslint-plugin-format/package.json", "{}")
		return nil
	}
	t.Cleanup(func() { ensureLintWorkspaceInstall = oldInstall })
	installLintCommandMock(t, func(string, []string) (string, int) { return "", 0 })
	oldEnv := lintExecutionEnvironmentFunc
	lintExecutionEnvironmentFunc = func(string) (string, error) { return "server", nil }
	t.Cleanup(func() { lintExecutionEnvironmentFunc = oldEnv })

	out, _, err := invokeLintCmd(t)
	if err != nil {
		t.Fatalf("simple lint error = %v\n%s", err, out)
	}
	if installs != 1 {
		t.Errorf("root installs = %d, want one", installs)
	}
}

func TestLintMissingToolchainsStillReportEveryCheck(t *testing.T) {
	root := makeLintWorkspace(t)
	t.Chdir(root)
	oldRunner := lintCommandFunc
	lintCommandFunc = func(name string, _ ...string) *exec.Cmd {
		if name == "npx" {
			return lintHelperProcess("", 0)
		}
		return exec.Command("simple-lint-tool-that-is-not-installed")
	}
	t.Cleanup(func() { lintCommandFunc = oldRunner })
	oldEnv := lintExecutionEnvironmentFunc
	lintExecutionEnvironmentFunc = func(string) (string, error) { return "both", nil }
	t.Cleanup(func() { lintExecutionEnvironmentFunc = oldEnv })

	out, _, err := invokeLintCmd(t)
	if err == nil || !strings.Contains(err.Error(), "6 lint checks failed") {
		t.Fatalf("simple lint error = %v, want six unavailable tool checks\n%s", err, out)
	}
	for _, want := range []string{"cargo is not available", "go is not available", "gofmt is not available", "6 checks failed."} {
		if !strings.Contains(out, want) {
			t.Errorf("lint output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Count(out, ") failed\n") != 6 {
		t.Errorf("lint did not report every unavailable check:\n%s", out)
	}
}

func TestLintJSONResultsAndMissingESLintManifest(t *testing.T) {
	tests := []struct {
		name          string
		removePackage bool
		failCheck     bool
		wantErr       bool
		wantFailed    int
	}{
		{name: "JSON result"},
		{name: "failed check", failCheck: true, wantErr: true, wantFailed: 1},
		{name: "missing package declaration", removePackage: true, wantErr: true, wantFailed: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := makeLintWorkspace(t)
			if tt.removePackage {
				writeLintFile(t, root, "package.json", `{"devDependencies":{}}`)
			}
			t.Chdir(root)
			installLintCommandMock(t, func(name string, _ []string) (string, int) {
				if tt.failCheck && name == "npx" {
					return "css rule failed\n", 1
				}
				return "", 0
			})
			oldEnv := lintExecutionEnvironmentFunc
			lintExecutionEnvironmentFunc = func(string) (string, error) { return "server", nil }
			t.Cleanup(func() { lintExecutionEnvironmentFunc = oldEnv })
			out, _, err := invokeLintCmd(t, "--json")
			if (err != nil) != tt.wantErr {
				t.Fatalf("simple lint error = %v, wantErr %v\n%s", err, tt.wantErr, out)
			}
			var result lintResult
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatalf("lint JSON is invalid: %v\n%s", err, out)
			}
			if tt.removePackage {
				if result.Failed != tt.wantFailed || !strings.Contains(result.Checks[0].Output, "devDependencies") {
					t.Errorf("missing manifest result = %+v", result)
				}
				return
			}
			if tt.failCheck {
				if result.Status != "failed" || result.Failed != tt.wantFailed || !strings.Contains(result.Checks[0].Output, "css rule failed") {
					t.Errorf("failed check JSON result = %+v", result)
				}
				return
			}
			if result.Status != "complete" || result.Failed != 0 {
				t.Errorf("success result = %+v", result)
			}
		})
	}
}

func TestReportLintResultsWritesJSONToCommandOutput(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	if err := reportLintResults(cmd, []lintCheck{{name: "ESLint", where: "."}}, true); err != nil {
		t.Fatalf("reportLintResults error = %v", err)
	}
	var result lintResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("command output is invalid JSON: %v\n%s", err, output.String())
	}
	if result.Status != "complete" || result.Total != 1 {
		t.Errorf("JSON result = %+v", result)
	}
}

func lintContainsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

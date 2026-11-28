package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

var (
	lintFix                      bool
	lintCommandFunc              = exec.Command
	lintCombinedOutputFunc       = func(cmd *exec.Cmd) ([]byte, error) { return cmd.CombinedOutput() }
	ensureLintWorkspaceInstall   = build.EnsureWorkspaceInstall
	lintExecutionEnvironmentFunc = lintExecutionEnvironment
)

type lintCheck struct {
	name    string
	where   string
	dir     string
	command []string
	output  string
	err     error
}

type lintCheckResult struct {
	Check  string `json:"check"`
	Where  string `json:"where"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
}

type lintResult struct {
	Status  string            `json:"status"`
	Total   int               `json:"total"`
	Success int               `json:"success"`
	Failed  int               `json:"failed"`
	Checks  []lintCheckResult `json:"checks"`
}

type lintFailure struct {
	failed int
}

func (e lintFailure) Error() string { return fmt.Sprintf("%d lint checks failed", e.failed) }

// AlreadyReported keeps the root command from printing a second error after
// lint has emitted its complete ordered report.
func (lintFailure) AlreadyReported() bool { return true }

var lintCmd = &cobra.Command{
	Use:   "lint [target]",
	Short: "Lint a workspace or target",
	Long: `Run the workspace ESLint checks and the language checks for its actions.

Examples:
  simple lint
  simple lint com.example.todo
  simple lint com.example.todo/send-email --fix`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLint,
}

func init() {
	RootCmd.AddCommand(lintCmd)
	lintCmd.Flags().BoolVar(&lintFix, "fix", false, "Apply formatting fixes where available")
}

func runLint(cmd *cobra.Command, args []string) error {
	root, err := build.FindWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("simple lint must run inside a workspace: %w", err)
	}
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	fsys := fsx.OSFileSystem{}
	target := ""
	if len(args) > 0 {
		target = args[0]
		// Like simple build, resolve an existing path from the caller's directory
		// before switching to the workspace root.
		if !filepath.IsAbs(target) && scaffold.PathExists(fsys, target) {
			target, err = filepath.Abs(target)
			if err != nil {
				return fmt.Errorf("failed to resolve lint target %s: %w", args[0], err)
			}
		}
	}
	if err := os.Chdir(root); err != nil {
		return fmt.Errorf("failed to enter workspace root %s: %w", root, err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("workspace root %s has no apps directory", root)
	}

	lintFolder := "."
	var actionDirs []string
	if len(args) == 0 {
		actionDirs, err = lintActionsInApps(fsys, "apps")
		if err != nil {
			return err
		}
	} else {
		resolved, err := resolveBuildTarget(fsys, target)
		if err != nil {
			return err
		}
		lintFolder, err = filepath.Rel(root, resolved.path)
		if err != nil {
			return fmt.Errorf("failed to resolve lint folder: %w", err)
		}
		lintFolder = filepath.ToSlash(lintFolder)
		if resolved.action {
			actionDirs = []string{resolved.path}
		} else if !resolved.space {
			actionDirs = resolved.actions
			if len(resolved.actions) == 0 && len(resolved.spaces) == 0 {
				return fmt.Errorf("no actions or spaces found in %s", args[0])
			}
		}
	}

	checks := []lintCheck{{name: "ESLint", where: lintFolder, dir: root}}
	if err := prepareLintESLint(root); err != nil {
		checks[0].err = err
		checks[0].output = err.Error()
	} else {
		args := []string{"npx", "--no", "--", "eslint", "--no-error-on-unmatched-pattern"}
		if lintFix {
			args = append(args, "--fix")
		}
		checks[0].command = append(args, lintFolder)
	}

	sort.Strings(actionDirs)
	for _, actionDir := range actionDirs {
		language, err := build.DetectActionLanguage(actionDir)
		where, pathErr := filepath.Rel(root, actionDir)
		if pathErr != nil {
			return fmt.Errorf("failed to resolve action folder %s: %w", actionDir, pathErr)
		}
		where = filepath.ToSlash(where)
		if err != nil {
			checks = append(checks, lintCheck{name: "Action language", where: where, output: err.Error(), err: err})
			continue
		}
		switch language {
		case build.LanguageRust:
			executionEnvironment, err := lintExecutionEnvironmentFunc(actionDir)
			if err != nil {
				checks = append(checks, lintCheck{name: "Rust execution environment", where: where, output: err.Error(), err: err})
				continue
			}
			commands := build.RustLintCommands(executionEnvironment, lintFix)
			if len(commands) == 0 {
				err := fmt.Errorf("execution_environment is %q, which names no artifact to lint: it has to be exactly server, client, or both", executionEnvironment)
				checks = append(checks, lintCheck{name: "Rust execution environment", where: where, output: err.Error(), err: err})
				continue
			}
			for _, command := range commands {
				checks = append(checks, lintCheck{
					name:    "Rust " + strings.Join(command, " "),
					where:   where,
					dir:     actionDir,
					command: append([]string{"cargo"}, command...),
				})
			}
		case build.LanguageGo:
			gofmt := []string{"-l", "."}
			if lintFix {
				gofmt = []string{"-w", "."}
			}
			checks = append(checks,
				lintCheck{name: "gofmt " + strings.Join(gofmt, " "), where: where, dir: actionDir, command: append([]string{"gofmt"}, gofmt...)},
				lintCheck{name: "go vet ./...", where: where, dir: actionDir, command: []string{"go", "vet", "./..."}},
			)
		}
	}

	runLintChecks(checks)
	return reportLintResults(cmd, checks, jsonOutput)
}

func lintActionsInApps(fsys fsx.FileSystem, appsDir string) ([]string, error) {
	entries, err := fsys.ReadDir(appsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}
	var actionDirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirs, err := build.FindActions(filepath.Join(appsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to find actions in %s: %w", entry.Name(), err)
		}
		for _, dir := range dirs {
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve action folder %s: %w", dir, err)
			}
			actionDirs = append(actionDirs, absDir)
		}
	}
	return actionDirs, nil
}

var lintPackages = []struct {
	name    string
	version string
}{
	{"@antfu/eslint-config", "10.0.0"},
	{"eslint", "10.15.0"},
	{"eslint-plugin-format", "2.0.1"},
	{"typescript", "6.0.3"},
}

func prepareLintESLint(root string) error {
	manifestPath := filepath.Join(root, "package.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read workspace package.json: %w", err)
	}
	var manifest struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("failed to parse workspace package.json: %w", err)
	}
	var missing []string
	for _, pkg := range lintPackages {
		if _, ok := manifest.Dependencies[pkg.name]; ok {
			continue
		}
		if _, ok := manifest.DevDependencies[pkg.name]; !ok {
			missing = append(missing, fmt.Sprintf("  %q: %q", pkg.name, pkg.version))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("ESLint is not listed in the workspace root package.json; add these lines to devDependencies:\n%s", strings.Join(missing, ",\n"))
	}

	if !lintESLintInstalled(root) {
		if err := ensureLintWorkspaceInstall(); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				hint := strings.TrimSuffix(lintToolHint("pnpm"), "\n")
				return fmt.Errorf("%s: %w", hint, err)
			}
			return fmt.Errorf("failed to install workspace ESLint packages: %w", err)
		}
	}
	if !lintESLintInstalled(root) {
		return fmt.Errorf("workspace ESLint is still missing after pnpm install --ignore-scripts")
	}
	return nil
}

func lintESLintInstalled(root string) bool {
	fsys := fsx.OSFileSystem{}
	for _, pkg := range lintPackages {
		if _, found := fsx.ResolveUpward(fsys, root, "node_modules", pkg.name); !found {
			return false
		}
	}
	return true
}

func lintExecutionEnvironment(actionDir string) (string, error) {
	parser, err := build.EnsureSCLParser(nil)
	if err != nil {
		return "", fmt.Errorf("failed to read action execution environment: %w", err)
	}
	environment, err := build.ParseExecutionEnvironment(parser, actionDir)
	if err != nil {
		return "", fmt.Errorf("failed to read action execution environment: %w", err)
	}
	return environment, nil
}

func runLintChecks(checks []lintCheck) {
	limit := runtime.NumCPU()
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	type checkGroup struct {
		items []int
	}
	groupsByFolder := make(map[string]int)
	var groups []checkGroup
	for index := range checks {
		if checks[index].err != nil || len(checks[index].command) == 0 {
			continue
		}
		dir := checks[index].dir
		if dir == "" {
			dir = checks[index].where
		}
		key := checks[index].where
		if key == "" {
			key = dir
		}
		groupIndex, exists := groupsByFolder[key]
		if !exists {
			groupIndex = len(groups)
			groupsByFolder[key] = groupIndex
			groups = append(groups, checkGroup{})
		}
		groups[groupIndex].items = append(groups[groupIndex].items, index)
	}

	var wg sync.WaitGroup
	for _, group := range groups {
		wg.Add(1)
		go func(group checkGroup) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Formatters may rewrite files that the other checks read in this folder.
			sort.SliceStable(group.items, func(i, j int) bool {
				return lintCheckIsFormatter(checks[group.items[i]]) && !lintCheckIsFormatter(checks[group.items[j]])
			})
			for _, index := range group.items {
				check := &checks[index]
				command := check.command[0]
				args := check.command[1:]
				cmd := lintCommandFunc(command, args...)
				cmd.Dir = check.dir
				if cmd.Dir == "" {
					cmd.Dir = check.where
				}
				output, err := lintCombinedOutputFunc(cmd)
				check.output = string(output)
				if err == nil && command == "gofmt" && strings.HasPrefix(check.name, "gofmt -l") && strings.TrimSpace(check.output) != "" {
					err = errors.New("gofmt listed files that need formatting")
				}
				if errors.Is(err, exec.ErrNotFound) && lintToolNeedsDevEnvironment(command) {
					check.output = lintToolHint(command)
				} else if err != nil && strings.TrimSpace(check.output) == "" {
					check.output = err.Error()
				}
				check.err = err
			}
		}(group)
	}
	wg.Wait()
}

func lintCheckIsFormatter(check lintCheck) bool {
	return strings.HasPrefix(check.name, "Rust fmt") || strings.HasPrefix(check.name, "gofmt ")
}

func lintToolNeedsDevEnvironment(tool string) bool {
	switch tool {
	case "npx", "pnpm", "cargo", "go", "gofmt":
		return true
	default:
		return false
	}
}

func lintToolHint(tool string) string {
	return fmt.Sprintf("%s is not available; run this command inside the workspace's dev environment (`devbox shell` or `devbox run -- simple lint`).\n", tool)
}

func reportLintResults(cmd *cobra.Command, checks []lintCheck, jsonMode bool) error {
	results := make([]lintCheckResult, 0, len(checks))
	failed := 0
	for _, check := range checks {
		status := "passed"
		if check.err != nil {
			status = "failed"
			failed++
		}
		results = append(results, lintCheckResult{
			Check: check.name, Where: check.where, Status: status, Output: check.output,
		})
	}

	if jsonMode {
		status := "complete"
		if failed > 0 {
			status = "failed"
		}
		if err := printJSONTo(cmd.OutOrStdout(), lintResult{
			Status: status, Total: len(checks), Success: len(checks) - failed, Failed: failed, Checks: results,
		}); err != nil {
			return fmt.Errorf("failed to write lint results: %w", err)
		}
	} else {
		for index, result := range results {
			mark := "✓"
			verb := "passed"
			if result.Status == "failed" {
				mark = "✗"
				verb = "failed"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s) %s\n", mark, result.Check, result.Where, verb); err != nil {
				return fmt.Errorf("failed to write lint check result: %w", err)
			}
			if result.Status == "failed" && result.Output != "" {
				if _, err := fmt.Fprint(cmd.OutOrStdout(), result.Output); err != nil {
					return fmt.Errorf("failed to write lint output for check %d: %w", index+1, err)
				}
				if !strings.HasSuffix(result.Output, "\n") {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return fmt.Errorf("failed to finish lint output for check %d: %w", index+1, err)
					}
				}
			}
		}
		if failed > 0 {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%d checks failed.\n", failed); err != nil {
				return fmt.Errorf("failed to write lint summary: %w", err)
			}
		} else if _, err := fmt.Fprintln(cmd.OutOrStdout(), "All checks passed."); err != nil {
			return fmt.Errorf("failed to write lint summary: %w", err)
		}
	}

	if failed > 0 {
		return lintFailure{failed: failed}
	}
	return nil
}

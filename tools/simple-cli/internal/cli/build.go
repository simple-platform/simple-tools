package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"
	"simple-cli/internal/ui"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	buildAll    bool
	concurrency int
)

// buildCmd represents the 'build' command.
// It compiles actions or applications logic (often using esbuild for JS/TS) into artifacts.
var buildCmd = &cobra.Command{
	Use:   "build [target]",
	Short: "Build an app or action",
	Long: `Build a specific app, an action within an app, or all apps.
Examples:
  simple build com.example.todo/add_item
  simple build com.example.todo
  simple build --all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBuild(fsx.OSFileSystem{}, args)
	},
}

func init() {
	RootCmd.AddCommand(buildCmd)
	buildCmd.Flags().BoolVar(&buildAll, "all", false, "build all actions in all apps")
	buildCmd.Flags().IntVar(&concurrency, "concurrency", 0, "number of parallel builds (default: number of CPU cores)")
}

// runBuild executes the build process.
// It orchestrates tool setup, target resolution, and parallel build execution.
func runBuild(fsys fsx.FileSystem, args []string) error {
	// Validate arguments first
	if buildAll {
		if len(args) > 0 {
			return fmt.Errorf("cannot use --all with a target argument")
		}
	} else {
		if len(args) == 0 {
			return fmt.Errorf("requires a target argument or --all flag")
		}
	}

	opts := build.BuildOptions{
		Concurrency: concurrency,
		Verbose:     !jsonOutput,
		JSONOutput:  jsonOutput,
	}
	manager := build.NewBuildManager(opts)

	// Phase 1: Ensure Tools
	// We need specific binaries (scl-parser, javy, wasm-opt, esbuild) to be present.
	// This step downloads them if missing.
	toolKeys := []string{"scl-parser", "javy", "wasm-opt"}
	if !jsonOutput {
		if err := runWithProgress(toolKeys, func(report build.ProgressReporter) {
			_ = manager.EnsureTools(report)
		}); err != nil {
			return fmt.Errorf("failed to ensure tools: %w", err)
		}
	} else {
		if err := manager.EnsureTools(nil); err != nil {
			return fmt.Errorf("failed to ensure tools: %w", err)
		}
	}

	// Phase 2: Build Actions and Spaces in parallel
	if buildAll {
		return buildAllApps(manager, fsys)
	}

	target := args[0]
	return buildTarget(manager, fsys, target)
}

// runWithProgress runs a function while displaying a progress UI (Bubble Tea).
func runWithProgress(keys []string, runFn func(build.ProgressReporter)) error {
	model := ui.NewModel(keys)
	p := tea.NewProgram(model)

	go func() {
		reporter := func(item, status string, done bool, err error) {
			p.Send(ui.ProgressMsg{
				ID:      item,
				Message: status,
				Done:    done,
				Error:   err,
			})
		}
		runFn(reporter)
		p.Send(tea.Quit())
	}()

	_, err := p.Run()
	return err
}

// buildAllApps traverses the 'apps' directory to find and build all actions and spaces in parallel.
func buildAllApps(manager *build.BuildManager, fsys fsx.FileSystem) error {
	appsDir := "apps"
	if !scaffold.PathExists(fsys, appsDir) {
		return fmt.Errorf("apps directory not found")
	}

	entries, err := fsys.ReadDir(appsDir)
	if err != nil {
		return fmt.Errorf("failed to read apps directory: %w", err)
	}

	var allActionDirs []string
	var allSpaceDirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			appDir := filepath.Join(appsDir, entry.Name())
			actionDirs, err := build.FindActions(appDir)
			if err == nil {
				for _, actionDir := range actionDirs {
					absDir, err := filepath.Abs(actionDir)
					if err == nil {
						allActionDirs = append(allActionDirs, absDir)
					}
				}
			}
			// Find spaces
			spaceDirs, err := build.FindSpaces(appDir)
			if err == nil {
				for _, spaceDir := range spaceDirs {
					absDir, err := filepath.Abs(spaceDir)
					if err == nil {
						allSpaceDirs = append(allSpaceDirs, absDir)
					}
				}
			}
		}
	}

	if len(allActionDirs) == 0 && len(allSpaceDirs) == 0 {
		if jsonOutput {
			return printJSON(map[string]interface{}{"status": "success", "actions": []string{}, "spaces": []string{}})
		}
		fmt.Println("No actions or spaces found to build.")
		return nil
	}

	return runBuildAll(manager, allActionDirs, allSpaceDirs)
}

// buildTarget resolves a single target (app, action, or shorthand) and builds it.
func buildTarget(manager *build.BuildManager, fsys fsx.FileSystem, target string) error {
	targetPath := target
	if !scaffold.PathExists(fsys, targetPath) {
		// Try resolving as app/action
		// This assumes running from root
		// If target is like "com.example.todo", check apps/com.example.todo
		targetPath = filepath.Join("apps", target)
	}

	if !scaffold.PathExists(fsys, targetPath) {
		// Try handling "app/action" shorthand by checking "apps/app/actions/action"
		parts := strings.Split(target, "/")
		if len(parts) == 2 {
			implicitPath := filepath.Join("apps", parts[0], "actions", parts[1])
			if scaffold.PathExists(fsys, implicitPath) {
				targetPath = implicitPath
			}
		}
	}

	if !scaffold.PathExists(fsys, targetPath) {
		return fmt.Errorf("build target '%s' not found", target)
	}

	// Convert to absolute path to ensure tools (like esbuild) have correct working directory context
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", targetPath, err)
	}
	targetPath = absPath

	// Check if it's an action dir (has action.scl)
	if build.IsActionDir(targetPath) {
		return runBuildAll(manager, []string{targetPath}, nil)
	}

	// Check if it's a space dir (has package.json but not action.scl)
	if build.IsSpaceDir(targetPath) {
		return runBuildAll(manager, nil, []string{targetPath})
	}

	// Check if it's an app dir (has actions or spaces inside)
	actionDirs, _ := build.FindActions(targetPath)
	spaceDirs, _ := build.FindSpaces(targetPath)

	if len(actionDirs) == 0 && len(spaceDirs) == 0 {
		return fmt.Errorf("no actions or spaces found in %s", target)
	}

	return runBuildAll(manager, actionDirs, spaceDirs)
}

// runBuildAll builds actions and spaces together in a single parallel pool.
// This ensures maximum utilization of CPU cores by not waiting for all actions
// to finish before starting space builds.
func runBuildAll(manager *build.BuildManager, actionDirs, spaceDirs []string) error {
	type buildResult struct {
		name    string
		isSpace bool
		err     error
	}

	totalCount := len(actionDirs) + len(spaceDirs)
	results := make([]buildResult, totalCount)
	ctx := context.Background()

	// Collect all names for the progress UI
	var allNames []string
	for _, dir := range actionDirs {
		allNames = append(allNames, "[Action] "+filepath.Base(dir))
	}
	for _, dir := range spaceDirs {
		allNames = append(allNames, " [Space] "+filepath.Base(dir))
	}

	buildFn := func(report build.ProgressReporter) {
		sem := make(chan struct{}, manager.BuildConcurrency())
		var wg sync.WaitGroup

		// Launch action builds
		for i, dir := range actionDirs {
			wg.Add(1)
			go func(idx int, dir string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				var reporter build.ProgressReporter
				if report != nil {
					reporter = func(item, status string, done bool, err error) {
						report("[Action] "+item, status, done, err)
					}
				}
				res := manager.BuildAction(ctx, dir, reporter)
				results[idx] = buildResult{name: res.ActionName, isSpace: false, err: res.Error}
			}(i, dir)
		}

		// Launch space builds in the same pool
		for i, dir := range spaceDirs {
			wg.Add(1)
			go func(idx int, dir string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				var reporter build.ProgressReporter
				if report != nil {
					reporter = func(item, status string, done bool, err error) {
						report(" [Space] "+item, status, done, err)
					}
				}
				res := manager.BuildSpace(ctx, dir, reporter)
				results[idx] = buildResult{name: res.SpaceName, isSpace: true, err: res.Error}
			}(len(actionDirs)+i, dir)
		}

		wg.Wait()
	}

	if !jsonOutput {
		if err := runWithProgress(allNames, buildFn); err != nil {
			return err
		}
	} else {
		buildFn(nil)
	}

	// Summarize results
	var actionSuccesses, actionFailures, spaceSuccesses, spaceFailures int
	var failedActions, failedSpaces []string
	errors := make(map[string]string)

	for _, r := range results {
		if r.isSpace {
			if r.err != nil {
				spaceFailures++
				failedSpaces = append(failedSpaces, r.name)
				errors[r.name] = r.err.Error()
			} else {
				spaceSuccesses++
			}
		} else {
			if r.err != nil {
				actionFailures++
				failedActions = append(failedActions, r.name)
				errors[r.name] = r.err.Error()
			} else {
				actionSuccesses++
			}
		}
	}

	totalFailures := actionFailures + spaceFailures

	if jsonOutput {
		return printJSON(map[string]interface{}{
			"status":         "complete",
			"total":          totalCount,
			"success":        actionSuccesses + spaceSuccesses,
			"failed":         totalFailures,
			"failedActions":  failedActions,
			"failedSpaces":   failedSpaces,
			"errors":         errors,
		})
	}

	if totalFailures > 0 {
		var parts []string
		if actionFailures > 0 {
			parts = append(parts, fmt.Sprintf("%d action(s)", actionFailures))
		}
		if spaceFailures > 0 {
			parts = append(parts, fmt.Sprintf("%d space(s)", spaceFailures))
		}
		return fmt.Errorf("%s failed to build", strings.Join(parts, " and "))
	}
	return nil
}

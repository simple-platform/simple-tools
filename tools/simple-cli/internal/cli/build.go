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
	buildCmd.Flags().IntVar(&concurrency, "concurrency", 4, "number of parallel builds")
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

	// Phase 2: Build Actions
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

// buildAllApps traverses the 'apps' directory to find and build all actions in all apps.
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

	// Build actions first
	if len(allActionDirs) > 0 {
		if err := runBuildActions(manager, allActionDirs); err != nil {
			return err
		}
	}

	// Build spaces next
	if len(allSpaceDirs) > 0 {
		if err := runBuildSpaces(manager, allSpaceDirs); err != nil {
			return err
		}
	}

	return nil
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
		return runBuildActions(manager, []string{targetPath})
	}

	// Check if it's a space dir (has package.json but not action.scl)
	if build.IsSpaceDir(targetPath) {
		return runBuildSpaces(manager, []string{targetPath})
	}

	// Check if it's an app dir (has actions or spaces inside)
	actionDirs, _ := build.FindActions(targetPath)
	spaceDirs, _ := build.FindSpaces(targetPath)

	if len(actionDirs) == 0 && len(spaceDirs) == 0 {
		return fmt.Errorf("no actions or spaces found in %s", target)
	}

	if len(actionDirs) > 0 {
		if err := runBuildActions(manager, actionDirs); err != nil {
			return err
		}
	}

	if len(spaceDirs) > 0 {
		if err := runBuildSpaces(manager, spaceDirs); err != nil {
			return err
		}
	}

	return nil
}

// runBuildActions executes the build for a list of action directories.
func runBuildActions(manager *build.BuildManager, actionDirs []string) error {
	var results []build.ActionBuildResult
	ctx := context.Background()

	if !jsonOutput {
		// Collect action names for UI
		var actionNames []string
		for _, dir := range actionDirs {
			actionNames = append(actionNames, filepath.Base(dir))
		}

		err := runWithProgress(actionNames, func(report build.ProgressReporter) {
			results = manager.BuildActions(ctx, actionDirs, report)
		})
		if err != nil {
			return err
		}
	} else {
		results = manager.BuildActions(ctx, actionDirs, nil)
	}

	// Summarize
	var successes, failures int
	var failedActions []string

	for _, result := range results {
		if result.Error != nil {
			failures++
			failedActions = append(failedActions, result.ActionName)
		} else {
			successes++
		}
	}

	if jsonOutput {
		// Include error details for each failed action
		errors := make(map[string]string)
		for _, result := range results {
			if result.Error != nil {
				errors[result.ActionName] = result.Error.Error()
			}
		}
		return printJSON(map[string]interface{}{
			"status":   "complete",
			"total":    len(results),
			"success":  successes,
			"failed":   failures,
			"failures": failedActions,
			"errors":   errors,
		})
	}

	if failures > 0 {
		return fmt.Errorf("%d action(s) failed to build", failures)
	}
	return nil
}

// runBuildSpaces executes the build for a list of space directories.
func runBuildSpaces(manager *build.BuildManager, spaceDirs []string) error {
	var results []build.SpaceBuildResult
	ctx := context.Background()

	if !jsonOutput {
		// Collect space names for UI
		var spaceNames []string
		for _, dir := range spaceDirs {
			spaceNames = append(spaceNames, filepath.Base(dir))
		}

		err := runWithProgress(spaceNames, func(report build.ProgressReporter) {

			// Build sequentially or adapt BuildActions for spaces. For simplicity, we can do parallel:
			results = make([]build.SpaceBuildResult, len(spaceDirs))
			sem := make(chan struct{}, manager.BuildConcurrency())
			var wg sync.WaitGroup

			for i, dir := range spaceDirs {
				wg.Add(1)
				go func(i int, dir string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					res := manager.BuildSpace(ctx, dir, report)
					results[i] = res
				}(i, dir)
			}
			wg.Wait()
		})
		if err != nil {
			return err
		}
	} else {
		// Simple parallel without progress
		results = make([]build.SpaceBuildResult, len(spaceDirs))
		sem := make(chan struct{}, manager.BuildConcurrency())
		var wg sync.WaitGroup

		for i, dir := range spaceDirs {
			wg.Add(1)
			go func(i int, dir string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				res := manager.BuildSpace(ctx, dir, nil)
				results[i] = res
			}(i, dir)
		}
		wg.Wait()
	}

	// Summarize
	var successes, failures int
	var failedSpaces []string

	for _, result := range results {
		if result.Error != nil {
			failures++
			failedSpaces = append(failedSpaces, result.SpaceName)
		} else {
			successes++
		}
	}

	if jsonOutput {
		errors := make(map[string]string)
		for _, result := range results {
			if result.Error != nil {
				errors[result.SpaceName] = result.Error.Error()
			}
		}
		return printJSON(map[string]interface{}{
			"status":   "complete",
			"total":    len(results),
			"success":  successes,
			"failed":   failures,
			"failures": failedSpaces,
			"errors":   errors,
		})
	}

	if failures > 0 {
		return fmt.Errorf("%d space(s) failed to build", failures)
	}
	return nil
}

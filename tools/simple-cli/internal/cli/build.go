package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"
	"simple-cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	buildAll    bool
	concurrency int
)

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
	for _, entry := range entries {
		if entry.IsDir() {
			appDir := filepath.Join(appsDir, entry.Name())
			actionDirs, err := build.FindActions(appDir)
			if err != nil {
				continue
			}
			for _, actionDir := range actionDirs {
				absDir, err := filepath.Abs(actionDir)
				if err != nil {
					continue
				}
				allActionDirs = append(allActionDirs, absDir)
			}
		}
	}

	if len(allActionDirs) == 0 {
		if jsonOutput {
			return printJSON(map[string]interface{}{"status": "success", "actions": []string{}})
		}
		fmt.Println("No actions found to build.")
		return nil
	}

	return runBuildActions(manager, allActionDirs)
}

func buildTarget(manager *build.BuildManager, fsys fsx.FileSystem, target string) error {
	targetPath := target
	if !scaffold.PathExists(fsys, targetPath) {
		// Try resolving as app/action
		// This assumes running from root
		// If target is like "com.example.todo", check apps/com.example.todo
		targetPath = filepath.Join("apps", target)
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

	// Check if it's an app dir (has actions inside)
	actionDirs, err := build.FindActions(targetPath)
	if err != nil {
		return fmt.Errorf("failed to find actions: %w", err)
	}

	if len(actionDirs) == 0 {
		return fmt.Errorf("no actions found in %s", target)
	}

	return runBuildActions(manager, actionDirs)
}

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

func init() {
	RootCmd.AddCommand(buildCmd)
	buildCmd.Flags().BoolVar(&buildAll, "all", false, "build all actions in all apps")
	buildCmd.Flags().IntVar(&concurrency, "concurrency", 4, "number of parallel builds")
}

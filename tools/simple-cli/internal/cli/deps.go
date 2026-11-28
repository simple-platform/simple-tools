package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

var depsUpdateAll bool
var updateDependenciesFunc = build.UpdateDependencies

var depsCmd = &cobra.Command{Use: "deps", Short: "Manage dependency lock files"}

var depsUpdateCmd = &cobra.Command{
	Use:   "update [target]",
	Short: "Update dependency lock files",
	Long:  "Run npm install in each action or space in a target, then report lock files whose selected versions changed.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDepsUpdate(cmd.OutOrStdout(), fsx.OSFileSystem{}, args)
	},
}

func init() {
	RootCmd.AddCommand(depsCmd)
	depsCmd.AddCommand(depsUpdateCmd)
	depsUpdateCmd.Flags().BoolVar(&depsUpdateAll, "all", false, "update every action and space in every app")
}

func runDepsUpdate(out io.Writer, fsys fsx.FileSystem, args []string) error {
	if depsUpdateAll && len(args) != 0 || !depsUpdateAll && len(args) != 1 {
		return fmt.Errorf("provide one target or use --all")
	}
	var actionDirs, spaceDirs []string
	if depsUpdateAll {
		if !scaffold.PathExists(fsys, "apps") {
			return fmt.Errorf("apps directory not found")
		}
		apps, err := fsys.ReadDir("apps")
		if err != nil {
			return fmt.Errorf("failed to read apps directory: %w", err)
		}
		for _, app := range apps {
			if !app.IsDir() {
				continue
			}
			actions, err := findBuildActions(filepath.Join("apps", app.Name()))
			if err != nil {
				return fmt.Errorf("failed to find actions in %s: %w", app.Name(), err)
			}
			spaces, err := findBuildSpaces(filepath.Join("apps", app.Name()))
			if err != nil {
				return fmt.Errorf("failed to find spaces in %s: %w", app.Name(), err)
			}
			actionDirs = append(actionDirs, actions...)
			spaceDirs = append(spaceDirs, spaces...)
		}
	} else {
		resolved, err := resolveBuildTarget(fsys, args[0])
		if err != nil {
			return err
		}
		switch {
		case resolved.action:
			actionDirs = []string{resolved.path}
		case resolved.space:
			spaceDirs = []string{resolved.path}
		default:
			actionDirs, spaceDirs = resolved.actions, resolved.spaces
		}
	}
	if len(actionDirs)+len(spaceDirs) == 0 {
		return fmt.Errorf("no actions or spaces found to update")
	}
	folders, err := npmFolders(fsys, actionDirs, spaceDirs)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return fmt.Errorf("no npm action or space folders found to update")
	}
	return updateDependencyFolders(out, folders)
}

func updateDependencyFolders(out io.Writer, dirs []string) error {
	for _, dir := range dirs {
		lockPath := filepath.Join(dir, "package-lock.json")
		before, readErr := os.ReadFile(lockPath)
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("failed to read %s: %w", lockPath, readErr)
		}
		if err := updateDependenciesFunc(dir); err != nil {
			return err
		}
		after, err := os.ReadFile(lockPath)
		if err != nil {
			return fmt.Errorf("failed to read updated lock file %s: %w", lockPath, err)
		}
		if readErr != nil || !bytes.Equal(before, after) {
			if _, err := fmt.Fprintf(out, "Updated %s; commit this lock file.\n", lockPath); err != nil {
				return fmt.Errorf("failed to report updated lock file: %w", err)
			}
		}
	}
	return nil
}

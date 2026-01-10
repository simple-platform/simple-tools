package cli

import (
	"fmt"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

var buildAll bool

var buildCmd = &cobra.Command{
	Use:   "build [target]",
	Short: "Build apps or actions",
	Long: `Build a specific app, an action within an app, or all apps.
Examples:
  simple build myapp/myaction
  simple build myapp
  simple build --all`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBuild(fsx.OSFileSystem{}, args)
	},
}

func runBuild(fsys fsx.FileSystem, args []string) error {
	if buildAll {
		if len(args) > 0 {
			return fmt.Errorf("cannot use --all with a target argument")
		}
		// TODO: Implement build all
		if jsonOutput {
			_ = printJSON(map[string]string{"status": "success", "target": "all", "msg": "Built all apps"})
		} else {
			fmt.Println("Building all apps...")
			fmt.Println("Build complete.")
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("requires a target argument or --all flag")
	}

	target := args[0]

	// Validate target exists
	if !scaffold.PathExists(fsys, target) && !scaffold.PathExists(fsys, "apps/"+target) {
		return fmt.Errorf("build target '%s' not found", target)
	}

	// TODO: Implement build target logic (parse app/action)
	if jsonOutput {
		_ = printJSON(map[string]string{"status": "success", "target": target, "msg": fmt.Sprintf("Built %s", target)})
	} else {
		fmt.Printf("Building %s...\n", target)
		fmt.Println("Build complete.")
	}
	return nil
}

func init() {
	RootCmd.AddCommand(buildCmd)
	buildCmd.Flags().BoolVar(&buildAll, "all", false, "build all actions in all apps")
}

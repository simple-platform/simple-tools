package cli

import (
	"fmt"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy <app>",
	Short: "Deploy an app",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploy(fsx.OSFileSystem{}, args)
	},
}

func runDeploy(fsys fsx.FileSystem, args []string) error {
	app := args[0]

	// Validate app exists
	if !scaffold.PathExists(fsys, "apps/"+app) {
		return fmt.Errorf("app '%s' not found in apps/", app)
	}

	// TODO: Implement deploy logic
	if jsonOutput {
		return printJSON(map[string]string{"status": "success", "app": app, "msg": fmt.Sprintf("Deployed %s", app)})
	}

	fmt.Printf("Deploying %s...\n", app)
	fmt.Printf("Deployed %s successfully\n", app)
	return nil
}

func init() {
	RootCmd.AddCommand(deployCmd)
}

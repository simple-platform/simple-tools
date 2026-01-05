package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy <app>",
	Short: "Deploy an app",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		app := args[0]
		// TODO: Implement deploy logic
		if jsonOutput {
			printJSON(map[string]string{"status": "success", "app": app, "msg": fmt.Sprintf("Deployed %s", app)})
		} else {
			fmt.Printf("Deploying %s...\n", app)
			fmt.Printf("Deployed %s successfully\n", app)
		}
	},
}

func init() {
	RootCmd.AddCommand(deployCmd)
}

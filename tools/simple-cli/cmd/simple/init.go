package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init <path>",
	Short: "Initialize a new Simple project",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		// TODO: Implement actual init logic
		if jsonOutput {
			printJSON(map[string]string{
				"status": "success",
				"path":   path,
				"msg":    "Project initialized successfully",
			})
		} else {
			fmt.Printf("Initialized Simple project at %s\n", path)
		}
	},
}

func init() {
	RootCmd.AddCommand(initCmd)
}

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	jsonOutput bool
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "simple",
	Short: "A CLI tool for the Simple platform",
	Long:  `Simple CLI manages apps, builds, and deployments in the Simple platform.`,
	// Silence usage to prevent printing help on error
	SilenceUsage: true,
	// We handle errors to support JSON output
	SilenceErrors: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		if jsonOutput {
			printErrorJSON(err)
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output results in JSON format")
}

// Helper to print JSON output
func printJSON(data interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		printErrorJSON(fmt.Errorf("failed to encode JSON output: %w", err))
		os.Exit(1)
	}
}

// Helper to print JSON error
func printErrorJSON(err error) {
	type ErrorOutput struct {
		Error string `json:"error"`
	}
	encoder := json.NewEncoder(os.Stderr)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(ErrorOutput{Error: err.Error()})
}

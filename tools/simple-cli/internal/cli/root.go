// Package cli implements the command-line interface for the Simple Platform tool.
// It uses the Cobra library to handle command routing, flag parsing, and help generation.
// This package is the entry point for all 'simple' CLI operations.
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// jsonOutput controls whether command output is formatted as JSON.
	// This is useful for programmatic usage and CI/CD pipelines.
	jsonOutput bool

	// Version is the current version of the simple-cli.
	// It is typically set at build time via -ldflags.
	Version = "dev"
)

// RootCmd represents the base command when called without any subcommands.
// It serves as the container for global flags and settings.
var RootCmd = &cobra.Command{
	Use:   "simple",
	Short: "A CLI tool for the Simple platform",
	Long:  `Simple CLI manages apps, builds, and deployments in the Simple platform.`,
	// SilenceUsage prevents Cobra from printing the help message when a command fails.
	// We handle error reporting explicitly.
	SilenceUsage: true,
	// SilenceErrors prevents Cobra from automatically printing errors.
	// We handle error printing, especially for JSON output support.
	SilenceErrors: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// It is the main entry point called by main.main().
// Returns 0 on success, 1 on failure.
func Execute() int {
	if err := RootCmd.Execute(); err != nil {
		if jsonOutput {
			printErrorJSON(err)
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		return 1
	}
	return 0
}

func init() {
	RootCmd.Version = Version
	RootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output results in JSON format")
}

// printJSON encodes data to stdout in JSON format.
// This is used when the --json flag is provided.
func printJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		printErrorJSON(fmt.Errorf("failed to encode JSON output: %w", err))
		return err
	}
	return nil
}

// printErrorJSON encodes an error to stderr in JSON format.
// It ensures that external tools parsing stderr can consistently find error details.
func printErrorJSON(err error) {
	type ErrorOutput struct {
		Error string `json:"error"`
	}
	encoder := json.NewEncoder(os.Stderr)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(ErrorOutput{Error: err.Error()})
}

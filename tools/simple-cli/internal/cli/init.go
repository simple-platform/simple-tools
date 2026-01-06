// Package cli provides the CLI commands.
package cli

import (
	"fmt"
	"path/filepath"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

// initCmd represents the init command.
var initCmd = &cobra.Command{
	Use:   "init <path>",
	Short: "Initialize a Simple Platform monorepo",
	Long: `Initialize creates a new Simple Platform monorepo with:

  - AGENTS.md: Universal AI coding guidelines
  - .simple/context/: Documentation for AI context
  - apps/: Directory for Simple Platform apps

Example:
	simple init .
  simple init my-project
  simple init /path/to/my-project`,
	Args: cobra.ExactArgs(1),
	RunE: runInit,
}

// runInit executes the init command logic.
func runInit(cmd *cobra.Command, args []string) error {
	// Resolve target path to absolute
	targetPath, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("invalid path %q: %w", args[0], err)
	}

	// Extract project name from path
	projectName := filepath.Base(targetPath)

	// Create the monorepo structure
	if err := scaffold.CreateMonorepoStructure(fsx.OSFileSystem{}, scaffold.TemplatesFS, targetPath, projectName); err != nil {
		return fmt.Errorf("failed to create monorepo: %w", err)
	}

	// Output result
	if jsonOutput {
		return printJSON(map[string]string{
			"status":  "success",
			"path":    targetPath,
			"project": projectName,
		})
	} else {
		fmt.Printf("✅ Initialized Simple Platform monorepo at %s\n\n", targetPath)
		fmt.Println("Next steps:")
		fmt.Printf("  cd %s\n", args[0])
		fmt.Println("  simple new app com.mycompany.myapp \"My App\"")
	}

	return nil
}

func init() {
	RootCmd.AddCommand(initCmd)
}

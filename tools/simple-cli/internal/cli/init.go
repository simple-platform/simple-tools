// Package cli provides the CLI commands.
package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

var tenantName string

// initCmd represents the init command.
// It scaffolds a new project structure, ensuring all necessary configuration files are present.
var initCmd = &cobra.Command{
	Use:   "init <path>",
	Short: "Initialize a Simple Platform monorepo",
	Long: `Initialize creates a new Simple Platform monorepo with:

  - AGENTS.md: Universal AI coding guidelines
  - .simple/context/: Documentation for AI context
  - apps/: Directory for Simple Platform apps
  - simple.scl: Deployment configuration

The --tenant flag is required and sets up environment configurations.

Example:
  simple init . --tenant acme
  simple init my-project --tenant mycompany`,
	Args: cobra.ExactArgs(1),
	RunE: runInit,
}

func init() {
	RootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&tenantName, "tenant", "", "tenant name for deployment configuration (required)")
	_ = initCmd.MarkFlagRequired("tenant")
}

// runInit executes the initialization logic.
// It sets up the directory structure and optionally initializes a git repository.
func runInit(cmd *cobra.Command, args []string) error {
	// Validate tenant flag early
	if tenantName == "" {
		return fmt.Errorf("--tenant flag is required")
	}

	// Resolve target path to absolute to ensure consistent file operations
	targetPath, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("invalid path %q: %w", args[0], err)
	}

	// Extract project name from path (used in templates)
	projectName := filepath.Base(targetPath)

	// Create the monorepo structure using embedded templates
	cfg := scaffold.MonorepoConfig{
		ProjectName: projectName,
		TenantName:  tenantName,
	}
	if err := scaffold.CreateMonorepoStructure(fsx.OSFileSystem{}, scaffold.TemplatesFS, targetPath, cfg); err != nil {
		return fmt.Errorf("failed to create monorepo: %w", err)
	}

	// Initialize git repo if not already inside one.
	// We check if the command runs inside the target path. If it fails, it means we are not in a git repo.
	// This prevents nested git repositories unless explicitly intended by the user (who would likely not use 'simple init' inside a repo).
	if err := exec.Command("git", "-C", targetPath, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		// Not inside a git repo, so initialize one.
		// Treat failure as a hard error so the user knows init was incomplete.
		if err := exec.Command("git", "init", targetPath).Run(); err != nil {
			return fmt.Errorf("failed to initialize git repository at %s (is git installed and on your PATH?): %w", targetPath, err)
		}
	} else {
		// Already in a git repo
		if !jsonOutput {
			fmt.Printf("ℹ️  Directory %s is already inside a git repository, skipping git init\n", targetPath)
		}
	}

	// Output result
	if jsonOutput {
		return printJSON(map[string]string{
			"status":  "success",
			"path":    targetPath,
			"project": projectName,
			"tenant":  tenantName,
		})
	} else {
		fmt.Printf("✅ Initialized Simple Platform monorepo at %s\n\n", targetPath)
		fmt.Println("Next steps:")
		fmt.Printf("  cd %s\n", args[0])
		fmt.Println("  simple new app com.mycompany.myapp \"My App\"")
	}

	return nil
}

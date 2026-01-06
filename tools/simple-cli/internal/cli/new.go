package cli

import (
	"fmt"
	"os"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

// newCmd represents the new command
var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Scaffold new components",
	Long:  `Create new applications, actions, or other components within the Simple Platform monorepo.`,
}

// newAppCmd represents the new app command
var newAppCmd = &cobra.Command{
	Use:   "app <app-id> <name>",
	Short: "Create a new application",
	Long: `Scaffold a new application package in the apps/ directory.

Arguments:
  <app-id>: Unique identifier (e.g., com.mycompany.crm)
  <name>:   Human-readable display name (e.g., "CRM System")`,
	Example: `  simple new app com.mycompany.crm "CRM System" --desc "A CRM application"`,
	Args:    cobra.ExactArgs(2),
	RunE:    runNewApp,
}

func runNewApp(cmd *cobra.Command, args []string) error {
	appID := args[0]
	name := args[1]
	desc, _ := cmd.Flags().GetString("desc")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Verify we are in a monorepo (simple check: apps dir exists)
	fsys := fsx.OSFileSystem{}
	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("apps directory not found. Are you in a Simple Platform monorepo root?")
	}

	if err := scaffold.CreateAppStructure(fsys, scaffold.TemplatesFS, cwd, appID, name, desc); err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	if jsonOutput {
		return printJSON(map[string]string{
			"status": "success",
			"app_id": appID,
			"path":   "apps/" + appID,
		})
	}

	fmt.Printf("✅ Created app %s (%s) in apps/%s\n", name, appID, appID)
	return nil
}

func init() {
	newAppCmd.Flags().StringP("desc", "d", "", "Application description")
	RootCmd.AddCommand(newCmd)
	newCmd.AddCommand(newAppCmd)
}

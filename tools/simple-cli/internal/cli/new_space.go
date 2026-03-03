package cli

import (
	"fmt"
	"os"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

// newSpaceCmd represents the 'new space' command.
// It scaffolds a new space project within an existing application.
var newSpaceCmd = &cobra.Command{
	Use:   "space <app-id> <name> <display_name>",
	Short: "Create a new space",
	Long: `Scaffold a new Space inside an app's spaces/ directory.
A Space is a full-page custom React UI built with Vite.

Arguments:
  <app-id>:       Target App ID (e.g., com.mycompany.crm)
  <name>:         Space name in kebab-case (e.g., sales-dashboard)
  <display_name>: Human-readable display name (e.g., "Sales Dashboard")`,
	Example: `  simple new space com.mycompany.crm sales-dashboard "Sales Dashboard" --desc "Executive overview"`,
	Args:    cobra.ExactArgs(3),
	RunE:    runNewSpace,
}

// runNewSpace executes the logic to scaffold a new Space.
func runNewSpace(cmd *cobra.Command, args []string) error {
	appID := args[0]
	spaceName := args[1]
	displayName := args[2]

	desc, _ := cmd.Flags().GetString("desc")

	// Validate space name format (reuses action name regex: kebab-case)
	if err := validateActionName(spaceName); err != nil {
		return fmt.Errorf("invalid space name: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Verify we are in a monorepo
	fsys := fsx.OSFileSystem{}
	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("apps directory not found. Are you in a Simple Platform monorepo root?")
	}

	cfg := scaffold.SpaceConfig{
		AppID:       appID,
		SpaceName:   spaceName,
		DisplayName: displayName,
		Description: desc,
	}

	if err := scaffold.CreateSpaceStructure(fsys, scaffold.TemplatesFS, cwd, cfg); err != nil {
		return fmt.Errorf("failed to create space: %w", err)
	}

	if jsonOutput {
		return printJSON(map[string]string{
			"status":       "success",
			"app_id":       appID,
			"name":         spaceName,
			"display_name": displayName,
			"path":         "apps/" + appID + "/spaces/" + spaceName,
		})
	}

	fmt.Printf("✅ Created space %s (%s) in apps/%s/spaces/%s\n", displayName, spaceName, appID, spaceName)
	return nil
}

func init() {
	newSpaceCmd.Flags().StringP("desc", "d", "", "Space description")
	newCmd.AddCommand(newSpaceCmd)
}

package cli

import (
	"fmt"
	"os"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

// newBehaviorCmd represents the 'new behavior' command.
// It scaffolds a new record behavior script and logs instructions.
var newBehaviorCmd = &cobra.Command{
	Use:   "behavior <app-id> <table-name>",
	Short: "Create a new record behavior",
	Long: `Scaffold a new record behavior and register it in SCL.

Arguments:
  <app-id>:     Target App ID (e.g., com.mycompany.crm)
  <table-name>: Table name to attach behavior to (e.g., order)`,
	Example: `  simple new behavior com.mycompany.crm order`,
	Args:    cobra.ExactArgs(2),
	RunE:    runNewBehavior,
}

// runNewBehavior executes the logic to scaffold a new behavior.
func runNewBehavior(cmd *cobra.Command, args []string) error {
	appID := args[0]
	tableName := args[1]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Verify we are in a monorepo
	fsys := fsx.OSFileSystem{}
	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("apps directory not found. Are you in a Simple Platform monorepo root?")
	}

	cfg := scaffold.BehaviorConfig{
		AppID:     appID,
		TableName: tableName,
	}

	if err := scaffold.CreateBehaviorStructure(fsys, scaffold.TemplatesFS, cwd, cfg); err != nil {
		return fmt.Errorf("failed to create behavior: %w", err)
	}

	if jsonOutput {
		return printJSON(map[string]string{
			"status":     "success",
			"app_id":     appID,
			"table_name": tableName,
			"path":       "apps/" + appID + "/scripts/record-behaviors/" + tableName + ".js",
		})
	}

	fmt.Printf("✅ Created behavior for table %s in apps/%s\n", tableName, appID)
	return nil
}

func init() {
	newCmd.AddCommand(newBehaviorCmd)
}

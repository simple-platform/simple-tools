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

// newActionCmd represents the new action command
var newActionCmd = &cobra.Command{
	Use:   "action <app> <name> <display_name>",
	Short: "Create a new action",
	Long: `Scaffold a new TypeScript action inside an app's actions/ directory.

Arguments:
  <app>:          App ID where the action will be created (e.g., com.mycompany.crm)
  <name>:         Action name in kebab-case (e.g., send-email)
  <display_name>: Human-readable display name (e.g., "Send Email")`,
	Example: `  simple new action com.mycompany.crm send-email "Send Email" --lang ts --scope mycompany --desc "Sends an email notification"`,
	Args:    cobra.ExactArgs(3),
	RunE:    runNewAction,
}

// validExecutionEnvs lists the valid execution environment values
var validExecutionEnvs = []string{"server", "client", "both"}

func runNewAction(cmd *cobra.Command, args []string) error {
	appID := args[0]
	actionName := args[1]
	displayName := args[2]

	lang, _ := cmd.Flags().GetString("lang")
	desc, _ := cmd.Flags().GetString("desc")
	scope, _ := cmd.Flags().GetString("scope")
	env, _ := cmd.Flags().GetString("env")

	// Validate language
	if lang != "ts" {
		return fmt.Errorf("unsupported language: %s. Only 'ts' (TypeScript) is supported", lang)
	}

	// Validate scope is provided
	if scope == "" {
		return fmt.Errorf("--scope is required (e.g., --scope mycompany)")
	}

	// Validate execution environment
	validEnv := false
	for _, v := range validExecutionEnvs {
		if env == v {
			validEnv = true
			break
		}
	}
	if !validEnv {
		return fmt.Errorf("invalid execution environment: %s. Valid values: server, client, both", env)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Verify we are in a monorepo (simple check: apps dir exists)
	fsys := fsx.OSFileSystem{}
	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("apps directory not found. Are you in a Simple Platform monorepo root?")
	}

	cfg := scaffold.ActionConfig{
		AppID:        appID,
		ActionName:   actionName,
		DisplayName:  displayName,
		Description:  desc,
		Scope:        scope,
		ExecutionEnv: env,
	}

	if err := scaffold.CreateActionStructure(fsys, scaffold.TemplatesFS, cwd, cfg); err != nil {
		return fmt.Errorf("failed to create action: %w", err)
	}

	if jsonOutput {
		return printJSON(map[string]string{
			"status":      "success",
			"app_id":      appID,
			"action_name": actionName,
			"path":        "apps/" + appID + "/actions/" + actionName,
		})
	}

	fmt.Printf("✅ Created action %s (%s) in apps/%s/actions/%s\n", displayName, actionName, appID, actionName)
	return nil
}

func init() {
	newAppCmd.Flags().StringP("desc", "d", "", "Application description")

	newActionCmd.Flags().StringP("lang", "l", "ts", "Action language (only 'ts' supported)")
	newActionCmd.Flags().StringP("desc", "d", "", "Action description")
	newActionCmd.Flags().StringP("scope", "s", "", "NPM package scope without @ (e.g., mycompany)")
	newActionCmd.Flags().StringP("env", "e", "server", "Execution environment: server, client, or both")
	_ = newActionCmd.MarkFlagRequired("scope")

	RootCmd.AddCommand(newCmd)
	newCmd.AddCommand(newAppCmd)
	newCmd.AddCommand(newActionCmd)
}

package cli

import (
	"fmt"
	"os"
	"regexp"
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

// actionNameRegex validates action names: all lowercase, starts with letter, only letters/numbers/hyphens
var actionNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// validateActionName checks that the action name follows the naming conventions
func validateActionName(name string) error {
	if !actionNameRegex.MatchString(name) {
		return fmt.Errorf("invalid action name: %q. Must be all lowercase, start with a letter, and contain only letters, numbers, and hyphens", name)
	}
	return nil
}

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

	// Validate action name format
	if err := validateActionName(actionName); err != nil {
		return err
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

	// Register trigger commands
	newCmd.AddCommand(newTriggerTimedCmd)
	newCmd.AddCommand(newTriggerDbCmd)
	newCmd.AddCommand(newTriggerWebhookCmd)

	// Trigger common flags
	for _, cmd := range []*cobra.Command{newTriggerTimedCmd, newTriggerDbCmd, newTriggerWebhookCmd} {
		cmd.Flags().StringP("action", "a", "", "Action name to link to")
		cmd.Flags().StringP("desc", "d", "", "Trigger description")
		_ = cmd.MarkFlagRequired("action")
	}

	// Timed trigger flags
	newTriggerTimedCmd.Flags().StringP("frequency", "f", "", "Frequency: minutely, hourly, daily, weekly, monthly, yearly")
	newTriggerTimedCmd.Flags().IntP("interval", "i", 1, "Interval between runs")
	newTriggerTimedCmd.Flags().StringP("time", "t", "00:00:00", "Time of day (HH:MM:SS)")
	newTriggerTimedCmd.Flags().StringP("timezone", "z", "UTC", "Timezone")
	newTriggerTimedCmd.Flags().String("days", "", "Specific days (MON,TUE,WED,THU,FRI,SAT,SUN)")
	newTriggerTimedCmd.Flags().Bool("weekdays", false, "Run on weekdays (Mon-Fri)")
	newTriggerTimedCmd.Flags().Bool("weekends", false, "Run on weekends (Sat-Sun)")
	newTriggerTimedCmd.Flags().StringP("week-of-month", "w", "", "Week of month: first, second, third, fourth, fifth, last")
	newTriggerTimedCmd.Flags().String("start-at", "", "Start time (ISO8601)")
	newTriggerTimedCmd.Flags().String("end-at", "", "End time (ISO8601)")
	newTriggerTimedCmd.Flags().String("on-overlap", "skip", "Overlap policy: skip, queue, allow")
	_ = newTriggerTimedCmd.MarkFlagRequired("frequency")

	// DB trigger flags
	newTriggerDbCmd.Flags().StringP("table", "t", "", "Table name to watch")
	newTriggerDbCmd.Flags().StringP("ops", "o", "insert", "Operations: insert,update,delete (comma-separated)")
	newTriggerDbCmd.Flags().StringP("condition", "c", "", "JQ condition")
	_ = newTriggerDbCmd.MarkFlagRequired("table")

	// Webhook trigger flags
	newTriggerWebhookCmd.Flags().StringP("method", "m", "post", "HTTP method: get, post, put, delete")
	newTriggerWebhookCmd.Flags().BoolP("public", "p", false, "Make endpoint public")
}

// -----------------------------------------------------------
// Trigger Commands
// -----------------------------------------------------------

var newTriggerTimedCmd = &cobra.Command{
	Use:   "trigger:timed <app> <name> <display_name>",
	Short: "Create a timed trigger",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNewTrigger(cmd, args, "timed")
	},
}

var newTriggerDbCmd = &cobra.Command{
	Use:   "trigger:db <app> <name> <display_name>",
	Short: "Create a database event trigger",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNewTrigger(cmd, args, "db")
	},
}

var newTriggerWebhookCmd = &cobra.Command{
	Use:   "trigger:webhook <app> <name> <display_name>",
	Short: "Create a webhook trigger",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNewTrigger(cmd, args, "webhook")
	},
}

func runNewTrigger(cmd *cobra.Command, args []string, triggerType string) error {
	appID := args[0]
	triggerName := args[1]
	displayName := args[2]

	// Common flags
	actionName, _ := cmd.Flags().GetString("action")
	desc, _ := cmd.Flags().GetString("desc")

	// Validate name format
	if err := validateActionName(triggerName); err != nil {
		return err
	}

	// Prepare config
	cfg := scaffold.TriggerConfig{
		AppID:          appID,
		TriggerName:    triggerName,
		TriggerNameScl: regexp.MustCompile(`-`).ReplaceAllString(triggerName, "_"),
		DisplayName:    displayName,
		Description:    desc,
		TriggerType:    triggerType,
		ActionName:     actionName,
	}

	// Populate type-specific flags
	switch triggerType {
	case "timed":
		freq, _ := cmd.Flags().GetString("frequency")
		// Validate frequency
		validFreqs := map[string]bool{"minutely": true, "hourly": true, "daily": true, "weekly": true, "monthly": true, "yearly": true}
		if !validFreqs[freq] {
			return fmt.Errorf("invalid frequency: %s", freq)
		}
		cfg.Frequency = freq

		cfg.Interval, _ = cmd.Flags().GetInt("interval")
		cfg.Time, _ = cmd.Flags().GetString("time")
		cfg.Timezone, _ = cmd.Flags().GetString("timezone")

		daysStr, _ := cmd.Flags().GetString("days")
		if daysStr != "" {
			// Convert comma-separated string to JSON string array: "MON,TUE" -> `["MON", "TUE"]`
			parts := make([]string, 0)
			for _, d := range regexp.MustCompile(`,`).Split(daysStr, -1) {
				parts = append(parts, fmt.Sprintf(`"%s"`, d))
			}
			cfg.Days = fmt.Sprintf(`[%s]`, regexp.MustCompile(`\s+`).ReplaceAllString(regexp.MustCompile(`,`).ReplaceAllString(daysStr, `", "`), ""))
			// Better explicit construction
			cfg.Days = "["
			for i, p := range regexp.MustCompile(`,`).Split(daysStr, -1) {
				if i > 0 {
					cfg.Days += ", "
				}
				cfg.Days += fmt.Sprintf(`"%s"`, p)
			}
			cfg.Days += "]"
		}

		cfg.Weekdays, _ = cmd.Flags().GetBool("weekdays")
		cfg.Weekends, _ = cmd.Flags().GetBool("weekends")
		cfg.WeekOfMonth, _ = cmd.Flags().GetString("week-of-month")
		cfg.StartAt, _ = cmd.Flags().GetString("start-at")
		cfg.EndAt, _ = cmd.Flags().GetString("end-at")
		cfg.OnOverlap, _ = cmd.Flags().GetString("on-overlap")

	case "db":
		cfg.TableName, _ = cmd.Flags().GetString("table")

		opsStr, _ := cmd.Flags().GetString("ops")
		// Convert ops to JSON array: "insert,update" -> `["insert", "update"]`
		cfg.Operations = "["
		for i, p := range regexp.MustCompile(`,`).Split(opsStr, -1) {
			if i > 0 {
				cfg.Operations += ", "
			}
			cfg.Operations += fmt.Sprintf(`"%s"`, p)
		}
		cfg.Operations += "]"

		cfg.Condition, _ = cmd.Flags().GetString("condition")

	case "webhook":
		cfg.Method, _ = cmd.Flags().GetString("method")
		cfg.IsPublic, _ = cmd.Flags().GetBool("public")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Verify monorepo
	fsys := fsx.OSFileSystem{}
	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("apps directory not found. Are you in a Simple Platform monorepo root?")
	}

	if err := scaffold.CreateTriggerStructure(fsys, scaffold.TemplatesFS, cwd, cfg); err != nil {
		return fmt.Errorf("failed to create trigger: %w", err)
	}

	if jsonOutput {
		return printJSON(map[string]string{
			"status":       "success",
			"app_id":       appID,
			"trigger_name": triggerName,
			"type":         triggerType,
		})
	}

	fmt.Printf("✅ Created %s trigger %s (%s) for app %s\n", triggerType, displayName, triggerName, appID)
	return nil
}

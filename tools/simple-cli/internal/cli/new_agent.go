package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

var agentCommandFS fsx.FileSystem = fsx.OSFileSystem{}
var agentCommandRoot = os.Getwd
var createAgentWiring = scaffold.CreateAgent
var createTaskTypeWiring = scaffold.CreateTaskType
var createSkillWiring = scaffold.CreateSkill
var createToolWiring = scaffold.CreateTool

var newAgentCmd = &cobra.Command{
	Use:   "agent <app> <name> <display-name>",
	Short: "Create an agent and its persona",
	Args:  cobra.ExactArgs(3),
	RunE:  runNewAgent,
}

var newTaskTypeCmd = &cobra.Command{
	Use:   "task-type <app> <name> <plural-name>",
	Short: "Create a kind of work for an agent",
	Args:  cobra.ExactArgs(3),
	RunE:  runNewTaskType,
}

var newSkillCmd = &cobra.Command{
	Use:   "skill <app> <name> [title]",
	Short: "Create a skill or link an existing one",
	Args:  cobra.RangeArgs(2, 3),
	RunE:  runNewSkill,
}

var newToolCmd = &cobra.Command{
	Use:   "tool <app> <name> [display-name]",
	Short: "Link or create a tool for a kind of work",
	Args:  cobra.RangeArgs(2, 3),
	RunE:  runNewTool,
}

func runNewAgent(cmd *cobra.Command, args []string) error {
	appID, name, displayName := args[0], args[1], args[2]
	desc, _ := cmd.Flags().GetString("desc")
	if err := validateActionName(name); err != nil {
		return fmt.Errorf("invalid agent name: %w", err)
	}
	if utf8.RuneCountInString(displayName) > 100 {
		return fmt.Errorf("agent display name is longer than 100 characters")
	}
	if err := validateNewText("display name", displayName, 100); err != nil {
		return err
	}
	if err := validateNewText("description", desc, 1000); err != nil {
		return err
	}
	root, err := agentCommandRoot()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	result, err := createAgentWiring(agentCommandFS, scaffold.TemplatesFS, root, scaffold.AgentConfig{
		AppID: appID, Name: name, DisplayName: displayName, Description: desc,
	})
	if err != nil {
		return err
	}
	return printNewResult(cmd, appID, name, "agent", result)
}

func runNewTaskType(cmd *cobra.Command, args []string) error {
	appID, name, pluralName := args[0], args[1], args[2]
	desc, _ := cmd.Flags().GetString("desc")
	agentName, _ := cmd.Flags().GetString("agent")
	toolsText, _ := cmd.Flags().GetString("tools")
	every, _ := cmd.Flags().GetString("every")
	if err := validateTaskTypeName(name); err != nil {
		return err
	}
	if len(name) > 100 {
		return fmt.Errorf("task-type name is longer than 100 characters")
	}
	if utf8.RuneCountInString(pluralName) > 100 {
		return fmt.Errorf("plural name is longer than 100 characters")
	}
	if err := validateNewText("plural name", pluralName, 100); err != nil {
		return err
	}
	if err := validateNewText("description", desc, 1000); err != nil {
		return err
	}
	wakeSeconds := int64(3600)
	if cmd.Flags().Changed("every") {
		parsedSeconds, parseErr := parseNewWakeInterval(every)
		if parseErr != nil {
			return parseErr
		}
		wakeSeconds = parsedSeconds
	}
	tools, err := parseToolNames(toolsText)
	if err != nil {
		return err
	}
	root, err := agentCommandRoot()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	result, err := createTaskTypeWiring(agentCommandFS, scaffold.TemplatesFS, root, scaffold.TaskTypeConfig{
		AppID: appID, Name: name, PluralName: pluralName, Description: desc, AgentName: agentName,
		Tools: tools, WakeIntervalSeconds: wakeSeconds,
	})
	if err != nil {
		return err
	}
	return printNewResult(cmd, appID, name, "task-type", result)
}

func runNewSkill(cmd *cobra.Command, args []string) error {
	appID, name := args[0], args[1]
	title := ""
	if len(args) == 3 {
		title = args[2]
	}
	desc, _ := cmd.Flags().GetString("desc")
	taskType, _ := cmd.Flags().GetString("task-type")
	if err := validateActionName(name); err != nil {
		return fmt.Errorf("invalid skill name: %w", err)
	}
	if utf8.RuneCountInString(title) > 255 {
		return fmt.Errorf("knowledge title is longer than 255 characters")
	}
	if title != "" {
		if err := validateNewText("title", title, 255); err != nil {
			return err
		}
	}
	if err := validateNewText("description", desc, 1000); err != nil {
		return err
	}
	if taskType == "" {
		return fmt.Errorf("--task-type is required")
	}
	if err := validateTaskTypeName(taskType); err != nil {
		return fmt.Errorf("invalid task-type name: %w", err)
	}
	root, err := agentCommandRoot()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	result, err := createSkillWiring(agentCommandFS, scaffold.TemplatesFS, root, scaffold.SkillConfig{
		AppID: appID, Name: name, Title: title, Description: desc, TaskTypeName: taskType,
	})
	if err != nil {
		return err
	}
	return printNewResult(cmd, appID, name, "skill", result)
}

func runNewTool(cmd *cobra.Command, args []string) error {
	appID, name := args[0], args[1]
	displayName := ""
	if len(args) == 3 {
		displayName = args[2]
	}
	desc, _ := cmd.Flags().GetString("desc")
	taskType, _ := cmd.Flags().GetString("task-type")
	lang, _ := cmd.Flags().GetString("lang")
	scope, _ := cmd.Flags().GetString("scope")
	env, _ := cmd.Flags().GetString("env")
	if err := validateActionName(name); err != nil {
		return fmt.Errorf("invalid tool name: %w", err)
	}
	if utf8.RuneCountInString(displayName) > 100 {
		return fmt.Errorf("display name is longer than 100 characters")
	}
	if displayName != "" {
		if err := validateNewText("display name", displayName, 100); err != nil {
			return err
		}
	}
	if err := validateNewText("description", desc, 1000); err != nil {
		return err
	}
	if taskType == "" {
		return fmt.Errorf("--task-type is required")
	}
	if err := validateTaskTypeName(taskType); err != nil {
		return fmt.Errorf("invalid task-type name: %w", err)
	}
	language, known := actionLanguages[lang]
	if !known {
		return fmt.Errorf("unsupported language: %s. Supported: 'ts' (TypeScript), 'rust' (Rust)", lang)
	}
	scope = strings.TrimPrefix(scope, "@")
	validEnv := false
	for _, value := range validExecutionEnvs {
		if env == value {
			validEnv = true
			break
		}
	}
	if !validEnv {
		return fmt.Errorf("invalid execution environment: %s. Valid values: server, client, both", env)
	}
	root, err := agentCommandRoot()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	result, err := createToolWiring(agentCommandFS, scaffold.TemplatesFS, root, scaffold.ToolConfig{
		AppID: appID, Name: name, DisplayName: displayName, Description: desc, TaskTypeName: taskType,
		Language: language, Scope: scope, ExecutionEnv: env,
	})
	if err != nil {
		return err
	}
	return printNewResult(cmd, appID, name, "tool", result)
}

func printNewResult(cmd *cobra.Command, appID, name, kind string, result scaffold.WiringResult) error {
	created := result.Created
	if created == nil {
		created = []string{}
	}
	records := result.Records
	if records == nil {
		records = []string{}
	}
	fillIn := result.FillIn
	if fillIn == nil {
		fillIn = []string{}
	}
	if jsonOutput {
		return printJSONTo(cmd.OutOrStdout(), map[string]any{
			"status": "success", "app_id": appID, "name": name, "created": created, "records": records, "fill_in": fillIn,
		})
	}
	if result.NothingToAdd {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Nothing to add for %s %s in %s.\n", kind, name, appID); err != nil {
			return fmt.Errorf("failed to write command output: %w", err)
		}
	} else if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Created %s %s in %s.\n", kind, name, appID); err != nil {
		return fmt.Errorf("failed to write command output: %w", err)
	}
	if len(fillIn) == 0 {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Nothing to fill in."); err != nil {
			return fmt.Errorf("failed to write command output: %w", err)
		}
	} else {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Fill in:"); err != nil {
			return fmt.Errorf("failed to write command output: %w", err)
		}
		for _, path := range fillIn {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path); err != nil {
				return fmt.Errorf("failed to write command output: %w", err)
			}
		}
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Run `simple build %s` to see what is left.\n", appID); err != nil {
		return fmt.Errorf("failed to write command output: %w", err)
	}
	return nil
}

func validateTaskTypeName(name string) error {
	if !taskTypeNamePattern.MatchString(name) {
		return fmt.Errorf("invalid task-type name %q: use lowercase letters, digits, hyphens, or underscores, starting with a letter", name)
	}
	return nil
}

func validateNewText(field, value string, limit int) error {
	if err := validateActionText(field, value); err != nil {
		return err
	}
	if utf8.RuneCountInString(value) > limit {
		return fmt.Errorf("%s is longer than %d characters", field, limit)
	}
	return nil
}

func parseNewWakeInterval(value string) (int64, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second || duration%time.Second != 0 {
		return 0, fmt.Errorf("--every must be a whole-second duration of at least one second, such as 90s, 30m, or 1h")
	}
	return int64(duration / time.Second), nil
}

var taskTypeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func parseToolNames(value string) ([]string, error) {
	if value == "" {
		return []string{}, nil
	}
	result := make([]string, 0)
	seen := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("--tools contains an empty name; give each tool as a comma-separated name")
		}
		if err := validateActionName(name); err != nil {
			return nil, fmt.Errorf("invalid tool name in --tools: %w", err)
		}
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result, nil
}

func init() {
	for _, command := range []*cobra.Command{newAgentCmd, newTaskTypeCmd, newSkillCmd, newToolCmd} {
		command.Flags().StringP("desc", "d", "", "Description")
	}
	newTaskTypeCmd.Flags().String("agent", "", "Default agent name")
	newTaskTypeCmd.Flags().String("tools", "", "Comma-separated platform tools or app actions")
	newTaskTypeCmd.Flags().String("every", "", "Wake interval in whole seconds, such as 90s, 30m, or 1h")
	newSkillCmd.Flags().String("task-type", "", "Kind of work to receive this skill")
	newToolCmd.Flags().String("task-type", "", "Kind of work to receive this tool")
	newToolCmd.Flags().StringP("lang", "l", "ts", "Action language: ts or rust")
	newToolCmd.Flags().StringP("scope", "s", "", "NPM package scope without @; TypeScript actions only")
	newToolCmd.Flags().StringP("env", "e", "server", "Execution environment: server, client, or both")
	_ = newSkillCmd.MarkFlagRequired("task-type")
	_ = newToolCmd.MarkFlagRequired("task-type")
	newCmd.AddCommand(newAgentCmd, newTaskTypeCmd, newSkillCmd, newToolCmd)
}

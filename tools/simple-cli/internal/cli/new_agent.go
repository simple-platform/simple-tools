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
var createKnowledgeWiring = scaffold.CreateKnowledge
var createToolWiring = scaffold.CreateTool

var newAgentCmd = &cobra.Command{
	Use:   "agent <app> <name> <display-name>",
	Short: "Create an agent and its persona",
	Args:  cobra.ExactArgs(3),
	RunE:  runNewAgent,
}

var newTaskTypeCmd = &cobra.Command{
	Use:   "task-type <app> <name> <plural-name>",
	Short: "Create a kind of work for an agent or people",
	Args:  cobra.ExactArgs(3),
	RunE:  runNewTaskType,
}

var newSkillCmd = &cobra.Command{
	Use:   "skill <app> <name> [title]",
	Short: "Create a skill or link an existing one",
	Args:  cobra.RangeArgs(2, 3),
	RunE:  runNewSkill,
}

var newKnowledgeCmd = &cobra.Command{
	Use:   "knowledge <app> <name> [title]",
	Short: "Create reference knowledge or link an existing body",
	Args:  cobra.RangeArgs(2, 3),
	RunE:  runNewKnowledge,
}

var newToolCmd = &cobra.Command{
	Use:   "tool <app> <name> [display-name]",
	Short: "Set up a tool and optionally link it to a kind of work",
	Args:  cobra.RangeArgs(2, 3),
	RunE:  runNewTool,
}

func runNewAgent(cmd *cobra.Command, args []string) error {
	appID, name, displayName := args[0], args[1], args[2]
	desc, _ := cmd.Flags().GetString("desc")
	if err := validateNamedCommandName("agent", name); err != nil {
		return err
	}
	if utf8.RuneCountInString(displayName) > 100 {
		return fmt.Errorf("agent %q display name is too long; use 100 characters or fewer", name)
	}
	if err := validateNewText("agent", name, "display name", displayName, 100); err != nil {
		return err
	}
	if err := validateNewText("agent", name, "description", desc, 1000); err != nil {
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
	noAgent, _ := cmd.Flags().GetBool("no-agent")
	if err := validateTaskTypeName(name); err != nil {
		return err
	}
	if len(name) > 100 {
		return fmt.Errorf("kind of work %q name is too long; use 100 characters or fewer", name)
	}
	if utf8.RuneCountInString(pluralName) > 100 {
		return fmt.Errorf("kind of work %q plural name is too long; use 100 characters or fewer", name)
	}
	if err := validateNewText("kind of work", name, "plural name", pluralName, 100); err != nil {
		return err
	}
	if err := validateNewText("kind of work", name, "description", desc, 1000); err != nil {
		return err
	}
	if noAgent && (cmd.Flags().Changed("agent") || cmd.Flags().Changed("tools") || cmd.Flags().Changed("every")) {
		return fmt.Errorf("kind of work %q cannot use --no-agent with --agent, --tools, or --every; remove those flags", name)
	}
	wakeSeconds := int64(3600)
	if cmd.Flags().Changed("every") {
		parsedSeconds, parseErr := parseNewWakeInterval(every)
		if parseErr != nil {
			return fmt.Errorf("kind of work %q: %w", name, parseErr)
		}
		wakeSeconds = parsedSeconds
	}
	tools, err := parseToolNames(name, toolsText)
	if err != nil {
		return err
	}
	root, err := agentCommandRoot()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	result, err := createTaskTypeWiring(agentCommandFS, scaffold.TemplatesFS, root, scaffold.TaskTypeConfig{
		AppID: appID, Name: name, PluralName: pluralName, Description: desc, AgentName: agentName,
		Tools: tools, WakeIntervalSeconds: wakeSeconds, NoAgent: noAgent,
		AgentFlagSet: cmd.Flags().Changed("agent"), ToolsFlagSet: cmd.Flags().Changed("tools"), EveryFlagSet: cmd.Flags().Changed("every"),
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
	if err := validateNamedCommandName("skill", name); err != nil {
		return err
	}
	if utf8.RuneCountInString(title) > 255 {
		return fmt.Errorf("skill %q title is too long; use 255 characters or fewer", name)
	}
	if title != "" {
		if err := validateNewText("skill", name, "title", title, 255); err != nil {
			return err
		}
	}
	if err := validateNewText("skill", name, "description", desc, 1000); err != nil {
		return err
	}
	if taskType == "" {
		return fmt.Errorf("skill %q needs --task-type; name the kind of work to receive it", name)
	}
	if err := validateTaskTypeName(taskType); err != nil {
		return err
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

func runNewKnowledge(cmd *cobra.Command, args []string) error {
	appID, name := args[0], args[1]
	title := ""
	if len(args) == 3 {
		title = args[2]
	}
	taskType, _ := cmd.Flags().GetString("task-type")
	if err := validateNamedCommandName("reference knowledge", name); err != nil {
		return err
	}
	for _, prefix := range []string{"skill-", "tool-", "playbook-"} {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("reference knowledge %q cannot use the reserved %q prefix; choose a name without it", name, prefix)
		}
	}
	if utf8.RuneCountInString(title) > 255 {
		return fmt.Errorf("reference knowledge %q title is too long; use 255 characters or fewer", name)
	}
	if title != "" {
		if err := validateNewText("reference knowledge", name, "title", title, 255); err != nil {
			return err
		}
	}
	if taskType == "" {
		return fmt.Errorf("reference knowledge %q needs --task-type; name the kind of work to receive it", name)
	}
	if err := validateTaskTypeName(taskType); err != nil {
		return err
	}
	root, err := agentCommandRoot()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	result, err := createKnowledgeWiring(agentCommandFS, scaffold.TemplatesFS, root, scaffold.KnowledgeConfig{
		AppID: appID, Name: name, Title: title, TaskTypeName: taskType,
	})
	if err != nil {
		return err
	}
	return printNewResult(cmd, appID, name, "knowledge", result)
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
	if err := validateNamedCommandName("tool", name); err != nil {
		return err
	}
	if utf8.RuneCountInString(displayName) > 100 {
		return fmt.Errorf("tool %q display name is too long; use 100 characters or fewer", name)
	}
	if displayName != "" {
		if err := validateNewText("tool", name, "display name", displayName, 100); err != nil {
			return err
		}
	}
	if err := validateNewText("tool", name, "description", desc, 1000); err != nil {
		return err
	}
	if taskType != "" {
		if err := validateTaskTypeName(taskType); err != nil {
			return err
		}
	}
	language, known := actionLanguages[lang]
	if !known {
		return fmt.Errorf("tool %q has unsupported language %q; choose ts or rust", name, lang)
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
		return fmt.Errorf("tool %q has unsupported environment %q; choose server, client, or both", name, env)
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
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Nothing to fill in for %s %s in %s.\n", kind, name, appID); err != nil {
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

func validateNamedCommandName(kind, name string) error {
	if !agentCommandNameRegex.MatchString(name) {
		return fmt.Errorf("%s %q has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter", kind, name)
	}
	return nil
}

func validateTaskTypeName(name string) error {
	if !taskTypeNamePattern.MatchString(name) {
		return fmt.Errorf("kind of work %q has an invalid name; use lowercase letters and digits in groups joined by single hyphens or underscores, starting with a letter", name)
	}
	return nil
}

func validateNewText(kind, name, field, value string, limit int) error {
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("%s %q %s must be a single line; remove line breaks", kind, name, field)
	}
	if strings.Contains(value, `"`) {
		return fmt.Errorf("%s %q %s must not contain a double quote; remove the quote", kind, name, field)
	}
	if strings.Contains(value, `\`) {
		return fmt.Errorf("%s %q %s must not contain a backslash; remove it", kind, name, field)
	}
	if utf8.RuneCountInString(value) > limit {
		return fmt.Errorf("%s %q %s is longer than %d characters; shorten it", kind, name, field, limit)
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

var agentCommandNameRegex = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var taskTypeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)

func parseToolNames(taskTypeName, value string) ([]string, error) {
	if value == "" {
		return []string{}, nil
	}
	result := make([]string, 0)
	seen := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("kind of work %q --tools value %q has an empty tool name; remove empty entries", taskTypeName, value)
		}
		if !agentCommandNameRegex.MatchString(name) {
			return nil, fmt.Errorf("tool %q in --tools has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter", name)
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
	newTaskTypeCmd.Flags().Bool("no-agent", false, "Create work for people without an agent")
	newSkillCmd.Flags().String("task-type", "", "Kind of work to receive this skill")
	newKnowledgeCmd.Flags().String("task-type", "", "Kind of work to receive this reference knowledge")
	newToolCmd.Flags().String("task-type", "", "Optional kind of work to receive this tool")
	newToolCmd.Flags().StringP("lang", "l", "ts", "Action language: ts or rust")
	newToolCmd.Flags().StringP("scope", "s", "", "NPM package scope without @; TypeScript actions only")
	newToolCmd.Flags().StringP("env", "e", "server", "Execution environment: server, client, or both")
	newCmd.AddCommand(newAgentCmd, newTaskTypeCmd, newSkillCmd, newKnowledgeCmd, newToolCmd)
}

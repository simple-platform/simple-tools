package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestNewAgentCommandsOutput(t *testing.T) {
	tests := []struct {
		name  string
		cmd   *cobra.Command
		args  []string
		setup func(*testing.T, *cobra.Command)
		run   func(*cobra.Command, []string) error
		kind  string
		arg   string
	}{
		{name: "agent", cmd: newAgentCmd, args: []string{"com.acme.shop", "support", "Support Agent"}, run: runNewAgent, kind: "agent"},
		{name: "task type", cmd: newTaskTypeCmd, args: []string{"com.acme.shop", "returns", "Returns"}, setup: func(_ *testing.T, cmd *cobra.Command) { _ = cmd.Flags().Set("every", "90s") }, run: runNewTaskType, kind: "task-type"},
		{name: "skill", cmd: newSkillCmd, args: []string{"com.acme.shop", "refund-policy", "Refund Policy"}, setup: func(_ *testing.T, cmd *cobra.Command) { _ = cmd.Flags().Set("task-type", "returns") }, run: runNewSkill, kind: "skill"},
		{name: "tool", cmd: newToolCmd, args: []string{"com.acme.shop", "process-data"}, setup: func(_ *testing.T, cmd *cobra.Command) { _ = cmd.Flags().Set("task-type", "returns") }, run: runNewTool, kind: "tool"},
	}
	for _, tt := range tests {
		for _, jsonMode := range []bool{false, true} {
			label := "text"
			if jsonMode {
				label = "json"
			}
			t.Run(tt.name+"/"+label, func(t *testing.T) {
				oldRoot, oldFS := agentCommandRoot, agentCommandFS
				oldJSON := jsonOutput
				oldAgent, oldTask, oldSkill, oldTool := createAgentWiring, createTaskTypeWiring, createSkillWiring, createToolWiring
				t.Cleanup(func() {
					agentCommandRoot, agentCommandFS, jsonOutput = oldRoot, oldFS, oldJSON
					createAgentWiring, createTaskTypeWiring, createSkillWiring, createToolWiring = oldAgent, oldTask, oldSkill, oldTool
					resetNewFlags(tt.cmd)
				})
				agentCommandRoot = func() (string, error) { return "/repo", nil }
				agentCommandFS = &fsx.MockFileSystem{Files: map[string][]byte{}}
				jsonOutput = jsonMode
				result := scaffold.WiringResult{Created: []string{"apps/com.acme.shop/knowledge/example.md"}, Records: []string{"example_key"}, FillIn: []string{"apps/com.acme.shop/knowledge/example.md"}}
				createAgentWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.AgentConfig) (scaffold.WiringResult, error) {
					return result, nil
				}
				createTaskTypeWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.TaskTypeConfig) (scaffold.WiringResult, error) {
					return result, nil
				}
				createSkillWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.SkillConfig) (scaffold.WiringResult, error) {
					return result, nil
				}
				createToolWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.ToolConfig) (scaffold.WiringResult, error) {
					return result, nil
				}
				resetNewFlags(tt.cmd)
				if tt.setup != nil {
					tt.setup(t, tt.cmd)
				}
				var output bytes.Buffer
				tt.cmd.SetOut(&output)
				if err := tt.run(tt.cmd, tt.args); err != nil {
					t.Fatalf("command returned error: %v", err)
				}
				if jsonMode {
					var document struct {
						Status  string   `json:"status"`
						AppID   string   `json:"app_id"`
						Name    string   `json:"name"`
						Created []string `json:"created"`
						Records []string `json:"records"`
						FillIn  []string `json:"fill_in"`
					}
					if err := json.Unmarshal(output.Bytes(), &document); err != nil {
						t.Fatalf("invalid JSON output %q: %v", output.String(), err)
					}
					if document.Status != "success" || document.AppID != "com.acme.shop" || document.Name != tt.args[1] || len(document.Created) != 1 || len(document.Records) != 1 || len(document.FillIn) != 1 {
						t.Fatalf("JSON result = %#v", document)
					}
				} else {
					for _, want := range []string{"Created " + tt.kind + " " + tt.args[1], "Fill in:", "- apps/com.acme.shop/knowledge/example.md", "Run `simple build com.acme.shop` to see what is left."} {
						if !strings.Contains(output.String(), want) {
							t.Fatalf("output %q does not contain %q", output.String(), want)
						}
					}
				}
			})
		}
	}
}

func TestNewCommandArgumentValidation(t *testing.T) {
	oldRoot, oldFS := agentCommandRoot, agentCommandFS
	t.Cleanup(func() { agentCommandRoot, agentCommandFS = oldRoot, oldFS })
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{name: "agent name", call: func() error {
			resetNewFlags(newAgentCmd)
			return runNewAgent(newAgentCmd, []string{"com.acme.shop", "Bad_Name", "Support"})
		}, want: "invalid action name"},
		{name: "agent display length", call: func() error {
			resetNewFlags(newAgentCmd)
			return runNewAgent(newAgentCmd, []string{"com.acme.shop", "support", strings.Repeat("x", 101)})
		}, want: "100 characters"},
		{name: "task type underscore allowed", call: func() error {
			resetNewFlags(newTaskTypeCmd)
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "price_check", "Price Checks"})
		}, want: "does not exist"},
		{name: "task type bad underscore prefix", call: func() error {
			resetNewFlags(newTaskTypeCmd)
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "_price", "Price Checks"})
		}, want: "invalid task-type name"},
		{name: "skill requires task type", call: func() error {
			resetNewFlags(newSkillCmd)
			return runNewSkill(newSkillCmd, []string{"com.acme.shop", "refund-policy", "Refund Policy"})
		}, want: "--task-type is required"},
		{name: "tool requires task type", call: func() error {
			resetNewFlags(newToolCmd)
			return runNewTool(newToolCmd, []string{"com.acme.shop", "process-data"})
		}, want: "--task-type is required"},
		{name: "bad tool language", call: func() error {
			resetNewFlags(newToolCmd)
			_ = newToolCmd.Flags().Set("task-type", "returns")
			_ = newToolCmd.Flags().Set("lang", "go")
			return runNewTool(newToolCmd, []string{"com.acme.shop", "process-data"})
		}, want: "unsupported language"},
		{name: "bad tool environment", call: func() error {
			resetNewFlags(newToolCmd)
			_ = newToolCmd.Flags().Set("task-type", "returns")
			_ = newToolCmd.Flags().Set("env", "edge")
			return runNewTool(newToolCmd, []string{"com.acme.shop", "process-data"})
		}, want: "invalid execution environment"},
		{name: "bad every", call: func() error {
			resetNewFlags(newTaskTypeCmd)
			_ = newTaskTypeCmd.Flags().Set("every", "500ms")
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "returns", "Returns"})
		}, want: "whole-second duration"},
		{name: "quoted text refused", call: func() error {
			resetNewFlags(newAgentCmd)
			return runNewAgent(newAgentCmd, []string{"com.acme.shop", "support", `Support "Team"`})
		}, want: "double quote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentCommandRoot = func() (string, error) { return "/repo", nil }
			agentCommandFS = &fsx.MockFileSystem{Files: map[string][]byte{}}
			err := tt.call()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNewCommandPreflightRefusals(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "agent description quote", run: func() error {
			resetNewFlags(newAgentCmd)
			_ = newAgentCmd.Flags().Set("desc", `has "quote"`)
			return runNewAgent(newAgentCmd, []string{"com.acme.shop", "support", "Support"})
		}, want: "double quote"},
		{name: "agent description too long", run: func() error {
			resetNewFlags(newAgentCmd)
			_ = newAgentCmd.Flags().Set("desc", strings.Repeat("x", 1001))
			return runNewAgent(newAgentCmd, []string{"com.acme.shop", "support", "Support"})
		}, want: "1000 characters"},
		{name: "task plural quote", run: func() error {
			resetNewFlags(newTaskTypeCmd)
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "returns", `Return "Orders"`})
		}, want: "double quote"},
		{name: "task plural length", run: func() error {
			resetNewFlags(newTaskTypeCmd)
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "returns", strings.Repeat("x", 101)})
		}, want: "100 characters"},
		{name: "task description line break", run: func() error {
			resetNewFlags(newTaskTypeCmd)
			_ = newTaskTypeCmd.Flags().Set("desc", "first\nsecond")
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "returns", "Returns"})
		}, want: "single line"},
		{name: "skill title limit", run: func() error {
			resetNewFlags(newSkillCmd)
			_ = newSkillCmd.Flags().Set("task-type", "returns")
			return runNewSkill(newSkillCmd, []string{"com.acme.shop", "skill-one", strings.Repeat("x", 256)})
		}, want: "255 characters"},
		{name: "skill description quote", run: func() error {
			resetNewFlags(newSkillCmd)
			_ = newSkillCmd.Flags().Set("task-type", "returns")
			_ = newSkillCmd.Flags().Set("desc", `has "quote"`)
			return runNewSkill(newSkillCmd, []string{"com.acme.shop", "skill-one", "Skill One"})
		}, want: "double quote"},
		{name: "skill task type syntax", run: func() error {
			resetNewFlags(newSkillCmd)
			_ = newSkillCmd.Flags().Set("task-type", "Bad_Type")
			return runNewSkill(newSkillCmd, []string{"com.acme.shop", "skill-one"})
		}, want: "invalid task-type name"},
		{name: "tool display quote", run: func() error {
			resetNewFlags(newToolCmd)
			_ = newToolCmd.Flags().Set("task-type", "returns")
			return runNewTool(newToolCmd, []string{"com.acme.shop", "process-data", `Process "Data"`})
		}, want: "double quote"},
		{name: "tool display length", run: func() error {
			resetNewFlags(newToolCmd)
			_ = newToolCmd.Flags().Set("task-type", "returns")
			return runNewTool(newToolCmd, []string{"com.acme.shop", "process-data", strings.Repeat("x", 101)})
		}, want: "100 characters"},
		{name: "tool description length", run: func() error {
			resetNewFlags(newToolCmd)
			_ = newToolCmd.Flags().Set("task-type", "returns")
			_ = newToolCmd.Flags().Set("desc", strings.Repeat("x", 1001))
			return runNewTool(newToolCmd, []string{"com.acme.shop", "process-data"})
		}, want: "1000 characters"},
		{name: "tool task type syntax", run: func() error {
			resetNewFlags(newToolCmd)
			_ = newToolCmd.Flags().Set("task-type", "Bad_Type")
			return runNewTool(newToolCmd, []string{"com.acme.shop", "process-data"})
		}, want: "invalid task-type name"},
		{name: "task tools empty entry", run: func() error {
			resetNewFlags(newTaskTypeCmd)
			_ = newTaskTypeCmd.Flags().Set("tools", "process-data,")
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "returns", "Returns"})
		}, want: "empty name"},
		{name: "task explicitly empty duration", run: func() error {
			resetNewFlags(newTaskTypeCmd)
			_ = newTaskTypeCmd.Flags().Set("every", "")
			return runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "returns", "Returns"})
		}, want: "whole-second duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldRoot, oldFS := agentCommandRoot, agentCommandFS
			t.Cleanup(func() { agentCommandRoot, agentCommandFS = oldRoot, oldFS })
			agentCommandRoot = func() (string, error) { return "/repo", nil }
			agentCommandFS = &fsx.MockFileSystem{}
			err := tt.run()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNewCommandRootAndWiringErrors(t *testing.T) {
	oldRoot, oldFS := agentCommandRoot, agentCommandFS
	oldAgent, oldTask, oldSkill, oldTool := createAgentWiring, createTaskTypeWiring, createSkillWiring, createToolWiring
	t.Cleanup(func() {
		agentCommandRoot, agentCommandFS = oldRoot, oldFS
		createAgentWiring, createTaskTypeWiring, createSkillWiring, createToolWiring = oldAgent, oldTask, oldSkill, oldTool
	})
	agentCommandFS = &fsx.MockFileSystem{}
	t.Run("root lookup error", func(t *testing.T) {
		resetNewFlags(newAgentCmd)
		agentCommandRoot = func() (string, error) { return "", errors.New("cwd failed") }
		if err := runNewAgent(newAgentCmd, []string{"com.acme.shop", "support", "Support"}); err == nil || !strings.Contains(err.Error(), "cwd failed") {
			t.Fatalf("error = %v", err)
		}
	})
	for _, tt := range []struct {
		name string
		cmd  *cobra.Command
		args []string
		run  func(*cobra.Command, []string) error
		set  func(*cobra.Command)
	}{
		{name: "task type root error", cmd: newTaskTypeCmd, args: []string{"com.acme.shop", "returns", "Returns"}, run: runNewTaskType},
		{name: "skill root error", cmd: newSkillCmd, args: []string{"com.acme.shop", "skill-one"}, run: runNewSkill, set: func(cmd *cobra.Command) { _ = cmd.Flags().Set("task-type", "returns") }},
		{name: "tool root error", cmd: newToolCmd, args: []string{"com.acme.shop", "process-data"}, run: runNewTool, set: func(cmd *cobra.Command) { _ = cmd.Flags().Set("task-type", "returns") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetNewFlags(tt.cmd)
			if tt.set != nil {
				tt.set(tt.cmd)
			}
			agentCommandRoot = func() (string, error) { return "", errors.New("cwd failed") }
			if err := tt.run(tt.cmd, tt.args); err == nil || !strings.Contains(err.Error(), "cwd failed") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	t.Run("agent wiring error", func(t *testing.T) {
		resetNewFlags(newAgentCmd)
		agentCommandRoot = func() (string, error) { return "/repo", nil }
		createAgentWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.AgentConfig) (scaffold.WiringResult, error) {
			return scaffold.WiringResult{}, errors.New("agent write refused")
		}
		if err := runNewAgent(newAgentCmd, []string{"com.acme.shop", "support", "Support"}); err == nil || !strings.Contains(err.Error(), "agent write refused") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("task type wiring error", func(t *testing.T) {
		resetNewFlags(newTaskTypeCmd)
		agentCommandRoot = func() (string, error) { return "/repo", nil }
		createTaskTypeWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.TaskTypeConfig) (scaffold.WiringResult, error) {
			return scaffold.WiringResult{}, errors.New("task write refused")
		}
		if err := runNewTaskType(newTaskTypeCmd, []string{"com.acme.shop", "returns", "Returns"}); err == nil || !strings.Contains(err.Error(), "task write refused") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("skill wiring error", func(t *testing.T) {
		resetNewFlags(newSkillCmd)
		_ = newSkillCmd.Flags().Set("task-type", "returns")
		agentCommandRoot = func() (string, error) { return "/repo", nil }
		createSkillWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.SkillConfig) (scaffold.WiringResult, error) {
			return scaffold.WiringResult{}, errors.New("skill write refused")
		}
		if err := runNewSkill(newSkillCmd, []string{"com.acme.shop", "skill-one"}); err == nil || !strings.Contains(err.Error(), "skill write refused") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("tool wiring error", func(t *testing.T) {
		resetNewFlags(newToolCmd)
		_ = newToolCmd.Flags().Set("task-type", "returns")
		agentCommandRoot = func() (string, error) { return "/repo", nil }
		createToolWiring = func(fsx.FileSystem, fsx.TemplateFS, string, scaffold.ToolConfig) (scaffold.WiringResult, error) {
			return scaffold.WiringResult{}, errors.New("tool write refused")
		}
		if err := runNewTool(newToolCmd, []string{"com.acme.shop", "process-data"}); err == nil || !strings.Contains(err.Error(), "tool write refused") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestPrintNewResultEmptyAndWriteErrors(t *testing.T) {
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	result := scaffold.WiringResult{Created: nil, Records: nil, FillIn: nil, NothingToAdd: true}
	t.Run("text no-op", func(t *testing.T) {
		jsonOutput = false
		var output bytes.Buffer
		newSkillCmd.SetOut(&output)
		if err := printNewResult(newSkillCmd, "com.acme.shop", "refund-policy", "skill", result); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Nothing to add", "Nothing to fill in.", "Run `simple build com.acme.shop`"} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("output %q does not contain %q", output.String(), want)
			}
		}
	})
	t.Run("json uses empty lists", func(t *testing.T) {
		jsonOutput = true
		var output bytes.Buffer
		newSkillCmd.SetOut(&output)
		if err := printNewResult(newSkillCmd, "com.acme.shop", "refund-policy", "skill", result); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "null") {
			t.Fatalf("empty result arrays encoded as null: %s", output.String())
		}
	})
	t.Run("text writer error", func(t *testing.T) {
		jsonOutput = false
		newSkillCmd.SetOut(errorWriter{})
		if err := printNewResult(newSkillCmd, "com.acme.shop", "refund-policy", "skill", result); err == nil {
			t.Fatal("expected output write error")
		}
	})
	t.Run("json writer error", func(t *testing.T) {
		jsonOutput = true
		newSkillCmd.SetOut(errorWriter{})
		if err := printNewResult(newSkillCmd, "com.acme.shop", "refund-policy", "skill", result); err == nil {
			t.Fatal("expected JSON output error")
		}
	})
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

var _ io.Writer = errorWriter{}

func TestParseNewWakeInterval(t *testing.T) {
	tests := []struct {
		value string
		want  int64
		bad   bool
	}{
		{value: "90s", want: 90}, {value: "30m", want: 1800}, {value: "1h", want: 3600},
		{value: "1s", want: 1}, {value: "0s", bad: true}, {value: "500ms", bad: true},
		{value: "1.5s", bad: true}, {value: "-1s", bad: true}, {value: "soon", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseNewWakeInterval(tt.value)
			if tt.bad {
				if err == nil {
					t.Fatalf("parseNewWakeInterval(%q) = %d, nil", tt.value, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseNewWakeInterval(%q) = %d, %v; want %d", tt.value, got, err, tt.want)
			}
		})
	}
}

func TestParseToolNames(t *testing.T) {
	tests := []struct {
		value string
		want  []string
		bad   bool
	}{
		{value: "", want: []string{}},
		{value: "process-data, find-order", want: []string{"process-data", "find-order"}},
		{value: "process-data,process-data", want: []string{"process-data"}},
		{value: "process-data,", bad: true},
		{value: "Bad_Name", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseToolNames(tt.value)
			if tt.bad {
				if err == nil {
					t.Fatalf("parseToolNames(%q) = %#v, nil", tt.value, got)
				}
				return
			}
			if err != nil || strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("parseToolNames(%q) = %#v, %v; want %#v", tt.value, got, err, tt.want)
			}
		})
	}
}

func resetNewFlags(cmd *cobra.Command) {
	_ = cmd.Flags().Set("desc", "")
	switch cmd {
	case newTaskTypeCmd:
		_ = cmd.Flags().Set("agent", "")
		_ = cmd.Flags().Set("tools", "")
		_ = cmd.Flags().Set("every", "")
	case newSkillCmd:
		_ = cmd.Flags().Set("task-type", "")
	case newToolCmd:
		_ = cmd.Flags().Set("task-type", "")
		_ = cmd.Flags().Set("lang", "ts")
		_ = cmd.Flags().Set("scope", "")
		_ = cmd.Flags().Set("env", "server")
	}
	cmd.Flags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
}

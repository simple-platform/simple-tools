package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"simple-cli/internal/agents"
	"simple-cli/internal/agents/structure"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
)

const wiringRoot = "/repo"
const wiringAppID = "com.acme.shop"

func wiringFS() *fsx.MockFileSystem {
	return &fsx.MockFileSystem{Files: map[string][]byte{
		filepath.Join(wiringRoot, "apps", wiringAppID): nil,
	}}
}

func withWiringApp(t *testing.T, app *agents.App) {
	t.Helper()
	original := loadAgentApp
	loadAgentApp = func(_ fsx.FileSystem, _, _ string) (*agents.App, error) { return app, nil }
	t.Cleanup(func() { loadAgentApp = original })
}

func baseWiringApp() *agents.App {
	persona := &agents.Knowledge{Key: "persona_support", Body: agents.Body{Inline: "# Support Agent\n\n## Role\n\nRole text.\n\n## Communication Style\n\nStyle text.\n", Readable: true}}
	agent := &agents.Agent{Key: "agent_support", DisplayName: "Support Agent", Persona: persona}
	return &agents.App{ID: wiringAppID, Dir: filepath.Join(wiringRoot, "apps", wiringAppID), AgentRecords: []*agents.Agent{agent}, Knowledge: []*agents.Knowledge{persona}}
}

func TestCreateAgent(t *testing.T) {
	tests := []struct {
		name    string
		app     func() *agents.App
		cfg     AgentConfig
		wantErr string
	}{
		{name: "writes persona and records", app: baseWiringApp, cfg: AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing Agent", Description: "Answers billing questions."}},
		{name: "existing agent refuses", app: baseWiringApp, cfg: AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support"}, wantErr: "already exists"},
		{name: "invalid name refuses", app: baseWiringApp, cfg: AgentConfig{AppID: wiringAppID, Name: "Bad_Name", DisplayName: "Name"}, wantErr: "invalid agent name"},
		{name: "quoted text refuses", app: baseWiringApp, cfg: AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: `Billing "Team"`}, wantErr: "double quote"},
		{name: "long name refuses", app: baseWiringApp, cfg: AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: strings.Repeat("a", 101)}, wantErr: "100 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := wiringFS()
			withWiringApp(t, tt.app())
			got, err := CreateAgent(fsys, TemplatesFS, wiringRoot, tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CreateAgent() error = %v, want %q", err, tt.wantErr)
				}
				if len(fsys.Files) != 1 {
					t.Fatalf("refusal wrote files: %#v", fsys.Files)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateAgent() error = %v", err)
			}
			if len(got.Created) != 3 || len(got.Records) != 2 || len(got.FillIn) != 1 {
				t.Fatalf("unexpected result: %#v", got)
			}
			persona := fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "billing-persona.md")]
			if !strings.HasPrefix(string(persona), "# Billing Agent\n") || strings.Count(string(persona), agents.Marker) != 2 {
				t.Fatalf("persona template = %q", persona)
			}
		})
	}
}

func TestRenderedAgentTemplates(t *testing.T) {
	persona, err := renderAgentTemplate(TemplatesFS, "templates/ai-agent/persona.md", map[string]string{"DisplayName": "Support Agent"})
	if err != nil {
		t.Fatal(err)
	}
	playbook, err := renderAgentTemplate(TemplatesFS, "templates/ai-agent/playbook.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	personaFindings, playbookFindings := structure.CheckInstructions("Support Agent", persona, playbook)
	if len(personaFindings) != 0 || len(playbookFindings) != 0 {
		t.Fatalf("instruction findings: persona=%#v playbook=%#v", personaFindings, playbookFindings)
	}
	if got := strings.Count(persona, agents.Marker); got != 2 {
		t.Fatalf("persona marker count = %d, want 2", got)
	}
	if got := strings.Count(playbook, agents.Marker); got != 5 {
		t.Fatalf("playbook marker count = %d, want 5", got)
	}
	for _, tt := range []struct {
		name string
		path string
		data map[string]string
	}{
		{name: "skill", path: "templates/ai-agent/skill.md", data: map[string]string{"Title": "Refund Policy", "Name": "refund-policy", "Purpose": "TODO(simple): Say in one paragraph what this skill helps the agent do, and why it matters."}},
		{name: "tool lesson", path: "templates/ai-agent/tool-lesson.md", data: map[string]string{"DisplayName": "Find Order", "Name": "find-order", "Purpose": "TODO(simple): Say in one paragraph what this tool does and which fact or change it gives the agent."}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text, renderErr := renderAgentTemplate(TemplatesFS, tt.path, tt.data)
			if renderErr != nil {
				t.Fatal(renderErr)
			}
			if findings := structure.CheckSkill(text); len(findings) != 0 {
				t.Fatalf("structure findings: %#v", findings)
			}
			if got := strings.Count(text, agents.Marker); got != 8 {
				t.Fatalf("marker count = %d, want 8", got)
			}
		})
	}
}

func TestCreateAgentAppAndLoadRefusals(t *testing.T) {
	tests := []struct {
		name    string
		fsys    *fsx.MockFileSystem
		loadErr error
		want    string
	}{
		{name: "missing app", fsys: &fsx.MockFileSystem{Files: map[string][]byte{}}, want: "does not exist"},
		{name: "load fails", fsys: wiringFS(), loadErr: errors.New("parse failed"), want: "parse failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := loadAgentApp
			loadAgentApp = func(_ fsx.FileSystem, _, _ string) (*agents.App, error) { return nil, tt.loadErr }
			t.Cleanup(func() { loadAgentApp = original })
			before := len(tt.fsys.Files)
			_, err := CreateAgent(tt.fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CreateAgent() error = %v, want %q", err, tt.want)
			}
			if len(tt.fsys.Files) != before {
				t.Fatal("refusal changed the in-memory filesystem")
			}
		})
	}
}

func TestAgentTemplateAndRecordRefusals(t *testing.T) {
	tests := []struct {
		name string
		tpl  fsx.TemplateFS
		fsys *fsx.MockFileSystem
		want string
	}{
		{name: "persona template read error", tpl: &fsx.MockTemplateFS{ReadErrors: map[string]error{"templates/ai-agent/persona.md": errors.New("template read failed")}}, fsys: wiringFS(), want: "template read failed"},
		{name: "persona template parse error", tpl: &fsx.MockTemplateFS{Files: map[string][]byte{"templates/ai-agent/persona.md": []byte("{{")}}, fsys: wiringFS(), want: "failed to parse template"},
		{name: "persona template execute error", tpl: &fsx.MockTemplateFS{Files: map[string][]byte{"templates/ai-agent/persona.md": []byte(`{{index . "DisplayName" "missing"}}`)}}, fsys: wiringFS(), want: "failed to render template"},
		{name: "existing record file read error", tpl: TemplatesFS, fsys: &fsx.MockFileSystem{Files: map[string][]byte{
			filepath.Join(wiringRoot, "apps", wiringAppID):                                      nil,
			filepath.Join(wiringRoot, "apps", wiringAppID, "records", "70_agent_knowledge.scl"): []byte("existing"),
		}, ReadFileErr: os.ErrPermission}, want: "failed to read existing records file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withWiringApp(t, baseWiringApp())
			before := len(tt.fsys.Files)
			_, err := CreateAgent(tt.fsys, tt.tpl, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CreateAgent() error = %v, want %q", err, tt.want)
			}
			if len(tt.fsys.Files) != before {
				t.Fatal("refusal changed files")
			}
		})
	}
}

func TestRecordAppendPreservesExistingFiles(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantHead string
	}{
		{name: "without final newline", content: "existing record", wantHead: "existing record\n\n"},
		{name: "with final newline", content: "existing record\n", wantHead: "existing record\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := wiringFS()
			path := filepath.Join(wiringRoot, "apps", wiringAppID, "records", "70_agent_knowledge.scl")
			fsys.Files[path] = []byte(tt.content)
			withWiringApp(t, baseWiringApp())
			result, err := CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(fsys.Files[path]), tt.wantHead) {
				t.Fatalf("existing file was not appended after a blank line: %q", fsys.Files[path])
			}
			for _, created := range result.Created {
				if created == "apps/com.acme.shop/records/70_agent_knowledge.scl" {
					t.Fatal("an existing records file was reported as created")
				}
			}
		})
	}
}

func TestAgentValidationAndFilesystemFailures(t *testing.T) {
	t.Run("agent directory write failure", func(t *testing.T) {
		fsys := wiringFS()
		fsys.MkdirAllErr = os.ErrPermission
		withWiringApp(t, baseWiringApp())
		_, err := CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing"})
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("CreateAgent() error = %v", err)
		}
	})
	t.Run("record file write failure", func(t *testing.T) {
		fsys := wiringFS()
		fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "billing-persona.md")] = []byte("existing")
		fsys.WriteFileErr = os.ErrPermission
		withWiringApp(t, baseWiringApp())
		_, err := CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing"})
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("CreateAgent() error = %v", err)
		}
	})
	t.Run("text limits", func(t *testing.T) {
		tests := []struct {
			cfg  TaskTypeConfig
			want string
		}{
			{cfg: TaskTypeConfig{Name: strings.Repeat("a", 101), PluralName: "Returns"}, want: "task-type name is longer"},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: strings.Repeat("a", 101)}, want: "plural name is longer"},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: "Returns", Description: strings.Repeat("a", 1001)}, want: "description is longer"},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: "Returns", WakeIntervalSeconds: -1}, want: "at least one whole second"},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: "Returns", Tools: []string{"Bad_Name"}}, want: "invalid tool name"},
		}
		for _, tt := range tests {
			if err := validateTaskTypeConfig(tt.cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("validateTaskTypeConfig() error = %v, want %q", err, tt.want)
			}
		}
		if err := validateSkillConfig(SkillConfig{Name: "skill-one", Title: strings.Repeat("x", 256)}); err == nil {
			t.Error("expected title length error")
		}
		if err := validateSkillConfig(SkillConfig{Name: "skill-one"}); err != nil {
			t.Errorf("empty optional title should be valid for an existing skill: %v", err)
		}
		if err := validateToolConfig(ToolConfig{Name: "tool-one", DisplayName: strings.Repeat("x", 101)}); err == nil {
			t.Error("expected display name length error")
		}
		if err := validateToolConfig(ToolConfig{Name: "tool-one"}); err != nil {
			t.Errorf("empty optional display name should be valid for an existing tool: %v", err)
		}
		if err := validateActionConfigForTool(ToolConfig{Name: "tool-one"}); err == nil {
			t.Error("expected missing display name error")
		}
		if err := validateScaffoldText("description", strings.Repeat("x", 1001), 1000); err == nil {
			t.Error("expected text length error")
		}
	})
}

func TestCreateTaskType(t *testing.T) {
	tests := []struct {
		name    string
		app     *agents.App
		cfg     TaskTypeConfig
		wantErr string
	}{
		{name: "creates complete task type with platform lookup", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", Tools: []string{"process-data"}}},
		{name: "one agent may be inferred", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}},
		{name: "no agents", app: &agents.App{ID: wiringAppID}, cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}, wantErr: "has no agent yet"},
		{name: "multiple agents need selection", app: appWithTwoAgents(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}, wantErr: "say which with --agent"},
		{name: "named unknown agent", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "billing"}, wantErr: "does not exist"},
		{name: "selected agent without persona", app: appWithoutPersona(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support"}, wantErr: "no readable persona"},
		{name: "unknown tool", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", Tools: []string{"missing-tool"}}, wantErr: "simple new tool"},
		{name: "bad name", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "Bad_Name", PluralName: "Returns", AgentName: "support"}, wantErr: "invalid task-type name"},
		{name: "duplicate task type", app: appWithTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}, wantErr: "simple new skill"},
		{name: "bad persona structure", app: appWithPersona("# Not Support\n\n## Role\n\nRole.\n\n## Communication Style\n\nStyle.\n"), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support"}, wantErr: "make the first heading match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := wiringFS()
			withWiringApp(t, tt.app)
			got, err := CreateTaskType(fsys, TemplatesFS, wiringRoot, tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CreateTaskType() error = %v, want %q", err, tt.wantErr)
				}
				if len(fsys.Files) != 1 {
					t.Fatal("refusal wrote files")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateTaskType() error = %v", err)
			}
			if len(got.FillIn) != 3 { // playbook, input shape, persona is carried but reused
				if len(got.FillIn) != 2 {
					t.Fatalf("unexpected marker paths: %#v", got.FillIn)
				}
			}
			links := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")])
			if tt.cfg.Tools != nil && (!strings.Contains(links, "var platform_tool_process_data") || !strings.Contains(links, "platform_tool_process_data")) {
				t.Fatalf("platform lookup/link missing: %s", links)
			}
		})
	}
}

func TestCreateTaskTypeReusesExistingFiles(t *testing.T) {
	app := baseWiringApp()
	app.TaskTypes = []*agents.TaskType{{Key: "kind_intake", Name: "intake", StatesPath: "records/task-types/shared-states.json"}}
	withWiringApp(t, app)
	fsys := wiringFS()
	appDir := filepath.Join(wiringRoot, "apps", wiringAppID)
	paths := []string{
		filepath.Join(appDir, "knowledge", "returns-playbook.md"),
		filepath.Join(appDir, "records", "task-types", "returns.json"),
		filepath.Join(appDir, "records", "task-types", "shared-states.json"),
		filepath.Join(appDir, "knowledge", "support-persona.md"),
	}
	for _, path := range paths {
		fsys.Files[path] = []byte("kept")
	}
	app.AgentRecords[0].Persona.Body.Path = "knowledge/support-persona.md"
	app.AgentRecords[0].Persona.Body.Inline = ""
	fsys.Files[paths[3]] = []byte("# Support Agent\n\n## Role\n\nRole.\n\n## Communication Style\n\nStyle.\n")
	got, err := CreateTaskType(fsys, TemplatesFS, wiringRoot, TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", WakeIntervalSeconds: 90})
	if err != nil {
		t.Fatalf("CreateTaskType() error = %v", err)
	}
	if len(got.FillIn) != 0 || len(got.Created) != 3 {
		t.Fatalf("unexpected reuse result: %#v", got)
	}
	if string(fsys.Files[paths[0]]) != "kept" || string(fsys.Files[paths[1]]) != "kept" || string(fsys.Files[paths[2]]) != "kept" {
		t.Fatal("an existing playbook, shape, or states file was changed")
	}
	taskRecord := string(fsys.Files[filepath.Join(appDir, "records", "71_agents.scl")])
	if !strings.Contains(taskRecord, "wake_interval_seconds 90") || !strings.Contains(taskRecord, "shared-states.json") {
		t.Fatalf("task type did not keep the existing state path/interval: %s", taskRecord)
	}
}

func TestCreateTaskTypeTemplateAndRecordRefusals(t *testing.T) {
	for _, path := range []string{
		"templates/ai-agent/playbook.md", "templates/ai-agent/task-type.json", "templates/ai-agent/states.json",
		"templates/ai-agent/knowledge.scl", "templates/ai-agent/task-type.scl", "templates/ai-agent/agent_task_type.scl",
		"templates/ai-agent/task_type_skill.scl",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fsys := wiringFS()
			withWiringApp(t, baseWiringApp())
			tpl := failingTemplateFS{TemplateFS: TemplatesFS, path: path, err: errors.New("template unavailable")}
			_, err := CreateTaskType(fsys, tpl, wiringRoot, TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"})
			if err == nil || !strings.Contains(err.Error(), "template unavailable") {
				t.Fatalf("CreateTaskType() error = %v", err)
			}
			if len(fsys.Files) != 1 {
				t.Fatal("template refusal wrote files")
			}
		})
	}
	t.Run("records read error", func(t *testing.T) {
		fsys := wiringFS()
		fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "70_agent_knowledge.scl")] = []byte("kept")
		fsys.ReadFileErr = os.ErrPermission
		withWiringApp(t, baseWiringApp())
		_, err := CreateTaskType(fsys, TemplatesFS, wiringRoot, TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"})
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("CreateTaskType() error = %v", err)
		}
	})
}

type failingTemplateFS struct {
	fsx.TemplateFS
	path string
	err  error
}

func (f failingTemplateFS) ReadFile(name string) ([]byte, error) {
	if name == f.path {
		return nil, f.err
	}
	return f.TemplateFS.ReadFile(name)
}

func TestCreateSkill(t *testing.T) {
	tests := []struct {
		name    string
		app     *agents.App
		cfg     SkillConfig
		wantErr string
	}{
		{name: "creates skill", app: appWithTask(), cfg: SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", TaskTypeName: "returns"}},
		{name: "description replaces purpose marker", app: appWithTask(), cfg: SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", Description: "Apply the approved refund rules.", TaskTypeName: "returns"}},
		{name: "missing title for new skill", app: appWithTask(), cfg: SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"}, wantErr: "title is required"},
		{name: "missing task type", app: baseWiringApp(), cfg: SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", TaskTypeName: "returns"}, wantErr: "does not exist"},
		{name: "invalid skill name", app: appWithTask(), cfg: SkillConfig{AppID: wiringAppID, Name: "Refund_Policy", Title: "Refund Policy", TaskTypeName: "returns"}, wantErr: "invalid skill name"},
		{name: "existing skill adds link only", app: appWithSkill(false), cfg: SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"}},
		{name: "existing skill and link is a no-op", app: appWithSkill(true), cfg: SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := wiringFS()
			withWiringApp(t, tt.app)
			got, err := CreateSkill(fsys, TemplatesFS, wiringRoot, tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CreateSkill() error = %v, want %q", err, tt.wantErr)
				}
				if len(fsys.Files) != 1 {
					t.Fatal("refusal wrote files")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateSkill() error = %v", err)
			}
			if tt.name == "existing skill and link is a no-op" {
				if !got.NothingToAdd || len(fsys.Files) != 1 {
					t.Fatalf("expected no-op, got %#v", got)
				}
				return
			}
			if tt.name == "existing skill adds link only" && len(got.Created) != 1 {
				t.Fatalf("existing skill should only create records: %#v", got)
			}
			if strings.Contains(tt.name, "creates skill") {
				content := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "skill-refund-policy.md")])
				if strings.Count(content, agents.Marker) != 8 && tt.cfg.Description == "" {
					t.Fatalf("unexpected skill template marker count %d", strings.Count(content, agents.Marker))
				}
				if tt.cfg.Description != "" && !strings.Contains(content, tt.cfg.Description) {
					t.Fatalf("purpose description missing: %s", content)
				}
			}
		})
	}
}

func TestCreateSkillReuseAndRefusals(t *testing.T) {
	t.Run("existing Markdown file is reused", func(t *testing.T) {
		fsys := wiringFS()
		path := filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "skill-refund-policy.md")
		fsys.Files[path] = []byte("hand-written content")
		withWiringApp(t, appWithTask())
		result, err := CreateSkill(fsys, TemplatesFS, wiringRoot, SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", TaskTypeName: "returns"})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.FillIn) != 0 || string(fsys.Files[path]) != "hand-written content" {
			t.Fatalf("existing skill file was changed or reported: %#v", result)
		}
	})
	t.Run("existing skill file is reused when adding link", func(t *testing.T) {
		fsys := wiringFS()
		path := filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "skill-refund-policy.md")
		fsys.Files[path] = []byte("hand-written content")
		withWiringApp(t, appWithSkill(false))
		result, err := CreateSkill(fsys, TemplatesFS, wiringRoot, SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Created) != 1 || len(result.FillIn) != 0 || string(fsys.Files[path]) != "hand-written content" {
			t.Fatalf("existing skill was not reused: %#v", result)
		}
	})
	t.Run("template refusals leave disk untouched", func(t *testing.T) {
		for _, path := range []string{"templates/ai-agent/skill.md", "templates/ai-agent/knowledge.scl", "templates/ai-agent/task_type_skill.scl"} {
			fsys := wiringFS()
			withWiringApp(t, appWithTask())
			tpl := failingTemplateFS{TemplateFS: TemplatesFS, path: path, err: errors.New("template unavailable")}
			_, err := CreateSkill(fsys, tpl, wiringRoot, SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", TaskTypeName: "returns"})
			if err == nil || !strings.Contains(err.Error(), "template unavailable") {
				t.Errorf("template %s error = %v", path, err)
			}
			if len(fsys.Files) != 1 {
				t.Errorf("template %s refusal wrote files", path)
			}
		}
	})
	t.Run("existing records file read refusal", func(t *testing.T) {
		fsys := wiringFS()
		fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "70_agent_knowledge.scl")] = []byte("kept")
		fsys.ReadFileErr = os.ErrPermission
		withWiringApp(t, appWithTask())
		_, err := CreateSkill(fsys, TemplatesFS, wiringRoot, SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", TaskTypeName: "returns"})
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("CreateSkill() error = %v", err)
		}
	})
	t.Run("invalid description", func(t *testing.T) {
		if err := validateSkillConfig(SkillConfig{Name: "skill-one", Description: "two\nlines"}); err == nil {
			t.Fatal("expected skill description line refusal")
		}
	})
}

func TestCreateTool(t *testing.T) {
	base := appWithTask()
	withAction := appWithAction(false, false)
	withExistingLesson := appWithAction(true, true)
	tests := []struct {
		name    string
		app     *agents.App
		cfg     ToolConfig
		wantErr string
	}{
		{name: "platform tool", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"}},
		{name: "existing action gets lesson", app: withAction, cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}},
		{name: "existing lesson gets missing tool link", app: appWithAction(true, false), cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}},
		{name: "existing lesson and links are a no-op", app: withExistingLesson, cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}},
		{name: "new action is marked tool", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", Description: "Find a customer.", TaskTypeName: "returns", Language: LanguageTypeScript, Scope: "acme", ExecutionEnv: "server"}},
		{name: "new Rust action is marked tool", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "summarize-order", DisplayName: "Summarize Order", TaskTypeName: "returns", Language: LanguageRust, ExecutionEnv: "server"}},
		{name: "new action missing display name", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", TaskTypeName: "returns"}, wantErr: "neither a platform tool nor an action"},
		{name: "new TypeScript tool missing scope", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns"}, wantErr: "--scope is required"},
		{name: "existing action path without record refuses", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: LanguageRust}, wantErr: "action path"},
		{name: "missing kind of work", app: baseWiringApp(), cfg: ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"}, wantErr: "does not exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := wiringFS()
			if tt.name == "existing action path without record refuses" {
				fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "actions", tt.cfg.Name)] = nil
			}
			before := len(fsys.Files)
			withWiringApp(t, tt.app)
			got, err := CreateTool(fsys, TemplatesFS, wiringRoot, tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CreateTool() error = %v, want %q", err, tt.wantErr)
				}
				if len(fsys.Files) != before {
					t.Fatal("refusal wrote files")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateTool() error = %v", err)
			}
			switch tt.name {
			case "platform tool":
				links := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")])
				if !strings.Contains(links, "var platform_tool_process_data") || !strings.Contains(links, "task_type_returns_may_call_process_data") {
					t.Fatalf("platform tool records = %s", links)
				}
			case "existing action gets lesson":
				if len(got.FillIn) != 1 || !strings.HasSuffix(got.FillIn[0], "knowledge/tool-find-order.md") {
					t.Fatalf("lesson marker paths = %#v", got.FillIn)
				}
			case "existing lesson and links are a no-op":
				if !got.NothingToAdd {
					t.Fatalf("expected no-op, got %#v", got)
				}
			case "new action is marked tool":
				source := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "actions", "lookup-customer", "src", "index.ts")])
				if !strings.Contains(source, "@tool") || !strings.Contains(source, agents.Marker) {
					t.Fatalf("tool source missing marker: %s", source)
				}
				if len(got.Records) != 4 {
					t.Fatalf("unexpected written records: %#v", got.Records)
				}
			case "new Rust action is marked tool":
				source := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "actions", "summarize-order", "src", "main.rs")])
				if !strings.Contains(source, "/// @tool") || !strings.Contains(source, "/// @shortdesc TODO(simple):") {
					t.Fatalf("Rust tool source missing tool tags: %s", source)
				}
			}
		})
	}
}

func TestActionToolTemplateMarkers(t *testing.T) {
	tests := []struct {
		name       string
		language   string
		actionName string
		tool       bool
		wantMarker bool
	}{
		{name: "TypeScript action stays unmarked", language: LanguageTypeScript, actionName: "plain-ts"},
		{name: "TypeScript tool is marked", language: LanguageTypeScript, actionName: "marked-ts", tool: true, wantMarker: true},
		{name: "Rust action keeps its existing short description", language: LanguageRust, actionName: "plain-rust"},
		{name: "Rust tool short description is marked", language: LanguageRust, actionName: "marked-rust", tool: true, wantMarker: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := wiringFS()
			cfg := ActionConfig{
				AppID: wiringAppID, ActionName: tt.actionName, DisplayName: "Example Tool",
				Scope: "acme", ExecutionEnv: "server", Language: tt.language, Tool: tt.tool,
			}
			if err := CreateActionStructure(fsys, TemplatesFS, wiringRoot, cfg); err != nil {
				t.Fatalf("CreateActionStructure() error = %v", err)
			}
			sourceName := "index.ts"
			if tt.language == LanguageRust {
				sourceName = "main.rs"
			}
			sourcePath := filepath.Join(wiringRoot, "apps", wiringAppID, "actions", tt.actionName, "src", sourceName)
			source := string(fsys.Files[sourcePath])
			gotMarker := strings.Contains(source, "TODO(simple): Say in one line what this tool gives an agent.")
			if gotMarker != tt.wantMarker {
				t.Fatalf("tool short-description marker presence = %v, want %v", gotMarker, tt.wantMarker)
			}
			if tt.language == LanguageTypeScript && strings.Contains(source, "@tool") != tt.tool {
				t.Fatalf("TypeScript @tool marker presence does not match Tool=%v", tt.tool)
			}
			if tt.language == LanguageRust && !strings.Contains(source, "@shortdesc Greet whoever the caller names, and answer with the greeting.") && !tt.tool {
				t.Fatal("ordinary Rust action short description changed")
			}
		})
	}
}

func TestCreateToolTemplateAndValidationRefusals(t *testing.T) {
	base := appWithTask()
	for _, tt := range []struct {
		name string
		app  *agents.App
		cfg  ToolConfig
		tpl  fsx.TemplateFS
		fsys *fsx.MockFileSystem
		want string
	}{
		{name: "bad name", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "Bad_Tool", TaskTypeName: "returns"}, want: "invalid tool name"},
		{name: "description quote", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "process-data", Description: `a "quote"`, TaskTypeName: "returns"}, want: "double quote"},
		{name: "unsupported action language", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: "go"}, want: "unsupported action language"},
		{name: "invalid action environment", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: LanguageRust, ExecutionEnv: "edge"}, want: "invalid execution environment"},
		{name: "platform lookup template", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"}, tpl: failingTemplateFS{TemplateFS: TemplatesFS, path: "templates/ai-agent/platform_tool_lookup.scl", err: errors.New("lookup template failed")}, want: "lookup template failed"},
		{name: "tool link template", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"}, tpl: failingTemplateFS{TemplateFS: TemplatesFS, path: "templates/ai-agent/task_type_tool.scl", err: errors.New("tool link template failed")}, want: "tool link template failed"},
		{name: "new action record template", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: LanguageRust}, tpl: failingTemplateFS{TemplateFS: TemplatesFS, path: "templates/action/10_actions.scl", err: errors.New("action record template failed")}, want: "action record template failed"},
		{name: "lesson template", app: appWithAction(false, false), cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}, tpl: failingTemplateFS{TemplateFS: TemplatesFS, path: "templates/ai-agent/tool-lesson.md", err: errors.New("lesson template failed")}, want: "lesson template failed"},
		{name: "lesson knowledge template", app: appWithAction(false, false), cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}, tpl: failingTemplateFS{TemplateFS: TemplatesFS, path: "templates/ai-agent/knowledge.scl", err: errors.New("knowledge template failed")}, want: "knowledge template failed"},
		{name: "lesson link template", app: appWithAction(false, false), cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}, tpl: failingTemplateFS{TemplateFS: TemplatesFS, path: "templates/ai-agent/logic_skill.scl", err: errors.New("lesson link template failed")}, want: "lesson link template failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fsys := tt.fsys
			if fsys == nil {
				fsys = wiringFS()
			}
			tpl := tt.tpl
			if tpl == nil {
				tpl = TemplatesFS
			}
			withWiringApp(t, tt.app)
			_, err := CreateTool(fsys, tpl, wiringRoot, tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CreateTool() error = %v, want %q", err, tt.want)
			}
			if len(fsys.Files) == 1 {
				return
			}
			if strings.Contains(tt.name, "template") && len(fsys.Files) != 1 {
				t.Fatalf("template refusal wrote files: %#v", fsys.Files)
			}
		})
	}
	t.Run("title length", func(t *testing.T) {
		app := appWithAction(false, false)
		app.Actions[0].DisplayName = strings.Repeat("x", 256)
		withWiringApp(t, app)
		_, err := CreateTool(wiringFS(), TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"})
		if err == nil || !strings.Contains(err.Error(), "255 characters") {
			t.Fatalf("CreateTool() error = %v", err)
		}
	})
	t.Run("record read and action file write failures", func(t *testing.T) {
		fsys := wiringFS()
		fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")] = []byte("existing")
		fsys.ReadFileErr = os.ErrPermission
		withWiringApp(t, appWithTask())
		_, err := CreateTool(fsys, TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"})
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("record read error = %v", err)
		}

		fsys = wiringFS()
		fsys.WriteFileErr = os.ErrPermission
		withWiringApp(t, appWithTask())
		_, err = CreateTool(fsys, TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: LanguageRust})
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("action write error = %v", err)
		}
	})
}

func TestAgentWiringSmallHelpers(t *testing.T) {
	if got := defaultWakeInterval(90); got != 90 {
		t.Fatalf("defaultWakeInterval(90) = %d", got)
	}
	if got := relativeToRoot("/repo", "relative/path"); got != "relative/path" {
		t.Fatalf("relativeToRoot() fallback = %q", got)
	}
	if records, err := prepareRecords(wiringFS(), filepath.Join(wiringRoot, "apps", wiringAppID), nil); err != nil || len(records) != 0 {
		t.Fatalf("prepareRecords(empty) = %#v, %v", records, err)
	}
	if _, err := readKnowledgeBody(wiringFS(), wiringRoot, &agents.Knowledge{Body: agents.Body{}}); err == nil {
		t.Fatal("unreadable knowledge unexpectedly returned a body")
	}
	paths := actionFilePaths("/app", "my-tool", LanguageTypeScript)
	if len(paths) != 7 {
		t.Fatalf("TypeScript action paths = %#v", paths)
	}
	paths = actionFilePaths("/app", "my-tool", LanguageRust)
	if len(paths) != 3 {
		t.Fatalf("Rust action paths = %#v", paths)
	}
}

func TestSkillPositions(t *testing.T) {
	for _, tt := range []struct {
		position int
		want     string
	}{{20, "30"}, {30, "40"}} {
		app := appWithTask()
		app.TaskTypeSkillLinks = []*agents.TaskTypeSkillLink{{TaskTypeRef: agents.Reference{Key: "task_type_returns"}, KnowledgeRef: agents.Reference{Key: "persona_support"}, Position: tt.position}}
		withWiringApp(t, app)
		fsys := wiringFS()
		_, err := CreateSkill(fsys, TemplatesFS, wiringRoot, SkillConfig{AppID: wiringAppID, Name: "shipping-rules", Title: "Shipping Rules", TaskTypeName: "returns"})
		if err != nil {
			t.Fatalf("CreateSkill() error = %v", err)
		}
		links := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")])
		if !strings.Contains(links, "position "+tt.want) {
			t.Fatalf("position %s missing from %s", tt.want, links)
		}
	}
}

func TestRecordAppendHeadersAndRefusals(t *testing.T) {
	t.Run("new record file header", func(t *testing.T) {
		fsys := wiringFS()
		withWiringApp(t, baseWiringApp())
		_, err := CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing"})
		if err != nil {
			t.Fatal(err)
		}
		text := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "70_agent_knowledge.scl")])
		if !strings.HasPrefix(text, "# This file holds agent knowledge records.\n# simple new wrote this file; the Markdown files are where the words go.\n\n") {
			t.Fatalf("record header = %q", text)
		}
	})
	t.Run("I/O failure is wrapped", func(t *testing.T) {
		fsys := wiringFS()
		fsys.WriteFileErr = os.ErrPermission
		withWiringApp(t, baseWiringApp())
		_, err := CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing"})
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("WriteFile error = %v", err)
		}
	})
}

func TestAgentWiringRealParserEndToEnd(t *testing.T) {
	if os.Getenv("SIMPLE_CLI_INTEGRATION") == "" {
		t.Skip("Skipping integration test (set SIMPLE_CLI_INTEGRATION=1 to run)")
	}
	if _, err := build.EnsureSCLParser(nil); err != nil {
		t.Skipf("scl-parser is unavailable: %v", err)
	}
	collisionRoot := t.TempDir()
	if err := CreateAppStructure(fsx.OSFileSystem{}, TemplatesFS, collisionRoot, wiringAppID, "Acme Shop", "Example app"); err != nil {
		t.Fatalf("create collision app: %v", err)
	}
	collisionFile := filepath.Join(collisionRoot, "apps", wiringAppID, "records", "custom.scl")
	if err := os.WriteFile(collisionFile, []byte("set dev_simple_system.logic, persona_collision {\n  name \"unused\"\n}\n"), fsx.FilePerm); err != nil {
		t.Fatalf("write collision record: %v", err)
	}
	collisionBefore := snapshotFiles(t, filepath.Join(collisionRoot, "apps", wiringAppID))
	if _, err := CreateAgent(fsx.OSFileSystem{}, TemplatesFS, collisionRoot, AgentConfig{AppID: wiringAppID, Name: "collision", DisplayName: "Collision Agent"}); err == nil || !strings.Contains(err.Error(), "record key") {
		t.Fatalf("record key collision error = %v", err)
	}
	if !sameSnapshot(collisionBefore, snapshotFiles(t, filepath.Join(collisionRoot, "apps", wiringAppID))) {
		t.Fatal("record key refusal changed files")
	}

	root := t.TempDir()
	fsys := fsx.OSFileSystem{}
	if err := CreateAppStructure(fsys, TemplatesFS, root, wiringAppID, "Acme Shop", "Example app"); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if _, err := CreateAgent(fsys, TemplatesFS, root, AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support Agent", Description: "Answers customer questions."}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := CreateTaskType(fsys, TemplatesFS, root, TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", Tools: []string{"process-data"}}); err != nil {
		t.Fatalf("create task type: %v", err)
	}
	if _, err := CreateSkill(fsys, TemplatesFS, root, SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", TaskTypeName: "returns"}); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := CreateTool(fsys, TemplatesFS, root, ToolConfig{AppID: wiringAppID, Name: "introspect-data", TaskTypeName: "returns"}); err != nil {
		t.Fatalf("link platform tool: %v", err)
	}
	if err := CreateActionStructure(fsys, TemplatesFS, root, ActionConfig{AppID: wiringAppID, ActionName: "find-order", DisplayName: "Find Order", Language: LanguageRust, ExecutionEnv: "server"}); err != nil {
		t.Fatalf("create action without lesson: %v", err)
	}
	if _, err := CreateTool(fsys, TemplatesFS, root, ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}); err != nil {
		t.Fatalf("add action lesson: %v", err)
	}
	if _, err := CreateTool(fsys, TemplatesFS, root, ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", Description: "Find a customer by their account reference.", TaskTypeName: "returns", Language: LanguageTypeScript, Scope: "acme", ExecutionEnv: "server"}); err != nil {
		t.Fatalf("create app tool: %v", err)
	}

	app, err := agents.Load(fsys, root, wiringAppID)
	if err != nil {
		t.Fatalf("load generated records: %v", err)
	}
	canonical := app.Canonical()
	before := snapshotFiles(t, filepath.Join(root, "apps", wiringAppID))
	_, repeatErr := CreateAgent(fsys, TemplatesFS, root, AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support Agent"})
	if err := expectNoWriteRefusal(repeatErr); err != nil {
		t.Fatalf("repeat agent: %v", err)
	}
	_, repeatErr = CreateTaskType(fsys, TemplatesFS, root, TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"})
	if err := expectNoWriteRefusal(repeatErr); err != nil {
		t.Fatalf("repeat task type: %v", err)
	}
	if result, err := CreateSkill(fsys, TemplatesFS, root, SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"}); err != nil || !result.NothingToAdd {
		t.Fatalf("repeat skill = %#v, %v", result, err)
	}
	for _, name := range []string{"process-data", "introspect-data", "find-order", "lookup-customer"} {
		result, toolErr := CreateTool(fsys, TemplatesFS, root, ToolConfig{AppID: wiringAppID, Name: name, TaskTypeName: "returns", DisplayName: "Lookup Customer", Language: LanguageTypeScript, Scope: "acme", ExecutionEnv: "server"})
		if toolErr != nil || !result.NothingToAdd {
			t.Fatalf("repeat tool %s = %#v, %v", name, result, toolErr)
		}
	}
	after := snapshotFiles(t, filepath.Join(root, "apps", wiringAppID))
	if !sameSnapshot(before, after) {
		t.Fatal("repeat commands changed files after satisfying their missing-items rules")
	}
	appAfter, err := agents.Load(fsys, root, wiringAppID)
	if err != nil {
		t.Fatalf("reload generated records: %v", err)
	}
	if appAfter.Canonical() != canonical {
		t.Fatal("Canonical() changed after repeating commands")
	}
	assertNoDuplicateRecordKeys(t, filepath.Join(root, "apps", wiringAppID, "records"))
	problems, err := agents.Check(fsys, root, wiringAppID)
	if err != nil {
		t.Fatalf("agents.Check(): %v", err)
	}
	markerProblems, unbuiltProblems := 0, 0
	for _, problem := range problems {
		switch {
		case strings.Contains(problem.Rule, "still holds a line for you to replace"):
			markerProblems++
		case strings.Contains(problem.Rule, "has not been built yet"):
			unbuiltProblems++
		default:
			t.Errorf("unexpected check problem: %s:%d: %s", problem.File, problem.Line, problem.Rule)
		}
	}
	if markerProblems == 0 || unbuiltProblems != 2 {
		t.Fatalf("agents.Check() reported %d marker findings and %d unbuilt tools: %#v", markerProblems, unbuiltProblems, problems)
	}
}

func expectNoWriteRefusal(err error) error {
	if err == nil {
		return errors.New("expected the existing agent or kind of work to be refused")
	}
	return nil
}

func snapshotFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		result[filepath.ToSlash(rel)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot files: %v", err)
	}
	return result
}

func sameSnapshot(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for path, content := range left {
		if right[path] != content {
			return false
		}
	}
	return true
}

func assertNoDuplicateRecordKeys(t *testing.T, recordsDir string) {
	t.Helper()
	keyPattern := regexp.MustCompile(`(?m)^\s*set\s+[^,]+,\s*([^\s{]+)`)
	seen := make(map[string]string)
	if err := filepath.WalkDir(recordsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".scl" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range keyPattern.FindAllSubmatch(data, -1) {
			key := string(match[1])
			if prior, exists := seen[key]; exists {
				return errors.New("record key " + key + " appears in both " + prior + " and " + path)
			}
			seen[key] = path
		}
		return nil
	}); err != nil {
		t.Fatalf("record keys are not unique: %v", err)
	}
}

func appWithTwoAgents() *agents.App {
	app := baseWiringApp()
	app.AgentRecords = append(app.AgentRecords, &agents.Agent{Key: "agent_billing", DisplayName: "Billing Agent", Persona: app.Knowledge[0]})
	return app
}

func appWithoutPersona() *agents.App {
	return &agents.App{ID: wiringAppID, Dir: filepath.Join(wiringRoot, "apps", wiringAppID), AgentRecords: []*agents.Agent{{Key: "agent_support", DisplayName: "Support Agent"}}}
}

func appWithPersona(body string) *agents.App {
	app := baseWiringApp()
	app.AgentRecords[0].Persona.Body.Inline = body
	return app
}

func appWithTask() *agents.App {
	app := baseWiringApp()
	app.TaskTypes = []*agents.TaskType{{Key: "task_type_returns", Name: "returns"}}
	return app
}

func appWithSkill(hasLink bool) *agents.App {
	app := appWithTask()
	knowledge := &agents.Knowledge{Key: "skill_refund_policy", Title: "Refund Policy", Body: agents.Body{Path: "knowledge/skill-refund-policy.md", Readable: true}}
	app.Knowledge = append(app.Knowledge, knowledge)
	if hasLink {
		app.TaskTypeSkillLinks = []*agents.TaskTypeSkillLink{{TaskTypeRef: agents.Reference{Key: "task_type_returns"}, KnowledgeRef: agents.Reference{Key: knowledge.Key}, Position: 30}}
	}
	return app
}

func appWithAction(hasLesson, hasToolLink bool) *agents.App {
	app := appWithTask()
	action := &agents.Action{Key: "find_order", Name: "find-order", DisplayName: "Find Order"}
	app.Actions = []*agents.Action{action}
	if hasLesson {
		lesson := &agents.Knowledge{Key: "tool_find_order", Title: action.DisplayName, Body: agents.Body{Path: "knowledge/tool-find-order.md", Readable: true}}
		app.Knowledge = append(app.Knowledge, lesson)
		app.LogicSkillLinks = []*agents.LogicSkillLink{{LogicRef: agents.Reference{Key: action.Key}, KnowledgeRef: agents.Reference{Key: lesson.Key}}}
	}
	if hasToolLink {
		app.TaskTypeToolLinks = []*agents.TaskTypeToolLink{{TaskTypeRef: agents.Reference{Key: "task_type_returns"}, Tool: agents.ToolRef{ActionKey: action.Key, Name: action.Name, OwnAction: true}}}
	}
	return app
}

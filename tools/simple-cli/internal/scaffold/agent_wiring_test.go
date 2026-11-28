package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

type failNthWriteFS struct {
	*fsx.MockFileSystem
	failAt int
	writes int
	err    error
}

type trackedWriteFS struct {
	*fsx.MockFileSystem
	dirs        map[string]struct{}
	createdDirs []string
	removedDirs []string
	writes      int
	writeErrors map[int]error
}

func newTrackedWriteFS(base *fsx.MockFileSystem) *trackedWriteFS {
	tracked := &trackedWriteFS{MockFileSystem: base, dirs: make(map[string]struct{}), writeErrors: make(map[int]error)}
	for path, content := range base.Files {
		if content == nil {
			tracked.dirs[filepath.Clean(path)] = struct{}{}
		}
		for parent := filepath.Dir(path); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			tracked.dirs[filepath.Clean(parent)] = struct{}{}
		}
	}
	return tracked
}

func (f *trackedWriteFS) Stat(name string) (os.FileInfo, error) {
	if _, exists := f.dirs[filepath.Clean(name)]; exists {
		return nil, nil
	}
	return f.MockFileSystem.Stat(name)
}

func (f *trackedWriteFS) MkdirAll(name string, _ os.FileMode) error {
	var missing []string
	for current := filepath.Clean(name); ; current = filepath.Dir(current) {
		if _, exists := f.dirs[current]; exists {
			break
		}
		if _, exists := f.Files[current]; exists {
			break
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		f.dirs[missing[i]] = struct{}{}
		f.createdDirs = append(f.createdDirs, missing[i])
	}
	return nil
}

func (f *trackedWriteFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	f.writes++
	if err := f.writeErrors[f.writes]; err != nil {
		return err
	}
	return f.MockFileSystem.WriteFile(name, data, perm)
}

func (f *trackedWriteFS) Remove(name string) error {
	name = filepath.Clean(name)
	if _, exists := f.Files[name]; exists {
		return f.MockFileSystem.Remove(name)
	}
	if _, exists := f.dirs[name]; exists {
		for path := range f.Files {
			if strings.HasPrefix(path, name+string(filepath.Separator)) {
				return fmt.Errorf("directory %s is not empty", name)
			}
		}
		for path := range f.dirs {
			if path != name && strings.HasPrefix(path, name+string(filepath.Separator)) {
				return fmt.Errorf("directory %s is not empty", name)
			}
		}
		delete(f.dirs, name)
		f.removedDirs = append(f.removedDirs, name)
		return nil
	}
	return nil
}

func (f *failNthWriteFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	f.writes++
	if err := f.MockFileSystem.WriteFile(name, data, perm); err != nil {
		return err
	}
	if f.writes == f.failAt {
		return f.err
	}
	return nil
}

func TestCreateAgentWriteFailuresRestoreEveryFile(t *testing.T) {
	writeErr := errors.New("disk write failed")
	for _, failAt := range []int{2, 3} {
		t.Run(fmt.Sprintf("write %d", failAt), func(t *testing.T) {
			base := wiringFS()
			recordPath := filepath.Join(wiringRoot, "apps", wiringAppID, "records", "70_agent_knowledge.scl")
			base.Files[recordPath] = []byte("existing knowledge records\n")
			before := make(map[string][]byte, len(base.Files))
			for path, content := range base.Files {
				before[path] = append([]byte(nil), content...)
			}
			fsys := &failNthWriteFS{MockFileSystem: base, failAt: failAt, err: writeErr}
			withWiringApp(t, baseWiringApp())
			_, err := CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing Agent"})
			if !errors.Is(err, writeErr) {
				t.Fatalf("CreateAgent() error = %v, want original write error", err)
			}
			if !reflect.DeepEqual(fsys.Files, before) {
				t.Errorf("files after failure = %#v, want original files %#v", fsys.Files, before)
			}
		})
	}
}

func TestCreateToolWriteFailuresRemoveItsActionAndRecords(t *testing.T) {
	writeErr := errors.New("tool output write failed")
	for _, failAt := range []int{2, 5} {
		t.Run(fmt.Sprintf("write %d", failAt), func(t *testing.T) {
			base := wiringFS()
			before := make(map[string][]byte, len(base.Files))
			for path, content := range base.Files {
				before[path] = append([]byte(nil), content...)
			}
			fsys := &failNthWriteFS{MockFileSystem: base, failAt: failAt, err: writeErr}
			withWiringApp(t, appWithTask())
			_, err := CreateTool(fsys, TemplatesFS, wiringRoot, ToolConfig{
				AppID: wiringAppID, Name: "summary-tool", DisplayName: "Summary Tool", TaskTypeName: "returns",
				Language: LanguageRust, ExecutionEnv: "server",
			})
			if !errors.Is(err, writeErr) {
				t.Fatalf("CreateTool() error = %v, want original write error", err)
			}
			if !reflect.DeepEqual(fsys.Files, before) {
				t.Errorf("files after failure = %#v, want original files %#v", fsys.Files, before)
			}
		})
	}
}

func TestCreateToolRollbackRemovesOnlyItsNewDirectoriesDeepestFirst(t *testing.T) {
	writeErr := errors.New("later action file write failed")
	fsys := newTrackedWriteFS(wiringFS())
	fsys.writeErrors[2] = writeErr
	withWiringApp(t, appWithTask())
	actionDir := filepath.Join(wiringRoot, "apps", wiringAppID, "actions", "summary-tool")
	_, err := CreateTool(fsys, TemplatesFS, wiringRoot, ToolConfig{
		AppID: wiringAppID, Name: "summary-tool", DisplayName: "Summary Tool", TaskTypeName: "returns",
		Language: LanguageTypeScript, Scope: "com.acme.shop",
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("CreateTool() error = %v, want the later write error", err)
	}
	actionsDir := filepath.Dir(actionDir)
	for _, dir := range []string{actionsDir, actionDir, filepath.Join(actionDir, "src")} {
		if _, exists := fsys.dirs[dir]; exists {
			t.Errorf("rollback left created directory %s", dir)
		}
	}
	if got := fsys.createdDirs; !reflect.DeepEqual(got, []string{actionsDir, actionDir, filepath.Join(actionDir, "src")}) {
		t.Errorf("created directories = %#v, want all action folders", got)
	}
	if _, exists := fsys.dirs[filepath.Join(wiringRoot, "apps", wiringAppID)]; !exists {
		t.Error("rollback removed the pre-existing app directory")
	}
	if got := fsys.removedDirs; !reflect.DeepEqual(got, []string{filepath.Join(actionDir, "src"), actionDir, actionsDir}) {
		t.Errorf("removed directories = %#v, want deepest first", got)
	}
	if len(fsys.Files) != 1 {
		t.Errorf("rollback left files behind: %#v", fsys.Files)
	}
}

func TestAgentWiringReportsEveryFileThatCouldNotBeRestored(t *testing.T) {
	writeErr := errors.New("record write failed")
	restoreErr := errors.New("record restore failed")
	base := wiringFS()
	knowledgePath := filepath.Join(wiringRoot, "apps", wiringAppID, "records", "70_agent_knowledge.scl")
	agentPath := filepath.Join(wiringRoot, "apps", wiringAppID, "records", "71_agents.scl")
	base.Files[knowledgePath] = []byte("existing knowledge\n")
	base.Files[agentPath] = []byte("existing agents\n")
	fsys := newTrackedWriteFS(base)
	fsys.writeErrors[3] = writeErr
	fsys.writeErrors[4] = restoreErr
	fsys.writeErrors[5] = restoreErr
	withWiringApp(t, baseWiringApp())
	_, err := CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing Agent"})
	if !errors.Is(err, writeErr) {
		t.Fatalf("CreateAgent() error = %v, want original write error", err)
	}
	for _, path := range []string{"apps/com.acme.shop/records/70_agent_knowledge.scl", "apps/com.acme.shop/records/71_agents.scl"} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("error %q does not name failed restore %s", err, path)
		}
	}
	if !strings.Contains(err.Error(), "git checkout -- apps/com.acme.shop/records/70_agent_knowledge.scl apps/com.acme.shop/records/71_agents.scl") {
		t.Errorf("error %q does not give the restore command", err)
	}
}

func TestAgentNameRulesAndTextEscaping(t *testing.T) {
	badNames := []string{"refund--policy", "refund-"}
	for _, name := range badNames {
		t.Run(name, func(t *testing.T) {
			want := "use lowercase letters and digits in groups joined by single hyphens, starting with a letter"
			checks := []struct {
				label string
				err   error
			}{
				{label: "agent", err: validateAgentConfig(AgentConfig{Name: name, DisplayName: "Agent"})},
				{label: "skill", err: validateSkillConfig(SkillConfig{Name: name})},
				{label: "knowledge", err: validateKnowledgeConfig(KnowledgeConfig{Name: name})},
				{label: "tool", err: validateToolConfig(ToolConfig{Name: name})},
				{label: "tool list", err: validateTaskTypeConfig(TaskTypeConfig{Name: "returns", PluralName: "Returns", WakeIntervalSeconds: 1, Tools: []string{name}})},
			}
			for _, check := range checks {
				if check.err == nil || !strings.Contains(check.err.Error(), want) {
					t.Errorf("%s validation for %q = %v, want %q", check.label, name, check.err, want)
				}
			}
		})
	}
	for _, name := range []string{"refund--policy", "refund-", "refund__policy", "refund_"} {
		if err := validateTaskTypeConfig(TaskTypeConfig{Name: name, PluralName: "Returns", WakeIntervalSeconds: 1}); err == nil {
			t.Errorf("kind-of-work name %q unexpectedly passed", name)
		}
	}

	for _, field := range []string{"display name", "title", "description", "plural name"} {
		if err := validateScaffoldText("agent", "support", field, `Ends with\`, 100); err == nil || !strings.Contains(err.Error(), "must not contain a backslash; remove it") {
			t.Errorf("backslash in %s = %v", field, err)
		}
	}
}

func TestScaffoldRefusalMessagesUseOneWording(t *testing.T) {
	tests := []struct {
		name string
		err  func(*testing.T) error
		want string
	}{
		{name: "invalid tool name", err: func(*testing.T) error {
			return validateTaskTypeConfig(TaskTypeConfig{Name: "returns", PluralName: "Returns", Tools: []string{"refund--policy"}})
		}, want: `tool "refund--policy" in --tools has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter`},
		{name: "unsupported language", err: func(t *testing.T) error {
			withWiringApp(t, appWithTask())
			_, err := CreateTool(wiringFS(), TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: "go"})
			return err
		}, want: `tool "lookup-customer" has unsupported language "go"; choose ts or rust`},
		{name: "unsupported environment", err: func(t *testing.T) error {
			withWiringApp(t, appWithTask())
			_, err := CreateTool(wiringFS(), TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: LanguageRust, ExecutionEnv: "edge"})
			return err
		}, want: `tool "lookup-customer" has unsupported environment "edge"; choose server, client, or both`},
		{name: "invalid every", err: func(*testing.T) error {
			return validateTaskTypeConfig(TaskTypeConfig{Name: "returns", PluralName: "Returns", WakeIntervalSeconds: -1})
		}, want: `--every must be a whole-second duration of at least one second, such as 90s, 30m, or 1h`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.err(t)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want exactly %q", err, tt.want)
			}
		})
	}
}

func TestCreateTaskTypeUsesTheExistingPlaybookForSizeChecks(t *testing.T) {
	appDir := filepath.Join(wiringRoot, "apps", wiringAppID)
	playbookPath := filepath.Join(appDir, "knowledge", "playbook-refund-review.md")
	fsys := wiringFS()
	fsys.Files[playbookPath] = []byte(strings.Repeat("x", 16_385) + "\n")
	withWiringApp(t, appWithTask())
	_, err := CreateTaskType(fsys, TemplatesFS, wiringRoot, TaskTypeConfig{
		AppID: wiringAppID, Name: "refund-review", PluralName: "Refund Reviews", WakeIntervalSeconds: 3600,
	})
	if err == nil || !strings.Contains(err.Error(), "16384-byte limit") {
		t.Fatalf("CreateTaskType() error = %v, want a size finding for the reused playbook", err)
	}
}

func TestCreateToolReusesOnlyMatchingPlatformLookups(t *testing.T) {
	t.Run("task type refuses a lookup for another tool", func(t *testing.T) {
		original := findPlatformToolLookup
		findPlatformToolLookup = func(*agents.App, string, string) (agents.LookupInfo, bool) {
			return agents.LookupInfo{AppID: "com.acme.shop", Name: "different-tool", File: "apps/com.acme.shop/records/custom.scl", Line: 23}, true
		}
		t.Cleanup(func() { findPlatformToolLookup = original })
		withWiringApp(t, baseWiringApp())
		_, err := CreateTaskType(wiringFS(), TemplatesFS, wiringRoot, TaskTypeConfig{
			AppID: wiringAppID, Name: "returns", PluralName: "Returns", Tools: []string{"process-data"},
		})
		if err == nil || !strings.Contains(err.Error(), "apps/com.acme.shop/records/custom.scl:23") || !strings.Contains(err.Error(), "platform_tool_process_data") {
			t.Fatalf("CreateTaskType() error = %v, want the conflicting variable and its location", err)
		}
	})
	t.Run("existing link needs no extra lookup", func(t *testing.T) {
		app := appWithTask()
		app.TaskTypeToolLinks = append(app.TaskTypeToolLinks, &agents.TaskTypeToolLink{
			File: "apps/com.acme.shop/records/custom.scl", TaskTypeRef: agents.Reference{Key: "task_type_returns"},
			Tool: agents.ToolRef{AppID: agents.PlatformAppID, Name: "process-data"},
		})
		withWiringApp(t, app)
		fsys := wiringFS()
		result, err := CreateTool(fsys, TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"})
		if err != nil || !result.NothingToAdd || len(fsys.Files) != 1 {
			t.Fatalf("CreateTool() = %#v, %v; want a no-op without a new lookup", result, err)
		}
	})
	t.Run("clashing variable is refused with its location", func(t *testing.T) {
		original := findPlatformToolLookup
		findPlatformToolLookup = func(*agents.App, string, string) (agents.LookupInfo, bool) {
			return agents.LookupInfo{AppID: "com.acme.shop", Name: "different-tool", File: "apps/com.acme.shop/records/custom.scl", Line: 23}, true
		}
		t.Cleanup(func() { findPlatformToolLookup = original })
		withWiringApp(t, appWithTask())
		_, err := CreateTool(wiringFS(), TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"})
		if err == nil || !strings.Contains(err.Error(), "apps/com.acme.shop/records/custom.scl:23") {
			t.Fatalf("CreateTool() error = %v, want the conflicting variable location", err)
		}
	})
	t.Run("matching variable is reused", func(t *testing.T) {
		original := findPlatformToolLookup
		findPlatformToolLookup = func(*agents.App, string, string) (agents.LookupInfo, bool) {
			return agents.LookupInfo{AppID: agents.PlatformAppID, Name: "process-data", File: "apps/com.acme.shop/records/72_agent_links.scl", Line: 3}, true
		}
		t.Cleanup(func() { findPlatformToolLookup = original })
		withWiringApp(t, appWithTask())
		fsys := wiringFS()
		if _, err := CreateTool(fsys, TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "process-data", TaskTypeName: "returns"}); err != nil {
			t.Fatal(err)
		}
		links := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")])
		if strings.Contains(links, "var platform_tool_process_data") {
			t.Fatalf("matching lookup was written again: %s", links)
		}
	})
}

func TestCreateSkillListsReusedBodyMarkers(t *testing.T) {
	app := appWithSkill(false)
	withWiringApp(t, app)
	fsys := wiringFS()
	bodyPath := filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "skill-refund-policy.md")
	fsys.Files[bodyPath] = []byte("# Refund Policy\n\nTODO(simple): finish the existing skill\n")
	result, err := CreateSkill(fsys, TemplatesFS, wiringRoot, SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 1 || len(result.FillIn) != 1 || result.FillIn[0] != "apps/com.acme.shop/knowledge/skill-refund-policy.md" {
		t.Fatalf("CreateSkill() result = %#v, want reused body in fill-in list", result)
	}
}

func TestNoOpAgentCommandsStillListReusedBodyMarkers(t *testing.T) {
	tests := []struct {
		name   string
		app    *agents.App
		path   string
		create func(*fsx.MockFileSystem) (WiringResult, error)
	}{
		{
			name: "skill",
			app:  appWithSkill(true),
			path: "knowledge/skill-refund-policy.md",
			create: func(fsys *fsx.MockFileSystem) (WiringResult, error) {
				return CreateSkill(fsys, TemplatesFS, wiringRoot, SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"})
			},
		},
		{
			name: "knowledge",
			app:  appWithReferenceKnowledge(true),
			path: "knowledge/return-terms.md",
			create: func(fsys *fsx.MockFileSystem) (WiringResult, error) {
				return CreateKnowledge(fsys, TemplatesFS, wiringRoot, KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", TaskTypeName: "returns"})
			},
		},
		{
			name: "tool lesson",
			app:  appWithAction(true, true),
			path: "knowledge/tool-find-order.md",
			create: func(fsys *fsx.MockFileSystem) (WiringResult, error) {
				return CreateTool(fsys, TemplatesFS, wiringRoot, ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withWiringApp(t, tt.app)
			fsys := wiringFS()
			path := filepath.Join(wiringRoot, "apps", wiringAppID, filepath.FromSlash(tt.path))
			fsys.Files[path] = []byte("TODO(simple): finish this body\n")
			got, err := tt.create(fsys)
			if err != nil {
				t.Fatalf("repeat command error = %v", err)
			}
			want := "apps/com.acme.shop/" + tt.path
			if !got.NothingToAdd || len(got.FillIn) != 1 || got.FillIn[0] != want {
				t.Fatalf("repeat command result = %#v, want no-op and fill-in %q", got, want)
			}
		})
	}
}

func TestNoOpAgentAndTaskTypeCommandsStillListMarkers(t *testing.T) {
	tests := []struct {
		name   string
		app    *agents.App
		paths  []string
		create func(*fsx.MockFileSystem) (WiringResult, error)
	}{
		{
			name: "agent persona",
			app: func() *agents.App {
				app := appWithAgentDetails("Support Agent", "")
				app.AgentRecords[0].Persona.Body = agents.Body{Path: "knowledge/support-persona.md", Readable: true}
				return app
			}(),
			paths: []string{"knowledge/support-persona.md"},
			create: func(fsys *fsx.MockFileSystem) (WiringResult, error) {
				return CreateAgent(fsys, TemplatesFS, wiringRoot, AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support Agent"})
			},
		},
		{
			name: "task type persona, playbook, and shape",
			app: func() *agents.App {
				app := appWithCompleteTask()
				app.TaskTypes[0].ShapePath = "records/task-types/returns.json"
				app.AgentRecords[0].Persona.Body = agents.Body{Path: "knowledge/support-persona.md", Readable: true}
				app.TaskTypes[0].Playbook = &agents.Knowledge{Key: "playbook_returns", Body: agents.Body{Path: "knowledge/playbook-returns.md", Readable: true}}
				return app
			}(),
			paths: []string{"knowledge/playbook-returns.md", "knowledge/support-persona.md", "records/task-types/returns.json"},
			create: func(fsys *fsx.MockFileSystem) (WiringResult, error) {
				return CreateTaskType(fsys, TemplatesFS, wiringRoot, TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withWiringApp(t, test.app)
			fsys := wiringFS()
			wantFillIn := make([]string, 0, len(test.paths))
			for _, path := range test.paths {
				fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, filepath.FromSlash(path))] = []byte("TODO(simple): finish this body\n")
				wantFillIn = append(wantFillIn, "apps/com.acme.shop/"+path)
			}
			got, err := test.create(fsys)
			if err != nil {
				t.Fatalf("repeat command error = %v", err)
			}
			if !got.NothingToAdd || !reflect.DeepEqual(got.FillIn, wantFillIn) || len(fsys.Files) != len(test.paths)+1 {
				t.Fatalf("repeat command result = %#v, files = %#v; want no-op and markers %#v", got, fsys.Files, wantFillIn)
			}
		})
	}
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
		name     string
		app      func() *agents.App
		cfg      AgentConfig
		wantErr  string
		wantNoop bool
	}{
		{name: "writes persona and records", app: baseWiringApp, cfg: AgentConfig{AppID: wiringAppID, Name: "billing", DisplayName: "Billing Agent", Description: "Answers billing questions."}},
		{name: "matching agent is a no-op", app: func() *agents.App { return appWithAgentDetails("Support Agent", "Answers customer questions.") }, cfg: AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support Agent", Description: "Answers customer questions."}, wantNoop: true},
		{name: "omitted description reuses matching agent", app: func() *agents.App { return appWithAgentDetails("Support Agent", "Answers customer questions.") }, cfg: AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support Agent"}, wantNoop: true},
		{name: "different display name refuses", app: func() *agents.App { return appWithAgentDetails("Support Agent", "") }, cfg: AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Help Agent"}, wantErr: `display name "Support Agent", not "Help Agent"`},
		{name: "different requested description refuses", app: func() *agents.App { return appWithAgentDetails("Support Agent", "Answers customer questions.") }, cfg: AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support Agent", Description: "Answers billing questions."}, wantErr: `description "Answers customer questions.", not "Answers billing questions."`},
		{name: "invalid name refuses", app: baseWiringApp, cfg: AgentConfig{AppID: wiringAppID, Name: "Bad_Name", DisplayName: "Name"}, wantErr: `agent "Bad_Name" has an invalid name`},
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
			if tt.wantNoop {
				if !got.NothingToAdd || len(fsys.Files) != 1 {
					t.Fatalf("matching repeat result = %#v and wrote files %#v", got, fsys.Files)
				}
				return
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

func TestAgentCommandsRefuseUnsafeNamesAndAppIDsWithoutWriting(t *testing.T) {
	commands := []struct {
		name string
		run  func(*fsx.MockFileSystem, string, string) error
	}{
		{name: "agent", run: func(f *fsx.MockFileSystem, appID, name string) error {
			withWiringApp(t, baseWiringApp())
			_, err := CreateAgent(f, TemplatesFS, wiringRoot, AgentConfig{AppID: appID, Name: name, DisplayName: "Support"})
			return err
		}},
		{name: "task type", run: func(f *fsx.MockFileSystem, appID, name string) error {
			withWiringApp(t, baseWiringApp())
			_, err := CreateTaskType(f, TemplatesFS, wiringRoot, TaskTypeConfig{AppID: appID, Name: name, PluralName: "Returns", NoAgent: true})
			return err
		}},
		{name: "skill", run: func(f *fsx.MockFileSystem, appID, name string) error {
			withWiringApp(t, appWithTask())
			_, err := CreateSkill(f, TemplatesFS, wiringRoot, SkillConfig{AppID: appID, Name: name, Title: "Support", TaskTypeName: "returns"})
			return err
		}},
		{name: "knowledge", run: func(f *fsx.MockFileSystem, appID, name string) error {
			withWiringApp(t, appWithTask())
			_, err := CreateKnowledge(f, TemplatesFS, wiringRoot, KnowledgeConfig{AppID: appID, Name: name, Title: "Support", TaskTypeName: "returns"})
			return err
		}},
		{name: "tool", run: func(f *fsx.MockFileSystem, appID, name string) error {
			withWiringApp(t, appWithTask())
			_, err := CreateTool(f, TemplatesFS, wiringRoot, ToolConfig{AppID: appID, Name: name})
			return err
		}},
	}
	unsafe := []string{"..", "../../..", "a/b", filepath.Join(string(filepath.Separator), "tmp", "escaped")}
	for _, command := range commands {
		for _, name := range unsafe {
			t.Run(command.name+"/name/"+name, func(t *testing.T) {
				fsys := wiringFS()
				before := cloneWiringFiles(fsys.Files)
				err := command.run(fsys, wiringAppID, name)
				if err == nil {
					t.Fatal("unsafe name was accepted")
				}
				if !reflect.DeepEqual(fsys.Files, before) {
					t.Fatalf("refusal changed files: %#v", fsys.Files)
				}
			})
		}
		for _, appID := range append(append([]string{}, unsafe...), "com.acme.missing") {
			t.Run(command.name+"/app/"+appID, func(t *testing.T) {
				fsys := wiringFS()
				before := cloneWiringFiles(fsys.Files)
				if err := command.run(fsys, appID, "safe-name"); err == nil {
					t.Fatal("invalid or missing app was accepted")
				}
				if !reflect.DeepEqual(fsys.Files, before) {
					t.Fatalf("refusal changed files: %#v", fsys.Files)
				}
			})
		}
	}
}

func cloneWiringFiles(files map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(files))
	for path, data := range files {
		cloned[path] = append([]byte(nil), data...)
	}
	return cloned
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
		{name: "reference knowledge", path: "templates/ai-agent/knowledge.md", data: map[string]string{"Title": "Return Terms"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text, renderErr := renderAgentTemplate(TemplatesFS, tt.path, tt.data)
			if renderErr != nil {
				t.Fatal(renderErr)
			}
			if tt.name == "reference knowledge" {
				if text != "# Return Terms\n\nTODO(simple): Write the reference material this work reads, such as a list of terms or standing answers.\n" {
					t.Fatalf("reference knowledge template = %q", text)
				}
				return
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
		{name: "missing app", fsys: &fsx.MockFileSystem{Files: map[string][]byte{}}, want: `app com.acme.shop does not exist: run 'simple new app com.acme.shop "<Display name>"' first`},
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
			{cfg: TaskTypeConfig{Name: strings.Repeat("a", 101), PluralName: "Returns"}, want: "kind of work"},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: strings.Repeat("a", 101)}, want: "plural name is longer"},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: "Returns", Description: strings.Repeat("a", 1001)}, want: "description is longer"},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: "Returns", WakeIntervalSeconds: -1}, want: `--every must be a whole-second duration of at least one second`},
			{cfg: TaskTypeConfig{Name: "returns", PluralName: "Returns", Tools: []string{"Bad_Name"}}, want: `tool "Bad_Name" in --tools has an invalid name`},
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
		if err := validateScaffoldText("tool", "find-order", "description", strings.Repeat("x", 1001), 1000); err == nil {
			t.Error("expected text length error")
		}
	})
}

func TestCreateTaskType(t *testing.T) {
	tests := []struct {
		name             string
		app              *agents.App
		cfg              TaskTypeConfig
		wantWakeInterval string
		wantErr          string
		wantNoop         bool
	}{
		{name: "creates complete task type with platform lookup", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", Tools: []string{"process-data"}}},
		{name: "writes requested wake interval", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", WakeIntervalSeconds: 90}, wantWakeInterval: "90"},
		{name: "one agent may be inferred", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}},
		{name: "work for people needs no agent wiring", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "manual-review", PluralName: "Manual Reviews", NoAgent: true}},
		{name: "no-agent refuses agent flag", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "manual-review", PluralName: "Manual Reviews", NoAgent: true, AgentFlagSet: true}, wantErr: "cannot use --no-agent"},
		{name: "no-agent refuses tools flag", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "manual-review", PluralName: "Manual Reviews", NoAgent: true, ToolsFlagSet: true}, wantErr: "cannot use --no-agent"},
		{name: "no-agent refuses every flag", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "manual-review", PluralName: "Manual Reviews", NoAgent: true, EveryFlagSet: true}, wantErr: "cannot use --no-agent"},
		{name: "no agents", app: &agents.App{ID: wiringAppID}, cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}, wantErr: `kind of work "returns" has no agent`},
		{name: "multiple agents need selection", app: appWithTwoAgents(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}, wantErr: `kind of work "returns" has multiple agents`},
		{name: "named unknown agent", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "billing"}, wantErr: "does not exist"},
		{name: "selected agent without persona", app: appWithoutPersona(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support"}, wantErr: "no readable persona"},
		{name: "unknown tool", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", Tools: []string{"missing-tool"}}, wantErr: "without --task-type, then run this command again"},
		{name: "bad name", app: baseWiringApp(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "Bad_Name", PluralName: "Returns", AgentName: "support"}, wantErr: `kind of work "Bad_Name" has an invalid name`},
		{name: "matching task type is a no-op", app: appWithCompleteTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", Tools: []string{"process-data"}, WakeIntervalSeconds: 90}, wantNoop: true},
		{name: "repeat uses its linked agent when the app has several agents", app: appWithTwoDefaultAgents(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", Tools: []string{"process-data"}, WakeIntervalSeconds: 90}, wantNoop: true},
		{name: "unknown requested agent refuses on existing work", app: appWithCompleteTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "missing"}, wantErr: `agent "missing" does not exist`},
		{name: "omitted description reuses matching task type", app: appWithDescribedTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", Tools: []string{"process-data"}, WakeIntervalSeconds: 90}, wantNoop: true},
		{name: "different plural name refuses", app: appWithCompleteTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returned Orders", AgentName: "support"}, wantErr: `plural name "Returns", not "Returned Orders"`},
		{name: "different requested description refuses", app: appWithDescribedTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", Description: "Checks refunds.", AgentName: "support"}, wantErr: `description "Reviews returns.", not "Checks refunds."`},
		{name: "different agent refuses", app: appWithTwoDefaultAgents(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "billing"}, wantErr: `default agent "Support Agent", not "Billing Agent"`},
		{name: "missing requested tool refuses", app: appWithCompleteTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", Tools: []string{"mutate-data"}}, wantErr: `tool "mutate-data"`},
		{name: "different requested interval refuses", app: appWithCompleteTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support", WakeIntervalSeconds: 3600}, wantErr: `wake interval 90 seconds, not 3600 seconds`},
		{name: "no-agent differs from agent-owned work", app: appWithCompleteTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", NoAgent: true}, wantErr: `agent-owned work, not people-only work`},
		{name: "agent-owned work differs from no-agent task", app: appWithNoAgentTask(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support"}, wantErr: `people-only work, not agent-owned work`},
		{name: "repeating task type with agent for work without default agent", app: appWithWorkWithoutDefaultAgent(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns", AgentName: "support"}, wantErr: `kind of work "returns" already exists in com.acme.shop without a default agent; repair its agent link before repeating the command`},
		{name: "repeating task type without agent for work without default agent", app: appWithWorkWithoutDefaultAgent(), cfg: TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}, wantErr: `kind of work "returns" already exists in com.acme.shop without a default agent; repair its agent link before repeating the command`},
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
			if tt.wantNoop {
				if !got.NothingToAdd || len(fsys.Files) != 1 {
					t.Fatalf("matching repeat result = %#v and wrote files %#v", got, fsys.Files)
				}
				return
			}
			wantFillIn := 2
			if tt.cfg.NoAgent {
				wantFillIn = 1
			}
			if len(got.FillIn) != wantFillIn {
				t.Fatalf("unexpected marker paths: %#v", got.FillIn)
			}
			taskRecord, exists := fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "71_agents.scl")]
			if !exists || strings.Contains(string(taskRecord), "wake_interval_seconds") != (tt.wantWakeInterval != "") || tt.wantWakeInterval != "" && !strings.Contains(string(taskRecord), "wake_interval_seconds "+tt.wantWakeInterval) {
				t.Errorf("task type wake interval = %q, want %q", taskRecord, tt.wantWakeInterval)
			}
			if tt.wantWakeInterval != "" {
				wantExact := "  states `$file('records/task-types/states.json') |> $json()`\n  wake_interval_seconds " + tt.wantWakeInterval + "\n}"
				if !strings.Contains(string(taskRecord), wantExact) {
					t.Errorf("task type record missing exact interval block:\n%s\nwant:\n%s", taskRecord, wantExact)
				}
			} else if !tt.cfg.NoAgent && tt.wantErr == "" {
				wantExact := "  states `$file('records/task-types/states.json') |> $json()`\n}"
				if !strings.Contains(string(taskRecord), wantExact) {
					t.Errorf("task type record missing exact no-interval block:\n%s\nwant:\n%s", taskRecord, wantExact)
				}
			}
			if tt.cfg.NoAgent {
				appDir := filepath.Join(wiringRoot, "apps", wiringAppID)
				if _, exists := fsys.Files[filepath.Join(appDir, "knowledge", "playbook-manual-review.md")]; exists {
					t.Fatal("no-agent task type created a playbook")
				}
				taskRecord := string(fsys.Files[filepath.Join(appDir, "records", "71_agents.scl")])
				if strings.Contains(taskRecord, "playbook_id") || strings.Contains(taskRecord, "default_agent") {
					t.Fatalf("no-agent task type has agent wiring: %s", taskRecord)
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
		filepath.Join(appDir, "knowledge", "playbook-returns.md"),
		filepath.Join(appDir, "records", "task-types", "returns.json"),
		filepath.Join(appDir, "records", "task-types", "shared-states.json"),
		filepath.Join(appDir, "knowledge", "support-persona.md"),
	}
	for _, path := range paths {
		fsys.Files[path] = []byte("kept")
	}
	existingPlaybook := []byte("## Collaboration\n\nCoordinate decisions with the person responsible for this work.\n\n## Domain Boundaries\n\nWork only within the agreed scope and raise anything outside it.\n\n## Completion Voice\n\nReport completed work and any remaining decisions clearly.\n")
	fsys.Files[paths[0]] = existingPlaybook
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
	if string(fsys.Files[paths[0]]) != string(existingPlaybook) || string(fsys.Files[paths[1]]) != "kept" || string(fsys.Files[paths[2]]) != "kept" {
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
		{name: "invalid skill name", app: appWithTask(), cfg: SkillConfig{AppID: wiringAppID, Name: "Refund_Policy", Title: "Refund Policy", TaskTypeName: "returns"}, wantErr: `skill "Refund_Policy" has an invalid name`},
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

func TestCreateKnowledge(t *testing.T) {
	tests := []struct {
		name     string
		app      *agents.App
		cfg      KnowledgeConfig
		wantErr  string
		wantNoop bool
	}{
		{name: "creates reference knowledge", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: "Return Terms", TaskTypeName: "returns"}},
		{name: "existing knowledge adds link only", app: appWithReferenceKnowledge(false), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", TaskTypeName: "returns"}},
		{name: "existing knowledge and link is a no-op", app: appWithReferenceKnowledge(true), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", TaskTypeName: "returns"}, wantNoop: true},
		{name: "new knowledge requires title", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", TaskTypeName: "returns"}, wantErr: "needs a title"},
		{name: "missing task type", app: baseWiringApp(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: "Return Terms", TaskTypeName: "returns"}, wantErr: "does not exist"},
		{name: "skill prefix refused", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "skill-return-terms", Title: "Return Terms", TaskTypeName: "returns"}, wantErr: "reserved"},
		{name: "tool prefix refused", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "tool-return-terms", Title: "Return Terms", TaskTypeName: "returns"}, wantErr: "reserved"},
		{name: "playbook prefix refused", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "playbook-returns", Title: "Returns", TaskTypeName: "returns"}, wantErr: "reserved"},
		{name: "invalid name refused", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return_terms", Title: "Return Terms", TaskTypeName: "returns"}, wantErr: `reference knowledge "return_terms" has an invalid name`},
		{name: "quoted title refused", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: `Return "Terms"`, TaskTypeName: "returns"}, wantErr: "double quote"},
		{name: "long title refused", app: appWithTask(), cfg: KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: strings.Repeat("x", 256), TaskTypeName: "returns"}, wantErr: "255 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := wiringFS()
			withWiringApp(t, tt.app)
			got, err := CreateKnowledge(fsys, TemplatesFS, wiringRoot, tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CreateKnowledge() error = %v, want %q", err, tt.wantErr)
				}
				if len(fsys.Files) != 1 {
					t.Fatal("refusal wrote files")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateKnowledge() error = %v", err)
			}
			if tt.wantNoop {
				if !got.NothingToAdd || len(fsys.Files) != 1 {
					t.Fatalf("expected no-op, got %#v", got)
				}
				return
			}
			if tt.name == "creates reference knowledge" {
				path := filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "return-terms.md")
				if string(fsys.Files[path]) != "# Return Terms\n\nTODO(simple): Write the reference material this work reads, such as a list of terms or standing answers.\n" {
					t.Fatalf("new body = %q", fsys.Files[path])
				}
				links := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")])
				if !strings.Contains(links, "task_type_returns_carries_knowledge_return_terms") || !strings.Contains(links, "position 10") {
					t.Fatalf("knowledge link = %s", links)
				}
			}
			if tt.name == "existing knowledge adds link only" && len(got.Created) != 1 {
				t.Fatalf("existing knowledge should create only a records file: %#v", got)
			}
		})
	}
	t.Run("uses the next position after every knowledge link", func(t *testing.T) {
		app := appWithTask()
		app.TaskTypeSkillLinks = []*agents.TaskTypeSkillLink{{TaskTypeRef: agents.Reference{Key: "task_type_returns"}, KnowledgeRef: agents.Reference{Key: "persona_support"}, Position: 20}}
		withWiringApp(t, app)
		fsys := wiringFS()
		_, err := CreateKnowledge(fsys, TemplatesFS, wiringRoot, KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: "Return Terms", TaskTypeName: "returns"})
		if err != nil {
			t.Fatal(err)
		}
		links := string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")])
		if !strings.Contains(links, "position 30") {
			t.Fatalf("knowledge was not assigned the next position: %s", links)
		}
	})
	t.Run("reuses existing body file", func(t *testing.T) {
		fsys := wiringFS()
		path := filepath.Join(wiringRoot, "apps", wiringAppID, "knowledge", "return-terms.md")
		fsys.Files[path] = []byte("Keep this material")
		withWiringApp(t, appWithTask())
		result, err := CreateKnowledge(fsys, TemplatesFS, wiringRoot, KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: "Return Terms", TaskTypeName: "returns"})
		if err != nil || string(fsys.Files[path]) != "Keep this material" || len(result.FillIn) != 0 {
			t.Fatalf("existing body = %#v, %v", result, err)
		}
	})
	t.Run("template refusal leaves files untouched", func(t *testing.T) {
		fsys := wiringFS()
		withWiringApp(t, appWithTask())
		tpl := failingTemplateFS{TemplateFS: TemplatesFS, path: "templates/ai-agent/knowledge.md", err: errors.New("template unavailable")}
		_, err := CreateKnowledge(fsys, tpl, wiringRoot, KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: "Return Terms", TaskTypeName: "returns"})
		if err == nil || !strings.Contains(err.Error(), "template unavailable") || len(fsys.Files) != 1 {
			t.Fatalf("template refusal = %v", err)
		}
	})
}

func appWithReferenceKnowledge(linked bool) *agents.App {
	app := appWithTask()
	item := &agents.Knowledge{Key: "knowledge_return_terms", Title: "Return Terms", Body: agents.Body{Path: "knowledge/return-terms.md", Readable: true}}
	app.Knowledge = append(app.Knowledge, item)
	if linked {
		app.TaskTypeSkillLinks = append(app.TaskTypeSkillLinks, &agents.TaskTypeSkillLink{TaskTypeRef: agents.Reference{Key: "task_type_returns"}, KnowledgeRef: agents.Reference{Key: item.Key}, Position: 30})
	}
	return app
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
		{name: "platform tool needs task type", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "process-data"}, wantErr: "needs --task-type"},
		{name: "app tool without task type", app: appWithAction(false, false), cfg: ToolConfig{AppID: wiringAppID, Name: "find-order"}},
		{name: "new tool action without task type", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "summary-tool", DisplayName: "Summary Tool", Language: LanguageRust, ExecutionEnv: "server"}},
		{name: "existing action gets lesson", app: withAction, cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}},
		{name: "existing lesson gets missing tool link", app: appWithAction(true, false), cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}},
		{name: "existing lesson and links are a no-op", app: withExistingLesson, cfg: ToolConfig{AppID: wiringAppID, Name: "find-order", TaskTypeName: "returns"}},
		{name: "new action is marked tool", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", Description: "Find a customer.", TaskTypeName: "returns", Language: LanguageTypeScript, Scope: "acme", ExecutionEnv: "server"}},
		{name: "new Rust action is marked tool", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "summarize-order", DisplayName: "Summarize Order", TaskTypeName: "returns", Language: LanguageRust, ExecutionEnv: "server"}},
		{name: "new action missing display name", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", TaskTypeName: "returns"}, wantErr: "neither a platform tool nor an action"},
		{name: "new action without task type needs display name", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer"}, wantErr: "neither a platform tool nor an action"},
		{name: "new TypeScript tool missing scope", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns"}, wantErr: "needs --scope"},
		{name: "existing action path without record refuses", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: LanguageRust}, wantErr: "action folder"},
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
			case "app tool without task type":
				if len(got.Records) != 2 || strings.Contains(string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")]), "task_type_") {
					t.Fatalf("unlinked app tool records = %#v", got.Records)
				}
			case "new tool action without task type":
				if len(got.Records) != 3 || strings.Contains(string(fsys.Files[filepath.Join(wiringRoot, "apps", wiringAppID, "records", "72_agent_links.scl")]), "task_type_") {
					t.Fatalf("unlinked new tool records = %#v", got.Records)
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
				if !strings.Contains(source, "/// @tool") || !strings.Contains(source, "/// @shortdesc TODO(simple):") || !strings.Contains(source, "/// @usewhen") {
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
		{name: "Rust action has a plain handler summary", language: LanguageRust, actionName: "plain-rust"},
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
			if tt.language == LanguageRust && !tt.tool {
				for _, tag := range []string{"@tool", "@shortdesc", "@usewhen", "@parallelsafe"} {
					if strings.Contains(source, tag) {
						t.Fatalf("plain Rust action contains tool documentation %q", tag)
					}
				}
				if !strings.Contains(source, "/// Greets whoever the caller names and answers with the greeting. A blank name\n/// is refused.") {
					t.Fatal("plain Rust action is missing its handler summary")
				}
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
		{name: "bad name", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "Bad_Tool", TaskTypeName: "returns"}, want: `tool "Bad_Tool" has an invalid name`},
		{name: "description quote", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "process-data", Description: `a "quote"`, TaskTypeName: "returns"}, want: "double quote"},
		{name: "unsupported action language", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: "go"}, want: "unsupported language"},
		{name: "invalid action environment", app: base, cfg: ToolConfig{AppID: wiringAppID, Name: "lookup-customer", DisplayName: "Lookup Customer", TaskTypeName: "returns", Language: LanguageRust, ExecutionEnv: "edge"}, want: "unsupported environment"},
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
	if _, err := CreateAgent(fsx.OSFileSystem{}, TemplatesFS, collisionRoot, AgentConfig{AppID: wiringAppID, Name: "collision", DisplayName: "Collision Agent"}); err == nil || !strings.Contains(err.Error(), "record name it needs") {
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
	if _, err := CreateTaskType(fsys, TemplatesFS, root, TaskTypeConfig{AppID: wiringAppID, Name: "manual-review", PluralName: "Manual Reviews", NoAgent: true}); err != nil {
		t.Fatalf("create work for people: %v", err)
	}
	if _, err := CreateSkill(fsys, TemplatesFS, root, SkillConfig{AppID: wiringAppID, Name: "refund-policy", Title: "Refund Policy", TaskTypeName: "returns"}); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := CreateKnowledge(fsys, TemplatesFS, root, KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", Title: "Return Terms", TaskTypeName: "returns"}); err != nil {
		t.Fatalf("create reference knowledge: %v", err)
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
	if _, err := CreateTool(fsys, TemplatesFS, root, ToolConfig{AppID: wiringAppID, Name: "background-tool", DisplayName: "Background Tool", Language: LanguageRust, ExecutionEnv: "server"}); err != nil {
		t.Fatalf("create unlinked tool: %v", err)
	}

	app, err := agents.Load(fsys, root, wiringAppID)
	if err != nil {
		t.Fatalf("load generated records: %v", err)
	}
	canonical := app.Canonical()
	before := snapshotFiles(t, filepath.Join(root, "apps", wiringAppID))
	if result, repeatErr := CreateAgent(fsys, TemplatesFS, root, AgentConfig{AppID: wiringAppID, Name: "support", DisplayName: "Support Agent"}); repeatErr != nil || !result.NothingToAdd {
		t.Fatalf("repeat agent = %#v, %v; want a successful no-op", result, repeatErr)
	}
	if result, repeatErr := CreateTaskType(fsys, TemplatesFS, root, TaskTypeConfig{AppID: wiringAppID, Name: "returns", PluralName: "Returns"}); repeatErr != nil || !result.NothingToAdd || !contains(result.FillIn, "apps/com.acme.shop/records/task-types/returns.json") {
		t.Fatalf("repeat task type = %#v, %v; want a successful no-op", result, repeatErr)
	}
	if result, repeatErr := CreateTaskType(fsys, TemplatesFS, root, TaskTypeConfig{AppID: wiringAppID, Name: "manual-review", PluralName: "Manual Reviews", NoAgent: true}); repeatErr != nil || !result.NothingToAdd || !contains(result.FillIn, "apps/com.acme.shop/records/task-types/manual_review.json") {
		t.Fatalf("repeat work for people = %#v, %v; want a successful no-op", result, repeatErr)
	}
	if result, err := CreateSkill(fsys, TemplatesFS, root, SkillConfig{AppID: wiringAppID, Name: "refund-policy", TaskTypeName: "returns"}); err != nil || !result.NothingToAdd {
		t.Fatalf("repeat skill = %#v, %v", result, err)
	}
	if result, err := CreateKnowledge(fsys, TemplatesFS, root, KnowledgeConfig{AppID: wiringAppID, Name: "return-terms", TaskTypeName: "returns"}); err != nil || !result.NothingToAdd {
		t.Fatalf("repeat reference knowledge = %#v, %v", result, err)
	}
	if result, err := CreateTool(fsys, TemplatesFS, root, ToolConfig{AppID: wiringAppID, Name: "background-tool"}); err != nil || !result.NothingToAdd {
		t.Fatalf("repeat unlinked tool = %#v, %v", result, err)
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
		case strings.Contains(problem.Rule, "has not been built"):
			unbuiltProblems++
		default:
			t.Errorf("unexpected check problem: %s:%d: %s", problem.File, problem.Line, problem.Rule)
		}
	}
	if markerProblems == 0 || unbuiltProblems != 2 {
		t.Fatalf("agents.Check() reported %d marker findings and %d unbuilt tools: %#v", markerProblems, unbuiltProblems, problems)
	}
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

func appWithAgentDetails(displayName, description string) *agents.App {
	app := baseWiringApp()
	app.AgentRecords[0].DisplayName = displayName
	app.AgentRecords[0].Description = description
	return app
}

func appWithCompleteTask() *agents.App {
	app := baseWiringApp()
	task := &agents.TaskType{Key: "task_type_returns", Name: "returns", PluralName: "Returns", WakeIntervalSeconds: 90}
	task.DefaultAgent = app.AgentRecords[0]
	task.DefaultAgentRef = agents.Reference{Key: app.AgentRecords[0].Key, Readable: true}
	app.TaskTypes = []*agents.TaskType{task}
	app.AgentTaskTypeLinks = []*agents.AgentTaskTypeLink{{TaskTypeRef: agents.Reference{Key: task.Key}, AgentRef: agents.Reference{Key: app.AgentRecords[0].Key}, Role: "executor", IsDefault: true}}
	app.TaskTypeToolLinks = []*agents.TaskTypeToolLink{{TaskTypeRef: agents.Reference{Key: task.Key}, Tool: agents.ToolRef{AppID: agents.PlatformAppID, Name: "process-data"}}}
	return app
}

func appWithDescribedTask() *agents.App {
	app := appWithCompleteTask()
	app.TaskTypes[0].Description = "Reviews returns."
	return app
}

func appWithTwoDefaultAgents() *agents.App {
	app := appWithCompleteTask()
	app.AgentRecords = append(app.AgentRecords, &agents.Agent{Key: "agent_billing", DisplayName: "Billing Agent"})
	return app
}

func appWithNoAgentTask() *agents.App {
	app := appWithCompleteTask()
	app.TaskTypes[0].DefaultAgent = nil
	app.TaskTypes[0].DefaultAgentRef = agents.Reference{}
	app.AgentTaskTypeLinks = nil
	app.TaskTypeToolLinks = nil
	return app
}

func appWithWorkWithoutDefaultAgent() *agents.App {
	app := appWithCompleteTask()
	app.TaskTypes[0].DefaultAgent = nil
	app.TaskTypes[0].DefaultAgentRef = agents.Reference{}
	app.AgentTaskTypeLinks[0].IsDefault = false
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

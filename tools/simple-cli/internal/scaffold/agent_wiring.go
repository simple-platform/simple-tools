package scaffold

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"

	"simple-cli/internal/agents"
	"simple-cli/internal/agents/structure"
	"simple-cli/internal/fsx"
)

var loadAgentApp = agents.Load

type AgentConfig struct {
	AppID, Name, DisplayName, Description string
}

type TaskTypeConfig struct {
	AppID, Name, PluralName, Description, AgentName string
	Tools                                           []string
	WakeIntervalSeconds                             int64
}

type SkillConfig struct {
	AppID, Name, Title, Description, TaskTypeName string
}

type ToolConfig struct {
	AppID, Name, DisplayName, Description, TaskTypeName string
	Language, Scope, ExecutionEnv                       string
}

type WiringResult struct {
	Created      []string `json:"created"`
	Records      []string `json:"records"`
	FillIn       []string `json:"fill_in"`
	NothingToAdd bool     `json:"-"`
}

type plannedFile struct {
	path    string
	content []byte
}

type plannedRecords struct {
	path    string
	content []byte
	keys    []string
	existed bool
}

var kebabName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var taskTypeName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func CreateAgent(fsys fsx.FileSystem, tplFS fsx.TemplateFS, repoRoot string, cfg AgentConfig) (WiringResult, error) {
	app, appDir, err := loadAgentAppFor(fsys, repoRoot, cfg.AppID)
	if err != nil {
		return WiringResult{}, err
	}
	if _, exists := app.Agent(cfg.Name); exists {
		return WiringResult{}, fmt.Errorf("agent %q already exists in %s", cfg.Name, cfg.AppID)
	}
	if err := validateAgentConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	personaKey, agentKey := "persona_"+agents.Snake(cfg.Name), "agent_"+agents.Snake(cfg.Name)
	if err := ensureKeysFree(app, personaKey, agentKey); err != nil {
		return WiringResult{}, err
	}
	personaPath := filepath.Join(appDir, "knowledge", agents.Kebab(cfg.Name)+"-persona.md")
	persona, err := renderAgentTemplate(tplFS, "templates/ai-agent/persona.md", map[string]string{"DisplayName": cfg.DisplayName})
	if err != nil {
		return WiringResult{}, err
	}
	knowledge, err := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.scl", map[string]string{
		"Key": personaKey, "Title": cfg.DisplayName, "BodyPath": "knowledge/" + agents.Kebab(cfg.Name) + "-persona.md",
	})
	if err != nil {
		return WiringResult{}, err
	}
	agentRecord, err := renderAgentTemplate(tplFS, "templates/ai-agent/agent.scl", map[string]string{
		"Key": agentKey, "DisplayName": cfg.DisplayName, "Description": cfg.Description, "PersonaKey": personaKey,
	})
	if err != nil {
		return WiringResult{}, err
	}
	files := []plannedFile{{personaPath, []byte(persona)}}
	records, err := prepareRecords(fsys, appDir, []recordAddition{
		{file: "records/70_agent_knowledge.scl", header: knowledgeHeader, key: personaKey, text: knowledge},
		{file: "records/71_agents.scl", header: agentsHeader, key: agentKey, text: agentRecord},
	})
	if err != nil {
		return WiringResult{}, err
	}
	return executePlan(fsys, repoRoot, files, records)
}

func CreateTaskType(fsys fsx.FileSystem, tplFS fsx.TemplateFS, repoRoot string, cfg TaskTypeConfig) (WiringResult, error) {
	app, appDir, err := loadAgentAppFor(fsys, repoRoot, cfg.AppID)
	if err != nil {
		return WiringResult{}, err
	}
	if _, exists := app.TaskType(cfg.Name); exists {
		return WiringResult{}, fmt.Errorf("kind of work %q already exists in %s: use 'simple new skill' or 'simple new tool' to add to it", cfg.Name, cfg.AppID)
	}
	if err := validateTaskTypeConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	agent, err := selectAgent(app, cfg.AppID, cfg.AgentName)
	if err != nil {
		return WiringResult{}, err
	}
	validTools := make([]agents.ToolRef, 0, len(cfg.Tools))
	seenTools := make(map[string]bool, len(cfg.Tools))
	for _, name := range cfg.Tools {
		if seenTools[name] {
			continue
		}
		seenTools[name] = true
		if contains(agents.PlatformTools, name) {
			validTools = append(validTools, agents.ToolRef{AppID: agents.PlatformAppID, Name: name})
			continue
		}
		if action, ok := app.Action(name); ok {
			validTools = append(validTools, agents.ToolRef{ActionKey: action.Key, Name: action.Name, OwnAction: true})
			continue
		}
		return WiringResult{}, fmt.Errorf("tool %q does not exist in %s: create it with 'simple new tool %s %s \"%s\" --task-type %s --scope <org>'", name, cfg.AppID, cfg.AppID, name, name, cfg.Name)
	}
	nameKey := agents.Snake(cfg.Name)
	taskKey := "task_type_" + nameKey
	playbookKey := "playbook_" + nameKey
	defaultKey := taskKey + "_default_agent"
	personaLinkKey := taskKey + "_carries_persona"
	playbookLinkKey := taskKey + "_carries_playbook"
	keys := []string{playbookKey, taskKey, defaultKey, personaLinkKey, playbookLinkKey}
	toolKeys := make([]string, len(validTools))
	for i, tool := range validTools {
		toolKeys[i] = taskKey + "_may_call_" + agents.Snake(tool.Name)
		keys = append(keys, toolKeys[i])
	}
	if err := ensureKeysFree(app, keys...); err != nil {
		return WiringResult{}, err
	}
	if agent.Persona == nil {
		return WiringResult{}, fmt.Errorf("agent %q in %s has no readable persona: add its persona with 'simple new agent'", cfg.AgentName, cfg.AppID)
	}
	playbookPath := filepath.Join(appDir, "knowledge", agents.Kebab(cfg.Name)+"-playbook.md")
	shapePath := filepath.Join(appDir, "records", "task-types", nameKey+".json")
	statesPath := filepath.Join(appDir, "records", "task-types", "states.json")
	statesRepoPath, hasStates := app.StatesFile()
	if hasStates {
		statesPath = filepath.Join(repoRoot, filepath.FromSlash(statesRepoPath))
	}
	playbook, err := renderAgentTemplate(tplFS, "templates/ai-agent/playbook.md", nil)
	if err != nil {
		return WiringResult{}, err
	}
	if persona, readErr := readKnowledgeBody(fsys, appDir, agent.Persona); readErr == nil {
		personaProblems, playbookProblems := structure.CheckInstructions(agent.DisplayName, persona, playbook)
		if len(personaProblems) != 0 {
			return WiringResult{}, findingError(agent.Persona.Body.Path, personaProblems[0])
		}
		if len(playbookProblems) != 0 {
			return WiringResult{}, findingError("knowledge/"+agents.Kebab(cfg.Name)+"-playbook.md", playbookProblems[0])
		}
	}
	shape, err := tplFS.ReadFile("templates/ai-agent/task-type.json")
	if err != nil {
		return WiringResult{}, fmt.Errorf("failed to read template templates/ai-agent/task-type.json: %w", err)
	}
	states, err := tplFS.ReadFile("templates/ai-agent/states.json")
	if err != nil {
		return WiringResult{}, fmt.Errorf("failed to read template templates/ai-agent/states.json: %w", err)
	}
	files := []plannedFile{{playbookPath, []byte(playbook)}, {shapePath, shape}}
	if !PathExists(fsys, statesPath) {
		files = append(files, plannedFile{statesPath, states})
	}
	if PathExists(fsys, playbookPath) {
		files = removePlannedFile(files, playbookPath)
	}
	if PathExists(fsys, shapePath) {
		files = removePlannedFile(files, shapePath)
	}
	if PathExists(fsys, statesPath) {
		files = removePlannedFile(files, statesPath)
	}
	playbookRecord, err := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.scl", map[string]string{
		"Key": playbookKey, "Title": cfg.PluralName + " playbook", "BodyPath": "knowledge/" + agents.Kebab(cfg.Name) + "-playbook.md",
	})
	if err != nil {
		return WiringResult{}, err
	}
	data := map[string]string{
		"Key": taskKey, "Name": cfg.Name, "PluralName": cfg.PluralName, "Description": cfg.Description,
		"PlaybookKey": playbookKey, "ShapePath": "records/task-types/" + nameKey + ".json",
		"StatesPath":          filepath.ToSlash(strings.TrimPrefix(filepath.Clean(statesPath), filepath.Clean(appDir)+string(filepath.Separator))),
		"WakeIntervalSeconds": strconv.FormatInt(defaultWakeInterval(cfg.WakeIntervalSeconds), 10),
	}
	taskRecord, err := renderAgentTemplate(tplFS, "templates/ai-agent/task-type.scl", data)
	if err != nil {
		return WiringResult{}, err
	}
	defaultRecord, err := renderAgentTemplate(tplFS, "templates/ai-agent/agent_task_type.scl", map[string]string{
		"Key": defaultKey, "AgentKey": agent.Key, "TaskTypeKey": taskKey,
	})
	if err != nil {
		return WiringResult{}, err
	}
	personaLink, err := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_skill.scl", map[string]string{
		"Key": personaLinkKey, "TaskTypeKey": taskKey, "KnowledgeKey": agent.Persona.Key, "Position": "10",
	})
	if err != nil {
		return WiringResult{}, err
	}
	playbookLink, err := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_skill.scl", map[string]string{
		"Key": playbookLinkKey, "TaskTypeKey": taskKey, "KnowledgeKey": playbookKey, "Position": "20",
	})
	if err != nil {
		return WiringResult{}, err
	}
	adds := []recordAddition{
		{file: "records/70_agent_knowledge.scl", header: knowledgeHeader, key: playbookKey, text: playbookRecord},
		{file: "records/71_agents.scl", header: agentsHeader, key: taskKey, text: taskRecord},
		{file: "records/71_agents.scl", header: agentsHeader, key: defaultKey, text: defaultRecord},
		{file: "records/72_agent_links.scl", header: linksHeader, key: personaLinkKey, text: personaLink},
		{file: "records/72_agent_links.scl", header: linksHeader, key: playbookLinkKey, text: playbookLink},
	}
	for _, tool := range validTools {
		if !tool.OwnAction && !app.HasLookup(filepath.Join(appDir, "records", "72_agent_links.scl"), "platform_tool_"+agents.Snake(tool.Name)) {
			lookupText, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/platform_tool_lookup.scl", map[string]string{"Name": tool.Name, "VarName": agents.Snake(tool.Name)})
			if renderErr != nil {
				return WiringResult{}, renderErr
			}
			adds = append(adds, recordAddition{file: "records/72_agent_links.scl", header: linksHeader,
				key: "platform_tool_" + agents.Snake(tool.Name), text: lookupText, isLookup: true})
		}
	}
	for i, tool := range validTools {
		logicRef := `$app_record("` + tool.ActionKey + `")`
		if !tool.OwnAction {
			logicRef = `$var('platform_tool_` + agents.Snake(tool.Name) + `') |> $jq('.logic[0].id')`
		}
		text, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_tool.scl", map[string]string{
			"Key": toolKeys[i], "TaskTypeKey": taskKey, "LogicRef": logicRef,
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/72_agent_links.scl", header: linksHeader, key: toolKeys[i], text: text})
	}
	records, err := prepareRecords(fsys, appDir, adds)
	if err != nil {
		return WiringResult{}, err
	}
	return executePlan(fsys, repoRoot, files, records)
}

func CreateSkill(fsys fsx.FileSystem, tplFS fsx.TemplateFS, repoRoot string, cfg SkillConfig) (WiringResult, error) {
	app, appDir, err := loadAgentAppFor(fsys, repoRoot, cfg.AppID)
	if err != nil {
		return WiringResult{}, err
	}
	task, ok := app.TaskType(cfg.TaskTypeName)
	if !ok {
		return WiringResult{}, missingTaskType(cfg.AppID, cfg.TaskTypeName)
	}
	if err := validateSkillConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	knowledge, exists := app.Skill(cfg.Name)
	filePath := filepath.Join(appDir, "knowledge", "skill-"+agents.Kebab(cfg.Name)+".md")
	knowledgeKey := "skill_" + agents.Snake(cfg.Name)
	position := app.NextSkillPosition(task.Key)
	linkKey := "task_type_" + agents.Snake(task.Name) + "_carries_skill_" + agents.Snake(cfg.Name)
	needsKnowledge := !exists
	if needsKnowledge {
		if cfg.Title == "" {
			return WiringResult{}, fmt.Errorf("a title is required for new skill %q: add \"<Title>\" to 'simple new skill'", cfg.Name)
		}
		if err := ensureKeysFree(app, knowledgeKey); err != nil {
			return WiringResult{}, err
		}
	} else {
		knowledgeKey = knowledge.Key
	}
	needsLink := !app.HasSkillLink(task.Key, knowledgeKey)
	if needsLink {
		if err := ensureKeysFree(app, linkKey); err != nil {
			return WiringResult{}, err
		}
	}
	if !needsKnowledge && !needsLink {
		return WiringResult{Created: []string{}, Records: []string{}, FillIn: []string{}, NothingToAdd: true}, nil
	}
	files := make([]plannedFile, 0, 1)
	adds := make([]recordAddition, 0, 2)
	if needsKnowledge {
		purpose := cfg.Description
		if purpose == "" {
			purpose = "TODO(simple): Say in one paragraph what this skill helps the agent do, and why it matters."
		}
		content, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/skill.md", map[string]string{
			"Title": cfg.Title, "Name": cfg.Name, "Purpose": purpose,
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		findings := structure.CheckSkill(content)
		if len(findings) != 0 {
			return WiringResult{}, findingError("knowledge/skill-"+agents.Kebab(cfg.Name)+".md", findings[0])
		}
		if !PathExists(fsys, filePath) {
			files = append(files, plannedFile{filePath, []byte(content)})
		}
		if _, _, taken := app.KeyTaken(knowledgeKey); !taken {
			text, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.scl", map[string]string{
				"Key": knowledgeKey, "Title": cfg.Title, "BodyPath": "knowledge/skill-" + agents.Kebab(cfg.Name) + ".md",
			})
			if renderErr != nil {
				return WiringResult{}, renderErr
			}
			adds = append(adds, recordAddition{file: "records/70_agent_knowledge.scl", header: knowledgeHeader, key: knowledgeKey, text: text})
		}
	}
	if needsLink {
		text, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_skill.scl", map[string]string{
			"Key": linkKey, "TaskTypeKey": task.Key, "KnowledgeKey": knowledgeKey, "Position": strconv.Itoa(position),
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/72_agent_links.scl", header: linksHeader, key: linkKey, text: text})
	}
	records, err := prepareRecords(fsys, appDir, adds)
	if err != nil {
		return WiringResult{}, err
	}
	return executePlan(fsys, repoRoot, files, records)
}

func CreateTool(fsys fsx.FileSystem, tplFS fsx.TemplateFS, repoRoot string, cfg ToolConfig) (WiringResult, error) {
	app, appDir, err := loadAgentAppFor(fsys, repoRoot, cfg.AppID)
	if err != nil {
		return WiringResult{}, err
	}
	task, ok := app.TaskType(cfg.TaskTypeName)
	if !ok {
		return WiringResult{}, missingTaskType(cfg.AppID, cfg.TaskTypeName)
	}
	if err := validateToolConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	_, platform := stringIn(agents.PlatformTools, cfg.Name)
	action, ownAction := app.Action(cfg.Name)
	if !platform && !ownAction && cfg.DisplayName == "" {
		return WiringResult{}, fmt.Errorf("%q is neither a platform tool nor an action in %s: create it with 'simple new tool %s %s \"<Display name>\" --task-type %s --scope <org>'", cfg.Name, cfg.AppID, cfg.AppID, cfg.Name, cfg.TaskTypeName)
	}
	if !platform && !ownAction {
		if err := validateActionConfigForTool(cfg); err != nil {
			return WiringResult{}, err
		}
	}
	tool := agents.ToolRef{AppID: agents.PlatformAppID, Name: cfg.Name}
	if ownAction {
		tool = agents.ToolRef{ActionKey: action.Key, Name: action.Name, OwnAction: true}
	}
	linkKey := "task_type_" + agents.Snake(task.Name) + "_may_call_" + agents.Snake(cfg.Name)
	needsLink := !app.HasToolLink(task.Key, tool)
	hasLesson := false
	if ownAction {
		_, hasLesson = app.Lesson(action.Key)
	}
	newAction := !platform && !ownAction
	needsLesson := (ownAction && !hasLesson) || newAction
	needsLookup := platform && !app.HasLookup(filepath.Join(appDir, "records", "72_agent_links.scl"), "platform_tool_"+agents.Snake(cfg.Name))
	if needsLink {
		if err := ensureKeysFree(app, linkKey); err != nil {
			return WiringResult{}, err
		}
	}
	lessonKey, lessonLinkKey := "tool_"+agents.Snake(cfg.Name), "tool_"+agents.Snake(cfg.Name)+"_lesson"
	if needsLesson {
		if err := ensureKeysFree(app, lessonKey, lessonLinkKey); err != nil {
			return WiringResult{}, err
		}
	}
	actionKey := agents.Snake(cfg.Name)
	if ownAction {
		actionKey = action.Key
	}
	if newAction {
		if err := ensureKeysFree(app, actionKey); err != nil {
			return WiringResult{}, err
		}
		if PathExists(fsys, filepath.Join(appDir, "actions", cfg.Name)) {
			return WiringResult{}, fmt.Errorf("action path for %q already exists in %s", cfg.Name, cfg.AppID)
		}
	}
	if !needsLink && !needsLesson && !needsLookup && !newAction {
		return WiringResult{Created: []string{}, Records: []string{}, FillIn: []string{}, NothingToAdd: true}, nil
	}
	files := make([]plannedFile, 0, 2)
	adds := make([]recordAddition, 0, 4)
	if newAction {
		action = &agents.Action{Key: actionKey, Name: cfg.Name, DisplayName: cfg.DisplayName, Description: cfg.Description}
	}
	if newAction {
		if cfg.Language == "" {
			cfg.Language = LanguageTypeScript
		}
		if cfg.Language != LanguageTypeScript && cfg.Language != LanguageRust {
			return WiringResult{}, fmt.Errorf("unsupported action language: %s", cfg.Language)
		}
		if cfg.ExecutionEnv == "" {
			cfg.ExecutionEnv = "server"
		}
		if cfg.Language == LanguageTypeScript && cfg.Scope == "" {
			return WiringResult{}, fmt.Errorf("--scope is required (e.g., --scope mycompany)")
		}
		if !contains(validToolExecutionEnvs, cfg.ExecutionEnv) {
			return WiringResult{}, fmt.Errorf("invalid execution environment: %s. Valid values: server, client, both", cfg.ExecutionEnv)
		}
		data := map[string]string{"ActionName": cfg.Name, "ActionNameScl": actionKey, "DisplayName": cfg.DisplayName,
			"Description": cfg.Description, "Scope": cfg.Scope, "ExecutionEnv": cfg.ExecutionEnv, "Language": cfg.Language, "Tool": "true"}
		actionRecord, renderErr := renderAgentTemplate(tplFS, "templates/action/10_actions.scl", data)
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/10_actions.scl", header: actionHeader, key: actionKey, text: actionRecord})
	}
	if needsLesson {
		title := action.DisplayName
		if title == "" {
			title = cfg.DisplayName
		}
		if utf8.RuneCountInString(title) > 255 {
			return WiringResult{}, fmt.Errorf("knowledge title is longer than 255 characters")
		}
		purpose := cfg.Description
		if purpose == "" {
			purpose = "TODO(simple): Say in one paragraph what this tool does and which fact or change it gives the agent."
		}
		lessonText, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/tool-lesson.md", map[string]string{
			"DisplayName": title, "Name": cfg.Name, "Purpose": purpose,
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		findings := structure.CheckSkill(lessonText)
		if len(findings) != 0 {
			return WiringResult{}, findingError("knowledge/tool-"+agents.Kebab(cfg.Name)+".md", findings[0])
		}
		lessonPath := filepath.Join(appDir, "knowledge", "tool-"+agents.Kebab(cfg.Name)+".md")
		if !PathExists(fsys, lessonPath) {
			files = append(files, plannedFile{lessonPath, []byte(lessonText)})
		}
		knowledgeRecord, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.scl", map[string]string{
			"Key": lessonKey, "Title": title, "BodyPath": "knowledge/tool-" + agents.Kebab(cfg.Name) + ".md",
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		lessonLink, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/logic_skill.scl", map[string]string{
			"Key": lessonLinkKey, "LogicKey": action.Key, "KnowledgeKey": lessonKey,
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/70_agent_knowledge.scl", header: knowledgeHeader, key: lessonKey, text: knowledgeRecord})
		adds = append(adds, recordAddition{file: "records/72_agent_links.scl", header: linksHeader, key: lessonLinkKey, text: lessonLink})
	}
	if needsLink {
		logicRef := ""
		if platform {
			logicRef = `$var('platform_tool_` + agents.Snake(cfg.Name) + `') |> $jq('.logic[0].id')`
		} else {
			logicRef = `$app_record("` + action.Key + `")`
		}
		link, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_tool.scl", map[string]string{
			"Key": linkKey, "TaskTypeKey": task.Key, "LogicRef": logicRef,
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/72_agent_links.scl", header: linksHeader, key: linkKey, text: link})
	}
	if needsLookup {
		lookupText, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/platform_tool_lookup.scl", map[string]string{"Name": cfg.Name, "VarName": agents.Snake(cfg.Name)})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append([]recordAddition{{file: "records/72_agent_links.scl", header: linksHeader, key: "platform_tool_" + agents.Snake(cfg.Name), text: lookupText, isLookup: true}}, adds...)
	}
	records, err := prepareRecords(fsys, appDir, adds)
	if err != nil {
		return WiringResult{}, err
	}
	if newAction {
		actionConfig := ActionConfig{AppID: cfg.AppID, ActionName: cfg.Name, DisplayName: cfg.DisplayName, Description: cfg.Description, Scope: cfg.Scope,
			ExecutionEnv: cfg.ExecutionEnv, Language: cfg.Language, Tool: true}
		if err := createActionStructure(fsys, tplFS, repoRoot, actionConfig, true); err != nil {
			return WiringResult{}, fmt.Errorf("failed to create tool action: %w", err)
		}
	}
	result, err := executePlan(fsys, repoRoot, files, records)
	if err != nil {
		return result, err
	}
	if newAction {
		for _, path := range actionFilePaths(appDir, cfg.Name, cfg.Language) {
			result.Created = append(result.Created, relativeToRoot(repoRoot, path))
			content, readErr := fsys.ReadFile(path)
			if readErr == nil && bytes.Contains(content, []byte(agents.Marker)) {
				result.FillIn = append(result.FillIn, relativeToRoot(repoRoot, path))
			}
		}
		sort.Strings(result.Created)
		sort.Strings(result.FillIn)
	}
	return result, nil
}

var validToolExecutionEnvs = []string{"server", "client", "both"}

const (
	knowledgeHeader = "# This file holds agent knowledge records.\n# simple new wrote this file; the Markdown files are where the words go."
	agentsHeader    = "# This file holds agents and kinds of work.\n# simple new wrote this file; the Markdown files are where the words go."
	linksHeader     = "# This file holds links between kinds of work, skills, and tools.\n# simple new wrote this file; the Markdown files are where the words go."
	actionHeader    = "# This file holds app actions.\n# simple new wrote this file; the Markdown files are where the words go."
)

type recordAddition struct {
	file, header, key, text string
	isLookup                bool
}

func loadAgentAppFor(fsys fsx.FileSystem, root, appID string) (*agents.App, string, error) {
	appDir := filepath.Join(root, "apps", filepath.FromSlash(appID))
	if !PathExists(fsys, appDir) {
		return nil, "", fmt.Errorf("app %s does not exist: run 'simple new app %s \"<Display name>\"' first", appID, appID)
	}
	app, err := loadAgentApp(fsys, root, appID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read app %s records: %w", appID, err)
	}
	return app, appDir, nil
}

func validateAgentConfig(cfg AgentConfig) error {
	if !kebabName.MatchString(cfg.Name) {
		return fmt.Errorf("invalid agent name %q: use lowercase letters, digits, and hyphens, starting with a letter", cfg.Name)
	}
	if utf8.RuneCountInString(cfg.DisplayName) > 100 {
		return fmt.Errorf("agent display name is longer than 100 characters")
	}
	return validateScaffoldText("display name", cfg.DisplayName, 100)
}

func validateTaskTypeConfig(cfg TaskTypeConfig) error {
	if !taskTypeName.MatchString(cfg.Name) {
		return fmt.Errorf("invalid task-type name %q: use lowercase letters, digits, hyphens, or underscores, starting with a letter", cfg.Name)
	}
	if len(cfg.Name) > 100 {
		return fmt.Errorf("task-type name is longer than 100 characters")
	}
	if utf8.RuneCountInString(cfg.PluralName) > 100 {
		return fmt.Errorf("plural name is longer than 100 characters")
	}
	if err := validateScaffoldText("plural name", cfg.PluralName, 100); err != nil {
		return err
	}
	if err := validateScaffoldText("description", cfg.Description, 1000); err != nil {
		return err
	}
	if cfg.WakeIntervalSeconds < 0 {
		return fmt.Errorf("--every must be at least one whole second")
	}
	for _, name := range cfg.Tools {
		if !kebabName.MatchString(name) {
			return fmt.Errorf("invalid tool name %q in --tools", name)
		}
	}
	return nil
}

func validateSkillConfig(cfg SkillConfig) error {
	if !kebabName.MatchString(cfg.Name) {
		return fmt.Errorf("invalid skill name %q: use lowercase letters, digits, and hyphens, starting with a letter", cfg.Name)
	}
	if utf8.RuneCountInString(cfg.Title) > 255 {
		return fmt.Errorf("knowledge title is longer than 255 characters")
	}
	if cfg.Title != "" {
		if err := validateScaffoldText("title", cfg.Title, 255); err != nil {
			return err
		}
	}
	return validateScaffoldText("description", cfg.Description, 1000)
}

func validateToolConfig(cfg ToolConfig) error {
	if !kebabName.MatchString(cfg.Name) {
		return fmt.Errorf("invalid tool name %q: use lowercase letters, digits, and hyphens, starting with a letter", cfg.Name)
	}
	if utf8.RuneCountInString(cfg.DisplayName) > 100 {
		return fmt.Errorf("display name is longer than 100 characters")
	}
	if cfg.DisplayName != "" {
		if err := validateScaffoldText("display name", cfg.DisplayName, 100); err != nil {
			return err
		}
	}
	return validateScaffoldText("description", cfg.Description, 1000)
}

func validateActionConfigForTool(cfg ToolConfig) error {
	if cfg.DisplayName == "" {
		return fmt.Errorf("display name is required when creating an app tool action")
	}
	if err := validateScaffoldText("display name", cfg.DisplayName, 100); err != nil {
		return err
	}
	return nil
}

func validateScaffoldText(field, value string, limit int) error {
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("%s must be a single line", field)
	}
	if strings.Contains(value, `"`) {
		return fmt.Errorf("%s must not contain a double quote", field)
	}
	if utf8.RuneCountInString(value) > limit {
		return fmt.Errorf("%s is longer than %d characters", field, limit)
	}
	return nil
}

func selectAgent(app *agents.App, appID, wanted string) (*agents.Agent, error) {
	if wanted != "" {
		if agent, ok := app.Agent(wanted); ok {
			return agent, nil
		}
		return nil, fmt.Errorf("agent %q does not exist in %s: create it with 'simple new agent'", wanted, appID)
	}
	agentsList := app.Agents()
	if len(agentsList) == 1 {
		return agentsList[0], nil
	}
	if len(agentsList) == 0 {
		return nil, fmt.Errorf("%s has no agent yet: run 'simple new agent %s <name> \"<Display name>\"' first", appID, appID)
	}
	names := make([]string, 0, len(agentsList))
	for _, item := range agentsList {
		name := strings.TrimPrefix(item.Key, "agent_")
		if name == "" {
			name = item.Key
		}
		names = append(names, agents.Kebab(name))
	}
	sort.Strings(names)
	return nil, fmt.Errorf("%s has %d agents (%s): say which with --agent", appID, len(agentsList), strings.Join(names, ", "))
}

func ensureKeysFree(app *agents.App, keys ...string) error {
	for _, key := range keys {
		if file, line, ok := app.KeyTaken(key); ok {
			return fmt.Errorf("record key %q is already used at %s:%d", key, file, line)
		}
	}
	return nil
}

func missingTaskType(appID, name string) error {
	return fmt.Errorf("kind of work %q does not exist in %s: create it with 'simple new task-type'", name, appID)
}

func renderAgentTemplate(tplFS fsx.TemplateFS, path string, data any) (string, error) {
	content, err := tplFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read template %s: %w", path, err)
	}
	tmpl, err := template.New(filepath.Base(path)).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", path, err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return "", fmt.Errorf("failed to render template %s: %w", path, err)
	}
	return rendered.String(), nil
}

func readKnowledgeBody(fsys fsx.FileSystem, appDir string, knowledge *agents.Knowledge) (string, error) {
	if knowledge.Body.Path != "" {
		body, err := fsys.ReadFile(filepath.Join(appDir, filepath.FromSlash(knowledge.Body.Path)))
		return string(body), err
	}
	if knowledge.Body.Readable {
		return knowledge.Body.Inline, nil
	}
	return "", fmt.Errorf("knowledge body is unreadable")
}

func findingError(file string, finding structure.Finding) error {
	return fmt.Errorf("%s:%d: %s", file, finding.Line, finding.Rule)
}

func defaultWakeInterval(seconds int64) int64 {
	if seconds == 0 {
		return 3600
	}
	return seconds
}

func prepareRecords(fsys fsx.FileSystem, appDir string, additions []recordAddition) ([]plannedRecords, error) {
	groups := make(map[string][]recordAddition)
	order := make([]string, 0)
	for _, addition := range additions {
		if _, ok := groups[addition.file]; !ok {
			order = append(order, addition.file)
		}
		groups[addition.file] = append(groups[addition.file], addition)
	}
	result := make([]plannedRecords, 0, len(order))
	for _, name := range order {
		path := filepath.Join(appDir, filepath.FromSlash(name))
		var existing []byte
		existed := PathExists(fsys, path)
		if existed {
			content, err := fsys.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to read existing records file %s: %w", name, err)
			}
			existing = content
		}
		var buffer bytes.Buffer
		if len(existing) == 0 {
			buffer.WriteString(groups[name][0].header)
			buffer.WriteString("\n\n")
		} else {
			buffer.Write(existing)
			if !bytes.HasSuffix(existing, []byte("\n")) {
				buffer.WriteByte('\n')
			}
			buffer.WriteByte('\n')
		}
		keys := make([]string, 0, len(groups[name]))
		for index, addition := range groups[name] {
			if index != 0 {
				buffer.WriteByte('\n')
			}
			buffer.WriteString(addition.text)
			if !strings.HasSuffix(addition.text, "\n") {
				buffer.WriteByte('\n')
			}
			if !addition.isLookup {
				keys = append(keys, addition.key)
			}
		}
		result = append(result, plannedRecords{path: path, content: buffer.Bytes(), keys: keys, existed: existed})
	}
	return result, nil
}

func executePlan(fsys fsx.FileSystem, repoRoot string, files []plannedFile, records []plannedRecords) (WiringResult, error) {
	result := WiringResult{Created: []string{}, Records: []string{}, FillIn: []string{}}
	for _, file := range files {
		if PathExists(fsys, file.path) {
			continue
		}
		if err := fsys.MkdirAll(filepath.Dir(file.path), fsx.DirPerm); err != nil {
			return WiringResult{}, fmt.Errorf("failed to create directory for %s: %w", file.path, err)
		}
		if err := fsys.WriteFile(file.path, file.content, fsx.FilePerm); err != nil {
			return WiringResult{}, fmt.Errorf("failed to write %s: %w", file.path, err)
		}
		result.Created = append(result.Created, relativeToRoot(repoRoot, file.path))
		if bytes.Contains(file.content, []byte(agents.Marker)) {
			result.FillIn = append(result.FillIn, relativeToRoot(repoRoot, file.path))
		}
	}
	for _, recordsFile := range records {
		if err := fsys.MkdirAll(filepath.Dir(recordsFile.path), fsx.DirPerm); err != nil {
			return WiringResult{}, fmt.Errorf("failed to create records directory: %w", err)
		}
		if err := fsys.WriteFile(recordsFile.path, recordsFile.content, fsx.FilePerm); err != nil {
			return WiringResult{}, fmt.Errorf("failed to write records file %s: %w", recordsFile.path, err)
		}
		if !recordsFile.existed {
			result.Created = append(result.Created, relativeToRoot(repoRoot, recordsFile.path))
		}
		result.Records = append(result.Records, recordsFile.keys...)
	}
	sort.Strings(result.Created)
	sort.Strings(result.FillIn)
	return result, nil
}

func relativeToRoot(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func removePlannedFile(files []plannedFile, path string) []plannedFile {
	result := files[:0]
	for _, file := range files {
		if file.path != path {
			result = append(result, file)
		}
	}
	return result
}

func actionFilePaths(appDir, name, language string) []string {
	actionDir := filepath.Join(appDir, "actions", name)
	files := []string{
		filepath.Join(actionDir, "package.json"), filepath.Join(actionDir, "src", "index.ts"), filepath.Join(actionDir, "tsconfig.json"),
		filepath.Join(actionDir, "tsdoc.json"), filepath.Join(actionDir, "vitest.config.ts"), filepath.Join(actionDir, "tests", "helpers.ts"),
		filepath.Join(actionDir, "tests", "index.test.ts"),
	}
	if language == LanguageRust {
		files = []string{filepath.Join(actionDir, "Cargo.toml"), filepath.Join(actionDir, "src", "main.rs"), filepath.Join(actionDir, ".gitignore")}
	}
	return files
}

func contains(values []string, value string) bool {
	_, ok := stringIn(values, value)
	return ok
}

func stringIn(values []string, value string) (int, bool) {
	for i, candidate := range values {
		if candidate == value {
			return i, true
		}
	}
	return 0, false
}

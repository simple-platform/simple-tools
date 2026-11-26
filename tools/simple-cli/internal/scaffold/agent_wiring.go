package scaffold

import (
	"bytes"
	"fmt"
	"os"
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
var findPlatformToolLookup = func(app *agents.App, file, name string) (agents.LookupInfo, bool) {
	return app.Lookup(file, name)
}

type AgentConfig struct {
	AppID, Name, DisplayName, Description string
}

type TaskTypeConfig struct {
	AppID, Name, PluralName, Description, AgentName string
	Tools                                           []string
	WakeIntervalSeconds                             int64
	NoAgent                                         bool
	AgentFlagSet, ToolsFlagSet, EveryFlagSet        bool
}

type SkillConfig struct {
	AppID, Name, Title, Description, TaskTypeName string
}

type KnowledgeConfig struct {
	AppID, Name, Title, TaskTypeName string
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
	path      string
	content   []byte
	reuseOnly bool
}

type plannedRecords struct {
	path    string
	content []byte
	keys    []string
	existed bool
}

var kebabName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var taskTypeName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)

func CreateAgent(fsys fsx.FileSystem, tplFS fsx.TemplateFS, repoRoot string, cfg AgentConfig) (WiringResult, error) {
	app, appDir, err := loadAgentAppFor(fsys, repoRoot, cfg.AppID)
	if err != nil {
		return WiringResult{}, err
	}
	if _, exists := app.Agent(cfg.Name); exists {
		return WiringResult{}, fmt.Errorf("agent %q already exists in %s; choose a different name", cfg.Name, cfg.AppID)
	}
	if err := validateAgentConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	personaKey, agentKey := "persona_"+agents.Snake(cfg.Name), "agent_"+agents.Snake(cfg.Name)
	if err := ensureKeysFree(app, "agent", cfg.Name, personaKey, agentKey); err != nil {
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
	files := []plannedFile{{path: personaPath, content: []byte(persona)}}
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
		return WiringResult{}, fmt.Errorf("kind of work %q already exists in %s; add to it with 'simple new skill' or 'simple new tool'", cfg.Name, cfg.AppID)
	}
	if err := validateTaskTypeConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	if cfg.NoAgent && (cfg.AgentFlagSet || cfg.ToolsFlagSet || cfg.EveryFlagSet || cfg.AgentName != "" || len(cfg.Tools) != 0) {
		return WiringResult{}, fmt.Errorf("kind of work %q cannot use --no-agent with --agent, --tools, or --every; remove those flags", cfg.Name)
	}
	var agent *agents.Agent
	if !cfg.NoAgent {
		agent, err = selectAgent(app, cfg.AppID, cfg.AgentName, cfg.Name)
		if err != nil {
			return WiringResult{}, err
		}
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
		return WiringResult{}, fmt.Errorf("tool %q does not exist in %s; create it first with 'simple new tool %s %s \"<Display name>\" --lang ts --scope <org>' without --task-type, then run this command again", name, cfg.AppID, cfg.AppID, name)
	}
	nameKey := agents.Snake(cfg.Name)
	taskKey := "task_type_" + nameKey
	playbookKey := ""
	keys := []string{taskKey}
	if !cfg.NoAgent {
		playbookKey = "playbook_" + nameKey
		keys = append(keys, playbookKey, taskKey+"_default_agent", taskKey+"_carries_persona", taskKey+"_carries_playbook")
	}
	toolKeys := make([]string, len(validTools))
	for i, tool := range validTools {
		toolKeys[i] = taskKey + "_may_call_" + agents.Snake(tool.Name)
		keys = append(keys, toolKeys[i])
	}
	if err := ensureKeysFree(app, "kind of work", cfg.Name, keys...); err != nil {
		return WiringResult{}, err
	}
	if agent != nil && agent.Persona == nil {
		return WiringResult{}, fmt.Errorf("agent %q in %s has no readable persona; point it to knowledge with a readable body before adding kind of work %q", agent.DisplayName, cfg.AppID, cfg.Name)
	}
	shapePath := filepath.Join(appDir, "records", "task-types", nameKey+".json")
	statesPath := filepath.Join(appDir, "records", "task-types", "states.json")
	statesRepoPath, hasStates := app.StatesFile()
	if hasStates {
		statesPath = filepath.Join(repoRoot, filepath.FromSlash(statesRepoPath))
	}
	files := make([]plannedFile, 0, 3)
	if !cfg.NoAgent {
		playbookPath := filepath.Join(appDir, "knowledge", "playbook-"+agents.Kebab(cfg.Name)+".md")
		playbook, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/playbook.md", nil)
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		if PathExists(fsys, playbookPath) {
			existing, readErr := fsys.ReadFile(playbookPath)
			if readErr != nil {
				return WiringResult{}, fmt.Errorf("failed to read existing playbook %s: %w", playbookPath, readErr)
			}
			playbook = string(existing)
			files = append(files, plannedFile{path: playbookPath, reuseOnly: true})
		} else {
			files = append(files, plannedFile{path: playbookPath, content: []byte(playbook)})
		}
		if persona, readErr := readKnowledgeBody(fsys, appDir, agent.Persona); readErr == nil {
			personaProblems, playbookProblems := structure.CheckInstructions(agent.DisplayName, persona, playbook)
			if len(personaProblems) != 0 {
				return WiringResult{}, findingError(agent.Persona.Body.Path, personaProblems[0])
			}
			if len(playbookProblems) != 0 {
				return WiringResult{}, findingError("knowledge/playbook-"+agents.Kebab(cfg.Name)+".md", playbookProblems[0])
			}
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
	files = append(files, plannedFile{path: shapePath, content: shape})
	files = append(files, plannedFile{path: statesPath, content: states})
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
	adds := []recordAddition{{file: "records/71_agents.scl", header: agentsHeader, key: taskKey, text: taskRecord}}
	if !cfg.NoAgent {
		playbookPath := "knowledge/playbook-" + agents.Kebab(cfg.Name) + ".md"
		playbookRecord, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.scl", map[string]string{
			"Key": playbookKey, "Title": cfg.PluralName + " playbook", "BodyPath": playbookPath,
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		defaultKey := taskKey + "_default_agent"
		defaultRecord, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/agent_task_type.scl", map[string]string{
			"Key": defaultKey, "AgentKey": agent.Key, "TaskTypeKey": taskKey,
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		personaLink, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_skill.scl", map[string]string{
			"Key": taskKey + "_carries_persona", "TaskTypeKey": taskKey, "KnowledgeKey": agent.Persona.Key, "Position": "10",
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		playbookLink, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_skill.scl", map[string]string{
			"Key": taskKey + "_carries_playbook", "TaskTypeKey": taskKey, "KnowledgeKey": playbookKey, "Position": "20",
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds,
			recordAddition{file: "records/70_agent_knowledge.scl", header: knowledgeHeader, key: playbookKey, text: playbookRecord},
			recordAddition{file: "records/71_agents.scl", header: agentsHeader, key: defaultKey, text: defaultRecord},
			recordAddition{file: "records/72_agent_links.scl", header: linksHeader, key: taskKey + "_carries_persona", text: personaLink},
			recordAddition{file: "records/72_agent_links.scl", header: linksHeader, key: taskKey + "_carries_playbook", text: playbookLink},
		)
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
		if err := ensureKeysFree(app, "skill", cfg.Name, knowledgeKey); err != nil {
			return WiringResult{}, err
		}
	} else {
		knowledgeKey = knowledge.Key
	}
	needsLink := !app.HasSkillLink(task.Key, knowledgeKey)
	if needsLink {
		if err := ensureKeysFree(app, "skill", cfg.Name, linkKey); err != nil {
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
		files = append(files, plannedFile{path: filePath, content: []byte(content)})
		if _, _, taken := app.KeyTaken(knowledgeKey); !taken {
			text, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.scl", map[string]string{
				"Key": knowledgeKey, "Title": cfg.Title, "BodyPath": "knowledge/skill-" + agents.Kebab(cfg.Name) + ".md",
			})
			if renderErr != nil {
				return WiringResult{}, renderErr
			}
			adds = append(adds, recordAddition{file: "records/70_agent_knowledge.scl", header: knowledgeHeader, key: knowledgeKey, text: text})
		}
	} else if needsLink && knowledge.Body.Path != "" {
		files = append(files, plannedFile{path: filepath.Join(appDir, filepath.FromSlash(knowledge.Body.Path)), reuseOnly: true})
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

func CreateKnowledge(fsys fsx.FileSystem, tplFS fsx.TemplateFS, repoRoot string, cfg KnowledgeConfig) (WiringResult, error) {
	app, appDir, err := loadAgentAppFor(fsys, repoRoot, cfg.AppID)
	if err != nil {
		return WiringResult{}, err
	}
	task, ok := app.TaskType(cfg.TaskTypeName)
	if !ok {
		return WiringResult{}, missingTaskType(cfg.AppID, cfg.TaskTypeName)
	}
	if err := validateKnowledgeConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	knowledge, exists := app.ReferenceKnowledge(cfg.Name)
	knowledgeKey := "knowledge_" + agents.Snake(cfg.Name)
	filePath := filepath.Join(appDir, "knowledge", agents.Kebab(cfg.Name)+".md")
	if exists {
		knowledgeKey = knowledge.Key
	} else if cfg.Title == "" {
		return WiringResult{}, fmt.Errorf("reference knowledge %q needs a title: add \"<Title>\" to 'simple new knowledge'", cfg.Name)
	}
	linkKey := "task_type_" + agents.Snake(task.Name) + "_carries_knowledge_" + agents.Snake(cfg.Name)
	needsLink := !app.HasSkillLink(task.Key, knowledgeKey)
	if !exists {
		if err := ensureKeysFree(app, "reference knowledge", cfg.Name, knowledgeKey); err != nil {
			return WiringResult{}, err
		}
	}
	if needsLink {
		if err := ensureKeysFree(app, "reference knowledge", cfg.Name, linkKey); err != nil {
			return WiringResult{}, err
		}
	}
	if exists && !needsLink {
		return WiringResult{Created: []string{}, Records: []string{}, FillIn: []string{}, NothingToAdd: true}, nil
	}
	files := make([]plannedFile, 0, 1)
	adds := make([]recordAddition, 0, 2)
	if !exists {
		content, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.md", map[string]string{"Title": cfg.Title})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		files = append(files, plannedFile{path: filePath, content: []byte(content)})
		knowledgeRecord, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/knowledge.scl", map[string]string{
			"Key": knowledgeKey, "Title": cfg.Title, "BodyPath": "knowledge/" + agents.Kebab(cfg.Name) + ".md",
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/70_agent_knowledge.scl", header: knowledgeHeader, key: knowledgeKey, text: knowledgeRecord})
	} else if needsLink && knowledge.Body.Path != "" {
		files = append(files, plannedFile{path: filepath.Join(appDir, filepath.FromSlash(knowledge.Body.Path)), reuseOnly: true})
	}
	if needsLink {
		position := app.NextSkillPosition(task.Key)
		link, renderErr := renderAgentTemplate(tplFS, "templates/ai-agent/task_type_skill.scl", map[string]string{
			"Key": linkKey, "TaskTypeKey": task.Key, "KnowledgeKey": knowledgeKey, "Position": strconv.Itoa(position),
		})
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/72_agent_links.scl", header: linksHeader, key: linkKey, text: link})
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
	if err := validateToolConfig(cfg); err != nil {
		return WiringResult{}, err
	}
	_, platform := stringIn(agents.PlatformTools, cfg.Name)
	action, ownAction := app.Action(cfg.Name)
	var task *agents.TaskType
	if cfg.TaskTypeName != "" {
		var ok bool
		task, ok = app.TaskType(cfg.TaskTypeName)
		if !ok {
			return WiringResult{}, missingTaskType(cfg.AppID, cfg.TaskTypeName)
		}
	} else if platform {
		return WiringResult{}, fmt.Errorf("platform tool %q needs --task-type because it has no app action to set up; name the kind of work to receive it", cfg.Name)
	}
	if !platform && !ownAction && cfg.DisplayName == "" {
		command := fmt.Sprintf("simple new tool %s %s \"<Display name>\"", cfg.AppID, cfg.Name)
		if cfg.TaskTypeName != "" {
			command += " --task-type " + cfg.TaskTypeName
		}
		if cfg.Language == LanguageTypeScript {
			command += " --scope <org>"
		}
		return WiringResult{}, fmt.Errorf("tool %q is neither a platform tool nor an action of %s; create it with '%s'", cfg.Name, cfg.AppID, command)
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
	linkKey := ""
	needsLink := false
	if task != nil {
		linkKey = "task_type_" + agents.Snake(task.Name) + "_may_call_" + agents.Snake(cfg.Name)
		needsLink = !app.HasToolLink(task.Key, tool)
	}
	hasLesson := false
	var lesson *agents.Knowledge
	if ownAction {
		lesson, hasLesson = app.Lesson(action.Key)
	}
	newAction := !platform && !ownAction
	needsLesson := (ownAction && !hasLesson) || newAction
	lookupFile := filepath.Join(appDir, "records", "72_agent_links.scl")
	lookupName := "platform_tool_" + agents.Snake(cfg.Name)
	lookup, hasLookup := findPlatformToolLookup(app, lookupFile, lookupName)
	if platform && needsLink && hasLookup && (lookup.AppID != agents.PlatformAppID || lookup.Name != cfg.Name) {
		target := "another tool"
		if lookup.AppID != "" && lookup.Name != "" {
			target = lookup.AppID + "/" + lookup.Name
		}
		return WiringResult{}, fmt.Errorf("tool %q cannot use variable %q because it already looks up %s at %s:%d; change that lookup or choose a different variable name", cfg.Name, lookupName, target, lookup.File, lookup.Line)
	}
	needsLookup := platform && needsLink && !hasLookup
	if needsLink {
		if err := ensureKeysFree(app, "tool", cfg.Name, linkKey); err != nil {
			return WiringResult{}, err
		}
	}
	lessonKey, lessonLinkKey := "tool_"+agents.Snake(cfg.Name), "tool_"+agents.Snake(cfg.Name)+"_lesson"
	if needsLesson {
		if err := ensureKeysFree(app, "tool", cfg.Name, lessonKey, lessonLinkKey); err != nil {
			return WiringResult{}, err
		}
	}
	actionKey := agents.Snake(cfg.Name)
	if ownAction {
		actionKey = action.Key
	}
	if newAction {
		if err := ensureKeysFree(app, "tool", cfg.Name, actionKey); err != nil {
			return WiringResult{}, err
		}
		if PathExists(fsys, filepath.Join(appDir, "actions", cfg.Name)) {
			return WiringResult{}, fmt.Errorf("action folder for tool %q already exists in %s; move or rename that folder", cfg.Name, cfg.AppID)
		}
	}
	if !needsLink && !needsLesson && !needsLookup && !newAction {
		return WiringResult{Created: []string{}, Records: []string{}, FillIn: []string{}, NothingToAdd: true}, nil
	}
	files := make([]plannedFile, 0, 1)
	adds := make([]recordAddition, 0, 4)
	if newAction {
		action = &agents.Action{Key: actionKey, Name: cfg.Name, DisplayName: cfg.DisplayName, Description: cfg.Description}
		if cfg.Language == "" {
			cfg.Language = LanguageTypeScript
		}
		if cfg.Language != LanguageTypeScript && cfg.Language != LanguageRust {
			return WiringResult{}, fmt.Errorf("tool %q has unsupported language %q; choose ts or rust", cfg.Name, cfg.Language)
		}
		if cfg.ExecutionEnv == "" {
			cfg.ExecutionEnv = "server"
		}
		if cfg.Language == LanguageTypeScript && cfg.Scope == "" {
			return WiringResult{}, fmt.Errorf("tool %q needs --scope when you create a TypeScript action", cfg.Name)
		}
		if !contains(validToolExecutionEnvs, cfg.ExecutionEnv) {
			return WiringResult{}, fmt.Errorf("tool %q has unsupported environment %q; choose server, client, or both", cfg.Name, cfg.ExecutionEnv)
		}
		data := map[string]string{"ActionName": cfg.Name, "ActionNameScl": actionKey, "DisplayName": cfg.DisplayName,
			"Description": cfg.Description, "Scope": cfg.Scope, "ExecutionEnv": cfg.ExecutionEnv, "Language": cfg.Language, "Tool": "true"}
		actionRecord, renderErr := renderAgentTemplate(tplFS, "templates/action/10_actions.scl", data)
		if renderErr != nil {
			return WiringResult{}, renderErr
		}
		adds = append(adds, recordAddition{file: "records/10_actions.scl", header: actionHeader, key: actionKey, text: actionRecord})
		actionFiles, planErr := planToolActionFiles(tplFS, appDir, cfg)
		if planErr != nil {
			return WiringResult{}, planErr
		}
		files = append(files, actionFiles...)
	}
	if needsLesson {
		title := action.DisplayName
		if title == "" {
			title = cfg.DisplayName
		}
		if utf8.RuneCountInString(title) > 255 {
			return WiringResult{}, fmt.Errorf("tool %q lesson title is too long; use 255 characters or fewer", cfg.Name)
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
		files = append(files, plannedFile{path: lessonPath, content: []byte(lessonText)})
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
	} else if lesson != nil && lesson.Body.Path != "" && needsLink {
		files = append(files, plannedFile{path: filepath.Join(appDir, filepath.FromSlash(lesson.Body.Path)), reuseOnly: true})
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
	result, err := executePlan(fsys, repoRoot, files, records)
	if err != nil {
		return result, err
	}
	return result, nil
}

var validToolExecutionEnvs = []string{"server", "client", "both"}

func planToolActionFiles(tplFS fsx.TemplateFS, appDir string, cfg ToolConfig) ([]plannedFile, error) {
	actionDir := filepath.Join(appDir, "actions", cfg.Name)
	type actionTemplate struct {
		source string
		target string
	}
	var templates []actionTemplate
	if cfg.Language == LanguageRust {
		templates = []actionTemplate{
			{source: "templates/action-rust/Cargo.toml", target: filepath.Join(actionDir, "Cargo.toml")},
			{source: "templates/action-rust/main.rs", target: filepath.Join(actionDir, "src", "main.rs")},
			{source: "templates/action-rust/gitignore", target: filepath.Join(actionDir, ".gitignore")},
		}
	} else {
		templates = []actionTemplate{
			{source: "templates/action/package.json", target: filepath.Join(actionDir, "package.json")},
			{source: "templates/action/index.ts", target: filepath.Join(actionDir, "src", "index.ts")},
			{source: "templates/action/tsconfig.json", target: filepath.Join(actionDir, "tsconfig.json")},
			{source: "templates/action/tsdoc.json", target: filepath.Join(actionDir, "tsdoc.json")},
			{source: "templates/action/vitest.config.ts", target: filepath.Join(actionDir, "vitest.config.ts")},
			{source: "templates/action/tests/helpers.ts", target: filepath.Join(actionDir, "tests", "helpers.ts")},
			{source: "templates/action/tests/index.test.ts", target: filepath.Join(actionDir, "tests", "index.test.ts")},
		}
	}
	data := map[string]string{
		"ActionName": cfg.Name, "ActionNameScl": agents.Snake(cfg.Name), "DisplayName": cfg.DisplayName,
		"Description": cfg.Description, "Scope": cfg.Scope, "ExecutionEnv": cfg.ExecutionEnv,
		"Language": cfg.Language, "Tool": "tool",
	}
	files := make([]plannedFile, 0, len(templates))
	for _, item := range templates {
		content, err := renderTemplateContent(tplFS, item.source, data)
		if err != nil {
			return nil, err
		}
		files = append(files, plannedFile{path: item.target, content: content})
	}
	return files, nil
}

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
		return fmt.Errorf("agent %q has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter", cfg.Name)
	}
	if utf8.RuneCountInString(cfg.DisplayName) > 100 {
		return fmt.Errorf("agent %q display name is longer than 100 characters; shorten the display name", cfg.Name)
	}
	return validateScaffoldText("agent", cfg.Name, "display name", cfg.DisplayName, 100)
}

func validateTaskTypeConfig(cfg TaskTypeConfig) error {
	if !taskTypeName.MatchString(cfg.Name) {
		return fmt.Errorf("kind of work %q has an invalid name; use lowercase letters and digits in groups joined by single hyphens or underscores, starting with a letter", cfg.Name)
	}
	if len(cfg.Name) > 100 {
		return fmt.Errorf("kind of work %q name is longer than 100 characters; shorten the name", cfg.Name)
	}
	if utf8.RuneCountInString(cfg.PluralName) > 100 {
		return fmt.Errorf("kind of work %q plural name is longer than 100 characters; shorten the plural name", cfg.Name)
	}
	if err := validateScaffoldText("kind of work", cfg.Name, "plural name", cfg.PluralName, 100); err != nil {
		return err
	}
	if err := validateScaffoldText("kind of work", cfg.Name, "description", cfg.Description, 1000); err != nil {
		return err
	}
	if cfg.WakeIntervalSeconds < 0 {
		return fmt.Errorf("--every must be a whole-second duration of at least one second, such as 90s, 30m, or 1h")
	}
	for _, name := range cfg.Tools {
		if !kebabName.MatchString(name) {
			return fmt.Errorf("tool %q in --tools has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter", name)
		}
	}
	return nil
}

func validateSkillConfig(cfg SkillConfig) error {
	if !kebabName.MatchString(cfg.Name) {
		return fmt.Errorf("skill %q has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter", cfg.Name)
	}
	if utf8.RuneCountInString(cfg.Title) > 255 {
		return fmt.Errorf("skill %q title is longer than 255 characters; shorten the title", cfg.Name)
	}
	if cfg.Title != "" {
		if err := validateScaffoldText("skill", cfg.Name, "title", cfg.Title, 255); err != nil {
			return err
		}
	}
	return validateScaffoldText("skill", cfg.Name, "description", cfg.Description, 1000)
}

func validateKnowledgeConfig(cfg KnowledgeConfig) error {
	if !kebabName.MatchString(cfg.Name) {
		return fmt.Errorf("reference knowledge %q has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter", cfg.Name)
	}
	for _, prefix := range []string{"skill-", "tool-", "playbook-"} {
		if strings.HasPrefix(cfg.Name, prefix) {
			return fmt.Errorf("reference knowledge %q cannot use the reserved %q prefix; choose a name without it", cfg.Name, prefix)
		}
	}
	if utf8.RuneCountInString(cfg.Title) > 255 {
		return fmt.Errorf("reference knowledge %q title is longer than 255 characters; shorten the title", cfg.Name)
	}
	if cfg.Title != "" {
		return validateScaffoldText("reference knowledge", cfg.Name, "title", cfg.Title, 255)
	}
	return nil
}

func validateToolConfig(cfg ToolConfig) error {
	if !kebabName.MatchString(cfg.Name) {
		return fmt.Errorf("tool %q has an invalid name; use lowercase letters and digits in groups joined by single hyphens, starting with a letter", cfg.Name)
	}
	if utf8.RuneCountInString(cfg.DisplayName) > 100 {
		return fmt.Errorf("tool %q display name is longer than 100 characters; shorten the display name", cfg.Name)
	}
	if cfg.DisplayName != "" {
		if err := validateScaffoldText("tool", cfg.Name, "display name", cfg.DisplayName, 100); err != nil {
			return err
		}
	}
	return validateScaffoldText("tool", cfg.Name, "description", cfg.Description, 1000)
}

func validateActionConfigForTool(cfg ToolConfig) error {
	if cfg.DisplayName == "" {
		return fmt.Errorf("tool %q needs a display name to create an app action; add \"<Display name>\"", cfg.Name)
	}
	if err := validateScaffoldText("tool", cfg.Name, "display name", cfg.DisplayName, 100); err != nil {
		return err
	}
	return nil
}

func validateScaffoldText(kind, name, field, value string, limit int) error {
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

func selectAgent(app *agents.App, appID, wanted, taskTypeName string) (*agents.Agent, error) {
	if wanted != "" {
		if agent, ok := app.Agent(wanted); ok {
			return agent, nil
		}
		return nil, fmt.Errorf("agent %q does not exist in %s; create it with 'simple new agent %s %s \"<Display name>\"'", wanted, appID, appID, wanted)
	}
	agentsList := app.Agents()
	if len(agentsList) == 1 {
		return agentsList[0], nil
	}
	if len(agentsList) == 0 {
		return nil, fmt.Errorf("kind of work %q has no agent in %s; create one with 'simple new agent %s <name> \"<Display name>\"'", taskTypeName, appID, appID)
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
	return nil, fmt.Errorf("kind of work %q has multiple agents in %s (%s); choose one with --agent", taskTypeName, appID, strings.Join(names, ", "))
}

func ensureKeysFree(app *agents.App, subject, name string, keys ...string) error {
	for _, key := range keys {
		if file, line, ok := app.KeyTaken(key); ok {
			return fmt.Errorf("cannot use %s %q because a record name it needs is already used at %s:%d; choose a different %s name", subject, name, file, line, subject)
		}
	}
	return nil
}

func missingTaskType(appID, name string) error {
	return fmt.Errorf("kind of work %q does not exist in %s; create it with 'simple new task-type'", name, appID)
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
	operations := make([]plannedWrite, 0, len(files)+len(records))
	fillIn := make(map[string]bool)
	created := make(map[string]bool)
	for _, file := range files {
		old, existed, err := readExistingFile(fsys, file.path)
		if err != nil {
			return WiringResult{}, fmt.Errorf("failed to check existing file %s: %w", file.path, err)
		}
		if file.reuseOnly && !existed {
			continue
		}
		content := file.content
		if existed {
			content = old
		} else {
			operations = append(operations, plannedWrite{path: file.path, content: file.content})
			created[relativeToRoot(repoRoot, file.path)] = true
		}
		if bytes.Contains(content, []byte(agents.Marker)) {
			fillIn[relativeToRoot(repoRoot, file.path)] = true
		}
	}
	for _, recordsFile := range records {
		old, existed, err := readExistingFile(fsys, recordsFile.path)
		if err != nil {
			return WiringResult{}, fmt.Errorf("failed to check existing records file %s: %w", recordsFile.path, err)
		}
		operations = append(operations, plannedWrite{path: recordsFile.path, content: recordsFile.content, old: old, existed: existed})
		path := relativeToRoot(repoRoot, recordsFile.path)
		if !existed {
			created[path] = true
		}
		if bytes.Contains(recordsFile.content, []byte(agents.Marker)) {
			fillIn[path] = true
		}
		result.Records = append(result.Records, recordsFile.keys...)
	}

	written := make([]plannedWrite, 0, len(operations))
	for _, operation := range operations {
		if err := fsys.MkdirAll(filepath.Dir(operation.path), fsx.DirPerm); err != nil {
			cause := fmt.Errorf("failed to create directory for %s: %w", operation.path, err)
			return WiringResult{}, rollbackPlan(fsys, written, cause)
		}
		if err := fsys.WriteFile(operation.path, operation.content, fsx.FilePerm); err != nil {
			cause := fmt.Errorf("failed to write %s: %w", operation.path, err)
			return WiringResult{}, rollbackPlan(fsys, append(written, operation), cause)
		}
		written = append(written, operation)
	}
	for path := range created {
		result.Created = append(result.Created, path)
	}
	for path := range fillIn {
		result.FillIn = append(result.FillIn, path)
	}
	sort.Strings(result.Created)
	sort.Strings(result.FillIn)
	return result, nil
}

type plannedWrite struct {
	path    string
	content []byte
	old     []byte
	existed bool
}

func readExistingFile(fsys fsx.FileSystem, path string) ([]byte, bool, error) {
	_, err := fsys.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	content, err := fsys.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func rollbackPlan(fsys fsx.FileSystem, written []plannedWrite, cause error) error {
	var rollbackFailures []string
	for i := len(written) - 1; i >= 0; i-- {
		operation := written[i]
		var err error
		if operation.existed {
			err = fsys.WriteFile(operation.path, operation.old, fsx.FilePerm)
		} else {
			err = fsys.Remove(operation.path)
		}
		if err != nil {
			rollbackFailures = append(rollbackFailures, fmt.Sprintf("%s: %v", operation.path, err))
		}
	}
	if len(rollbackFailures) > 0 {
		return fmt.Errorf("%w; rollback also failed for %s", cause, strings.Join(rollbackFailures, ", "))
	}
	return cause
}

func relativeToRoot(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
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

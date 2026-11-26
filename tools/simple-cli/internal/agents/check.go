package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"simple-cli/internal/agents/structure"
	"simple-cli/internal/fsx"
)

var (
	checkInstructions = structure.CheckInstructions
	checkSkill        = structure.CheckSkill
)

var requiredStates = []string{"queued", "working", "waiting_for_user", "waiting_for_event", "completed", "cancelled", "failed"}

func (a *App) check(fsys fsx.FileSystem) ([]Problem, error) {
	var problems []Problem
	add := func(file string, line int, rule string) {
		problems = append(problems, Problem{File: filepath.ToSlash(file), Line: line, Rule: ensureSentence(rule)})
	}
	if err := a.checkMarkers(fsys, add); err != nil {
		return nil, err
	}
	if len(a.AgentRecords) == 0 && len(a.TaskTypes) == 0 {
		return uniqueProblems(problems), nil
	}

	for _, agent := range a.AgentRecords {
		persona, ok := a.knowledgeForRef(agent.Instructions)
		if !agent.Instructions.Readable {
			add(agent.File, agent.Line, fmt.Sprintf("agent %q has an unreadable persona reference: set instructions_id to `$app_record(\"knowledge_key\")`.", agent.DisplayName))
			continue
		}
		if !ok || persona.Body.Path == "" && !persona.Body.Readable {
			add(agent.File, agent.Line, fmt.Sprintf("agent %q has no persona body: set instructions_id to a knowledge record with a body.", agent.DisplayName))
			continue
		}
		if _, file, _, bodyOK := readBody(fsys, a, persona.Body); !bodyOK {
			add(persona.File, persona.Line, fmt.Sprintf("agent %q persona body cannot be read: add the referenced file or use inline text.", agent.DisplayName))
		} else {
			_ = file
		}
	}

	for _, taskType := range a.TaskTypes {
		taskType.DefaultAgent = nil
		taskType.DefaultAgentRef = Reference{}
		display := workName(taskType)
		defaults := []*AgentTaskTypeLink{}
		for _, link := range a.AgentTaskTypeLinks {
			if link.TaskTypeRef.Key == taskType.Key && link.Role == "executor" && link.IsDefault {
				defaults = append(defaults, link)
			}
		}
		if len(defaults) == 0 {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has no default agent: run 'simple new task-type', or add an agent_task_type record with role executor and is_default true.", display))
		} else if len(defaults) > 1 {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has more than one default agent: keep one agent_task_type record with role executor and is_default true.", display))
		} else {
			link := defaults[0]
			if !link.AgentRef.Readable {
				add(link.File, link.Line, fmt.Sprintf("kind of work %q has an unreadable default agent: set agent_id to `$app_record(\"agent_key\")`.", display))
			} else if agent := a.resolveAgent(link.AgentRef); agent == nil {
				add(link.File, link.Line, fmt.Sprintf("kind of work %q names a default that is not an agent of this app: set agent_id to an app agent record.", display))
			} else {
				taskType.DefaultAgent, taskType.DefaultAgentRef = agent, link.AgentRef
				if agent.Persona != nil && !a.HasSkillLink(taskType.Key, agent.Persona.Key) {
					add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q does not carry its default agent's persona: add a task_type_skill record for %s.", display, agent.DisplayName))
				}
			}
		}

		playbook, ok := a.knowledgeForRef(taskType.PlaybookRef)
		if !taskType.PlaybookRef.Readable {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has an unreadable playbook reference: set playbook_id to `$app_record(\"knowledge_key\")`.", display))
		} else if !ok || !hasBody(playbook) {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has no playbook body: set playbook_id to a knowledge record with a body.", display))
		} else if _, _, _, bodyOK := readBody(fsys, a, playbook.Body); !bodyOK {
			add(playbook.File, playbook.Line, fmt.Sprintf("kind of work %q playbook body cannot be read: add the referenced file or use inline text.", display))
		}
		if taskType.Playbook != nil && !a.HasSkillLink(taskType.Key, taskType.Playbook.Key) {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q does not carry its playbook: add a task_type_skill record for %s.", display, taskType.Playbook.Title))
		}
		if taskType.Playbook != nil && taskType.DefaultAgent != nil && taskType.DefaultAgent.Persona != nil {
			personaText, personaFile, personaOffset, personaOK := readBody(fsys, a, taskType.DefaultAgent.Persona.Body)
			playbookText, playbookFile, playbookOffset, playbookOK := readBody(fsys, a, taskType.Playbook.Body)
			if personaOK && playbookOK {
				personaFindings, playbookFindings := checkInstructions(taskType.DefaultAgent.DisplayName, personaText, playbookText)
				for _, finding := range personaFindings {
					file, line := findingLocation(taskType.DefaultAgent.Persona, personaFile, personaOffset, finding.Line)
					add(file, line, fmt.Sprintf("agent %q persona: %s", taskType.DefaultAgent.DisplayName, finding.Rule))
				}
				for _, finding := range playbookFindings {
					file, line := findingLocation(taskType.Playbook, playbookFile, playbookOffset, finding.Line)
					add(file, line, fmt.Sprintf("kind of work %q playbook: %s", display, finding.Rule))
				}
			}
		}
		if err := a.checkShape(fsys, taskType, add); err != nil {
			return nil, err
		}
		if err := a.checkStates(fsys, taskType, add); err != nil {
			return nil, err
		}
	}

	for _, link := range a.AgentTaskTypeLinks {
		a.checkAgentTaskLink(link, add)
	}
	for _, link := range a.TaskTypeSkillLinks {
		a.checkTaskSkillLink(fsys, link, add)
	}
	for _, link := range a.TaskTypeToolLinks {
		if err := a.checkTaskToolLink(fsys, link, add); err != nil {
			return nil, err
		}
	}
	for _, link := range a.LogicSkillLinks {
		a.checkLogicSkillLink(fsys, link, add)
	}
	for _, action := range a.Actions {
		marked, err := a.actionMarkedTool(fsys, action)
		if err != nil {
			return nil, err
		}
		if marked {
			lesson, ok := a.Lesson(action.Key)
			if !ok || !hasBody(lesson) {
				add(action.File, action.Line, fmt.Sprintf("tool %q has no lesson body: add a logic_skill record linked to knowledge with a body.", action.Name))
			} else if text, file, offset, bodyOK := readBody(fsys, a, lesson.Body); !bodyOK {
				add(lesson.File, lesson.Line, fmt.Sprintf("tool lesson for %q cannot be read: add the referenced file or use inline text.", action.Name))
			} else {
				for _, finding := range checkSkill(text) {
					findingFile, line := findingLocation(lesson, file, offset, finding.Line)
					add(findingFile, line, fmt.Sprintf("tool lesson for %q: %s", action.Name, finding.Rule))
				}
			}
		}
	}
	return uniqueProblems(problems), nil
}

func (a *App) checkAgentTaskLink(link *AgentTaskTypeLink, add func(string, int, string)) {
	task, taskOK := a.resolveTaskType(link.TaskTypeRef)
	agent := a.resolveAgent(link.AgentRef)
	if !link.AgentRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable agent link: set agent_id to `$app_record(\"agent_key\")`.", taskLabel(a, task, taskOK, link.TaskTypeRef)))
	} else if agent == nil {
		add(link.File, link.Line, fmt.Sprintf("%s names a record that is not an agent of this app: set agent_id to `$app_record(\"agent_key\")` for an app agent.", taskLabel(a, task, taskOK, link.TaskTypeRef)))
	}
	if !link.TaskTypeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable kind of work link: set task_type_id to `$app_record(\"task_type_key\")`.", agentLabel(a, agent, link.AgentRef)))
	} else if !taskOK {
		add(link.File, link.Line, fmt.Sprintf("%s is linked to a kind of work that does not exist in this app: set task_type_id to `$app_record(\"task_type_key\")` for an app kind of work.", agentLabel(a, agent, link.AgentRef)))
	}
}

func (a *App) checkTaskSkillLink(fsys fsx.FileSystem, link *TaskTypeSkillLink, add func(string, int, string)) {
	var task *TaskType
	if !link.TaskTypeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable skill link: set task_type_id to `$app_record(\"task_type_key\")`.", taskLabel(a, nil, false, link.TaskTypeRef)))
	} else if found, ok := a.resolveTaskType(link.TaskTypeRef); !ok {
		add(link.File, link.Line, fmt.Sprintf("%s has a skill link to a kind of work that does not exist in this app: set task_type_id to `$app_record(\"task_type_key\")` for an app kind of work.", taskLabel(a, nil, false, link.TaskTypeRef)))
	} else {
		task = found
	}
	knowledge, ok := a.knowledgeForRef(link.KnowledgeRef)
	if !link.KnowledgeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable skill reference: set knowledge_id to `$app_record(\"knowledge_key\")`.", taskLabel(a, task, task != nil, link.TaskTypeRef)))
		return
	}
	if !ok {
		add(link.File, link.Line, fmt.Sprintf("%s has a skill reference to a knowledge record that does not exist in this app: set knowledge_id to `$app_record(\"knowledge_key\")` for app knowledge.", taskLabel(a, task, task != nil, link.TaskTypeRef)))
		return
	}
	if task == nil || !isCoreKnowledge(task, knowledge) {
		if !hasBody(knowledge) {
			add(link.File, link.Line, fmt.Sprintf("skill %q has no body: set body to `$file('knowledge/skill-%s.md')` or inline text.", knowledge.Title, Kebab(knowledge.Title)))
			return
		}
		text, file, offset, bodyOK := readBody(fsys, a, knowledge.Body)
		if !bodyOK {
			add(knowledge.File, knowledge.Line, fmt.Sprintf("skill %q body cannot be read: add the referenced file or use inline text.", knowledge.Title))
			return
		}
		for _, finding := range checkSkill(text) {
			findingFile, line := findingLocation(knowledge, file, offset, finding.Line)
			add(findingFile, line, fmt.Sprintf("skill %q: %s", knowledge.Title, finding.Rule))
		}
	}
}

func (a *App) checkTaskToolLink(fsys fsx.FileSystem, link *TaskTypeToolLink, add func(string, int, string)) error {
	task, taskOK := a.resolveTaskType(link.TaskTypeRef)
	toolName := first(link.Tool.Name, link.LogicRef.Name, link.LogicRef.Key, link.Key)
	if link.Tool.OwnAction {
		toolName = actionName(a, a.actionByKey(link.Tool.ActionKey), link.LogicRef)
	}
	if !link.TaskTypeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("tool %q has an unreadable kind of work link: set task_type_id to `$app_record(\"task_type_key\")`.", toolName))
	} else if !taskOK {
		add(link.File, link.Line, fmt.Sprintf("tool %q is linked to a kind of work that does not exist in this app: set task_type_id to `$app_record(\"task_type_key\")` for an app kind of work.", toolName))
	}
	if !link.LogicRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable tool reference: use `$app_record(\"action_key\")` or a platform-tool lookup.", taskLabel(a, task, taskOK, link.TaskTypeRef)))
		return nil
	}
	if link.Tool.OwnAction {
		action := a.actionByKey(link.Tool.ActionKey)
		if action == nil {
			add(link.File, link.Line, fmt.Sprintf("%q is linked as a tool but is not an action of this app: run 'simple new tool' to add it.", link.Tool.Name))
			return nil
		}
		marked, err := a.actionMarkedTool(fsys, action)
		if err != nil {
			return err
		}
		if !marked {
			jsonFile := actionJSONPath(a, action)
			_, readErr := fsys.ReadFile(jsonFile)
			if errors.Is(readErr, fs.ErrNotExist) || errors.Is(readErr, os.ErrNotExist) {
				add(link.File, link.Line, fmt.Sprintf("%q has not been built yet, so the build cannot tell whether it is a tool: run 'simple build %s' and check again.", action.Name, a.ID))
			} else if readErr != nil {
				return fmt.Errorf("failed to read %s: %w", relative(a.RepoRoot, jsonFile), readErr)
			} else {
				add(link.File, link.Line, fmt.Sprintf("%q is linked as a tool but its source is not marked as one: add the tool tag and a short description, then build again.", action.Name))
			}
		}
		return nil
	}
	if link.Tool.AppID != PlatformAppID || !contains(PlatformTools, link.Tool.Name) {
		add(link.File, link.Line, fmt.Sprintf("%q is not a platform tool this version of the CLI knows; known tools are %s. Run 'simple version update'.", link.Tool.Name, strings.Join(PlatformTools, ", ")))
		return nil
	}
	return nil
}

func (a *App) checkLogicSkillLink(fsys fsx.FileSystem, link *LogicSkillLink, add func(string, int, string)) {
	action := a.actionByKey(link.LogicRef.Key)
	name := actionName(a, action, link.LogicRef)
	if !link.LogicRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q has an unreadable action reference: set logic_id to `$app_record(\"action_key\")`.", name))
	} else if action == nil {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q refers to an action that does not exist in this app: set logic_id to `$app_record(\"action_key\")` for an app action.", name))
	}
	knowledge, ok := a.knowledgeForRef(link.KnowledgeRef)
	if !link.KnowledgeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q has an unreadable knowledge reference: set knowledge_id to `$app_record(\"knowledge_key\")`.", name))
		return
	}
	if !ok {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q refers to a knowledge record that does not exist in this app: set knowledge_id to `$app_record(\"knowledge_key\")` for app knowledge.", name))
		return
	}
	if !hasBody(knowledge) {
		add(link.File, link.Line, fmt.Sprintf("tool lesson %q has no body: set body to a file or inline text.", knowledge.Title))
		return
	}
	text, file, offset, bodyOK := readBody(fsys, a, knowledge.Body)
	if !bodyOK {
		add(knowledge.File, knowledge.Line, fmt.Sprintf("tool lesson %q body cannot be read: add the referenced file or use inline text.", knowledge.Title))
		return
	}
	for _, finding := range checkSkill(text) {
		findingFile, line := findingLocation(knowledge, file, offset, finding.Line)
		add(findingFile, line, fmt.Sprintf("tool lesson for %q: %s", actionName(a, a.actionByKey(link.LogicRef.Key), link.LogicRef), finding.Rule))
	}
}

func (a *App) checkShape(fsys fsx.FileSystem, item *TaskType, add func(string, int, string)) error {
	record := a.records[item.Key]
	raw, exists := record.fields["schema"]
	path, readable := parseFilePath(raw)
	if !exists || !readable || path == "" {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q has no readable shape file: set schema to `$file('records/task-types/%s.json') |> $json()`.", workName(item), Kebab(item.Name)))
		return nil
	}
	file := filepath.Join(a.Dir, filepath.FromSlash(path))
	data, err := fsys.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			add(item.File, item.Line, fmt.Sprintf("kind of work %q shape file is missing: add %s.", workName(item), path))
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", relative(a.RepoRoot, file), err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q shape file must be valid JSON with an object at its top level.", workName(item)))
		return nil
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(value["input"], &input); err != nil || input == nil {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q shape must have an input object.", workName(item)))
	}
	output, exists := value["output"]
	if !exists {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q shape must have an output object or null.", workName(item)))
		return nil
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed != "null" {
		var outputObject map[string]json.RawMessage
		if err := json.Unmarshal(output, &outputObject); err != nil || outputObject == nil {
			add(item.File, item.Line, fmt.Sprintf("kind of work %q shape output must be an object or null.", workName(item)))
		}
	}
	return nil
}

func (a *App) checkStates(fsys fsx.FileSystem, item *TaskType, add func(string, int, string)) error {
	record := a.records[item.Key]
	raw, exists := record.fields["states"]
	if !exists {
		return nil
	}
	path, readable := parseFilePath(raw)
	if !readable || path == "" {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q has an unreadable states file reference: set states to `$file('records/task-types/states.json') |> $json()`.", workName(item)))
		return nil
	}
	file := filepath.Join(a.Dir, filepath.FromSlash(path))
	data, err := fsys.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			add(item.File, item.Line, fmt.Sprintf("kind of work %q states file is missing: add %s.", workName(item), path))
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", relative(a.RepoRoot, file), err)
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q states file must be valid JSON and name all seven states.", workName(item)))
		return nil
	}
	text, _ := json.Marshal(value)
	for _, state := range requiredStates {
		if !strings.Contains(string(text), `"`+state+`"`) {
			add(item.File, item.Line, fmt.Sprintf("kind of work %q states file does not name %s: include all seven standard states.", workName(item), state))
		}
	}
	return nil
}

func (a *App) actionMarkedTool(fsys fsx.FileSystem, action *Action) (bool, error) {
	file := actionJSONPath(a, action)
	data, err := fsys.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read %s: %w", relative(a.RepoRoot, file), err)
	}
	var value struct {
		AI struct {
			Tool bool `json:"tool"`
		} `json:"ai"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return false, nil
	}
	return value.AI.Tool, nil
}

func actionJSONPath(a *App, action *Action) string {
	return filepath.Join(a.Dir, "actions", filepath.FromSlash(action.Name), "action.json")
}

func (a *App) checkMarkers(fsys fsx.FileSystem, add func(string, int, string)) error {
	for _, base := range []string{"knowledge", "records", "actions"} {
		root := filepath.Join(a.Dir, base)
		files, err := allFiles(fsys, root)
		if err != nil {
			return fmt.Errorf("failed to list %s: %w", relative(a.RepoRoot, root), err)
		}
		for _, file := range files {
			rel := relative(a.RepoRoot, file)
			appRel, _ := filepath.Rel(a.Dir, file)
			appRel = filepath.ToSlash(appRel)
			if !markerFile(base, appRel) {
				continue
			}
			data, err := fsys.ReadFile(file)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
					continue
				}
				return fmt.Errorf("failed to read %s: %w", rel, err)
			}
			for lineNum, line := range strings.Split(string(data), "\n") {
				if !strings.Contains(line, Marker) {
					continue
				}
				name := a.markerName(appRel, lineNum+1)
				trimmed := []rune(strings.TrimSpace(line))
				if len(trimmed) > 100 {
					trimmed = append(trimmed[:97], '.', '.', '.')
				}
				add(rel, lineNum+1, fmt.Sprintf("%q still holds a line for you to replace: %q", name, string(trimmed)))
			}
		}
	}
	return nil
}

func markerFile(base, path string) bool {
	switch base {
	case "knowledge":
		return strings.HasSuffix(path, ".md")
	case "records":
		return strings.HasSuffix(path, ".scl") || strings.HasSuffix(path, ".json")
	case "actions":
		parts := strings.Split(path, "/")
		return len(parts) >= 3 && parts[2] == "src"
	}
	return false
}

func allFiles(fsys fsx.FileSystem, root string) ([]string, error) {
	var result []string
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := fsys.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
			} else {
				result = append(result, path)
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Strings(result)
	return result, nil
}

func (a *App) markerName(path string, line int) string {
	if strings.HasPrefix(path, "actions/") {
		parts := strings.Split(path, "/")
		if len(parts) > 1 {
			if action, ok := a.Action(parts[1]); ok {
				return action.Name
			}
			return parts[1]
		}
	}
	if strings.HasPrefix(path, "records/task-types/") && strings.HasSuffix(path, ".json") {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		if name == "states" {
			return "task states"
		}
		for _, task := range a.TaskTypes {
			if Kebab(task.Name) == name {
				return workName(task)
			}
		}
	}
	for _, item := range a.Knowledge {
		if item.Body.Path == path {
			return first(item.Title, item.Key)
		}
	}
	for _, item := range a.AgentRecords {
		if item.File == "apps/"+a.ID+"/records/"+path && item.Line <= line {
			return first(item.DisplayName, item.Key)
		}
	}
	var selected *record
	for _, item := range a.records {
		if item.file == "apps/"+a.ID+"/"+path && item.line > 0 && item.line <= line && (selected == nil || item.line > selected.line) {
			value := item
			selected = &value
		}
	}
	if selected != nil {
		switch selected.table {
		case "task_type":
			if task, ok := a.TaskType(str(selected.fields["name"])); ok {
				return workName(task)
			}
		case "logic":
			if action, ok := a.Action(str(selected.fields["name"])); ok {
				return action.Name
			}
		case "agent":
			for _, agent := range a.AgentRecords {
				if agent.Key == selected.key {
					return first(agent.DisplayName, agent.Key)
				}
			}
		case "knowledge":
			for _, knowledge := range a.Knowledge {
				if knowledge.Key == selected.key {
					return first(knowledge.Title, knowledge.Key)
				}
			}
		}
		return selected.key
	}
	return filepath.Base(path)
}

func (a *App) knowledgeForRef(ref Reference) (*Knowledge, bool) {
	if !ref.Readable {
		return nil, false
	}
	item, ok := a.records[ref.Key]
	if !ok || item.table != "knowledge" {
		return nil, false
	}
	knowledge := a.knowledgeByKey(ref.Key)
	return knowledge, knowledge != nil
}
func (a *App) resolveTaskType(ref Reference) (*TaskType, bool) {
	if !ref.Readable {
		return nil, false
	}
	item, ok := a.records[ref.Key]
	if !ok || item.table != "task_type" {
		return nil, false
	}
	task := a.taskTypeByKey(ref.Key)
	return task, task != nil
}

func taskLabel(a *App, task *TaskType, ok bool, ref Reference) string {
	if task == nil {
		task = a.taskTypeByKey(ref.Key)
		ok = task != nil
	}
	if ok && task != nil {
		return fmt.Sprintf("kind of work %q", workName(task))
	}
	name := strings.TrimPrefix(ref.Key, "task_type_")
	if index := strings.Index(name, "_carries_"); index >= 0 {
		name = name[:index]
	}
	if index := strings.Index(name, "_may_call_"); index >= 0 {
		name = name[:index]
	}
	name = strings.TrimSuffix(name, "_default_agent")
	if name != "" {
		return fmt.Sprintf("kind of work %q", Kebab(name))
	}
	return "kind of work in this record"
}

func agentLabel(a *App, agent *Agent, ref Reference) string {
	if agent == nil {
		agent = a.resolveAgent(ref)
	}
	if agent != nil {
		return fmt.Sprintf("agent %q", first(agent.DisplayName, agent.Key))
	}
	name := strings.TrimPrefix(ref.Key, "agent_")
	if name != "" {
		return fmt.Sprintf("agent %q", Kebab(name))
	}
	return "agent link"
}

func actionName(a *App, action *Action, ref Reference) string {
	if action == nil {
		action = a.actionByKey(ref.Key)
	}
	if action != nil {
		return first(action.Name, action.Key)
	}
	name := first(ref.Name, ref.Key)
	name = strings.TrimPrefix(name, "action_")
	if name != "" {
		return Kebab(name)
	}
	return "action"
}

func hasBody(item *Knowledge) bool { return item != nil && item.Body.Readable }
func isCoreKnowledge(task *TaskType, knowledge *Knowledge) bool {
	return task.Playbook != nil && task.Playbook.Key == knowledge.Key || task.DefaultAgent != nil && task.DefaultAgent.Persona != nil && task.DefaultAgent.Persona.Key == knowledge.Key
}
func workName(task *TaskType) string { return first(task.Name, task.Key) }
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func findingLocation(body *Knowledge, bodyFile string, offset, line int) (string, int) {
	if body.Body.Path == "" {
		return body.File, body.Line
	}
	if line <= 0 {
		return bodyFile, 0
	}
	return bodyFile, offset + line
}

func uniqueProblems(problems []Problem) []Problem {
	sort.Slice(problems, func(i, j int) bool {
		if problems[i].File != problems[j].File {
			return problems[i].File < problems[j].File
		}
		if problems[i].Line != problems[j].Line {
			return problems[i].Line < problems[j].Line
		}
		return problems[i].Rule < problems[j].Rule
	})
	result := make([]Problem, 0, len(problems))
	for _, item := range problems {
		if len(result) == 0 || result[len(result)-1] != item {
			result = append(result, item)
		}
	}
	return result
}

func ensureSentence(rule string) string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return rule
	}
	if !strings.HasSuffix(rule, ".") && !strings.HasSuffix(rule, "!") && !strings.HasSuffix(rule, "?") {
		rule += "."
	}
	return rule
}

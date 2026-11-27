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
	"unicode/utf8"

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
		problems = append(problems, Problem{File: filepath.ToSlash(file), Line: line, Rule: strings.TrimSuffix(strings.TrimSpace(rule), ".")})
	}
	if err := a.checkMarkers(fsys, add); err != nil {
		return nil, err
	}
	if !a.hasAgentCheckRecords() {
		return uniqueProblems(problems), nil
	}

	for _, agent := range a.AgentRecords {
		persona, ok := a.knowledgeForRef(agent.Instructions)
		if !agent.Instructions.Readable {
			add(agent.File, agent.Line, fmt.Sprintf("agent %q has an unreadable persona reference: set its persona to `$app_record(\"knowledge_key\")`.", agent.DisplayName))
			continue
		}
		if !ok || persona.Body.Path == "" && !persona.Body.Readable {
			add(agent.File, agent.Line, fmt.Sprintf("agent %q has no persona body: choose app knowledge with a body for its persona.", agent.DisplayName))
			continue
		}
		if _, file, _, bodyOK := readBody(fsys, a, persona.Body); !bodyOK {
			add(persona.File, persona.Line, fmt.Sprintf("agent %q persona body cannot be read: add the referenced file or use inline text.", agent.DisplayName))
		} else {
			_ = file
		}
	}

	for _, taskType := range a.TaskTypes {
		display := workName(taskType)
		hasAgentLink := a.hasAgentTaskLink(taskType)
		taskType.DefaultAgent = nil
		taskType.DefaultAgentRef = Reference{}
		defaults := []*AgentTaskTypeLink{}
		if hasAgentLink {
			for _, link := range a.AgentTaskTypeLinks {
				if link.TaskTypeRef.Key == taskType.Key && link.Role == "executor" && link.IsDefault {
					defaults = append(defaults, link)
				}
			}
			if len(defaults) == 0 {
				add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has no default agent, so no task can be created for it: mark one executor link as the default", display))
			} else if len(defaults) > 1 {
				add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has more than one default agent, so no task can be created for it: keep exactly one executor link marked as the default", display))
			} else {
				link := defaults[0]
				if !link.AgentRef.Readable {
					add(link.File, link.Line, fmt.Sprintf("kind of work %q has an unreadable default agent: set it to `$app_record(\"agent_key\")` for an app agent", display))
				} else if agent := a.resolveAgent(link.AgentRef); agent == nil {
					add(link.File, link.Line, fmt.Sprintf("kind of work %q names a record that is not an app agent: point the default-agent link to that agent's `$app_record` reference", display))
				} else {
					taskType.DefaultAgent, taskType.DefaultAgentRef = agent, link.AgentRef
					if agent.Persona != nil && !a.HasSkillLink(taskType.Key, agent.Persona.Key) {
						add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q does not carry agent %q's persona: add a knowledge link to that persona", display, agent.DisplayName))
					}
				}
			}
		}

		playbook, playbookExists := a.knowledgeForRef(taskType.PlaybookRef)
		hasPlaybookReference := strings.TrimSpace(taskType.PlaybookRef.Raw) != ""
		if hasAgentLink && !hasPlaybookReference {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has no playbook body: choose app knowledge with a body for its playbook", display))
		} else if hasPlaybookReference && !taskType.PlaybookRef.Readable {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q has an unreadable playbook reference: set it to `$app_record(\"knowledge_key\")` for app knowledge", display))
		} else if hasPlaybookReference && (!playbookExists || !hasBody(playbook)) {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q names a playbook without a body: choose app knowledge with a body for its playbook", display))
		} else if hasPlaybookReference {
			if _, _, _, bodyOK := readBody(fsys, a, playbook.Body); !bodyOK {
				add(playbook.File, playbook.Line, fmt.Sprintf("kind of work %q playbook file cannot be read: add the file or point the playbook to readable knowledge", display))
			}
		}
		if hasAgentLink && taskType.Playbook != nil && !a.HasSkillLink(taskType.Key, taskType.Playbook.Key) {
			add(taskType.File, taskType.Line, fmt.Sprintf("kind of work %q does not carry playbook %q: add a knowledge link to that playbook", display, knowledgeName(taskType.Playbook)))
		}
		if hasAgentLink && taskType.Playbook != nil && taskType.DefaultAgent != nil && taskType.DefaultAgent.Persona != nil {
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
		mark, err := a.actionToolMark(fsys, action)
		if err != nil {
			return nil, err
		}
		if mark == toolMarked {
			lesson, ok := a.Lesson(action.Key)
			if !ok || !hasBody(lesson) {
				add(action.File, action.Line, fmt.Sprintf("tool %q has no lesson body: link a knowledge body to this tool as its lesson.", action.Name))
			}
		}
	}
	return uniqueProblems(problems), nil
}

func (a *App) hasAgentTaskLink(task *TaskType) bool {
	for _, link := range a.AgentTaskTypeLinks {
		if link.TaskTypeRef.Key == task.Key {
			return true
		}
	}
	return false
}

func (a *App) checkAgentTaskLink(link *AgentTaskTypeLink, add func(string, int, string)) {
	task, taskOK := a.resolveTaskType(link.TaskTypeRef)
	agent := a.resolveAgent(link.AgentRef)
	if !link.AgentRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable agent link: set the agent reference to `$app_record(\"agent_key\")`.", taskLabel(a, task, taskOK, link.TaskTypeRef)))
	} else if agent == nil {
		add(link.File, link.Line, fmt.Sprintf("%s names a record that is not an agent of this app: set the agent reference to `$app_record(\"agent_key\")` for an app agent.", taskLabel(a, task, taskOK, link.TaskTypeRef)))
	}
	if !link.TaskTypeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable kind of work link: set the kind-of-work reference to `$app_record(\"task_type_key\")`.", agentLabel(a, agent, link.AgentRef)))
	} else if !taskOK {
		add(link.File, link.Line, fmt.Sprintf("%s is linked to a kind of work that does not exist in this app: set the kind-of-work reference to `$app_record(\"task_type_key\")` for an app kind of work.", agentLabel(a, agent, link.AgentRef)))
	}
}

func (a *App) checkTaskSkillLink(fsys fsx.FileSystem, link *TaskTypeSkillLink, add func(string, int, string)) {
	var task *TaskType
	if !link.TaskTypeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable knowledge link: set its kind-of-work reference to `$app_record(\"task_type_key\")`.", taskLabel(a, nil, false, link.TaskTypeRef)))
	} else if found, ok := a.resolveTaskType(link.TaskTypeRef); !ok {
		add(link.File, link.Line, fmt.Sprintf("%s has a knowledge link to a kind of work that does not exist in this app: set its kind-of-work reference to `$app_record(\"task_type_key\")` for an app kind of work.", taskLabel(a, nil, false, link.TaskTypeRef)))
	} else {
		task = found
	}
	knowledge, ok := a.knowledgeForRef(link.KnowledgeRef)
	if !link.KnowledgeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable knowledge reference: set it to `$app_record(\"knowledge_key\")` for app knowledge.", taskLabel(a, task, task != nil, link.TaskTypeRef)))
		return
	}
	if !ok {
		add(link.File, link.Line, fmt.Sprintf("%s has a knowledge reference to a record that does not exist in this app: set the reference to `$app_record(\"knowledge_key\")` for app knowledge.", taskLabel(a, task, task != nil, link.TaskTypeRef)))
		return
	}
	if task == nil || !isCoreKnowledge(task, knowledge) {
		if !hasBody(knowledge) {
			add(link.File, link.Line, fmt.Sprintf("knowledge %q has no body: add a file or inline text", knowledgeName(knowledge)))
			return
		}
		text, file, offset, bodyOK := readBody(fsys, a, knowledge.Body)
		if !bodyOK {
			add(knowledge.File, knowledge.Line, fmt.Sprintf("knowledge %q body cannot be read: add the file or use inline text", knowledgeName(knowledge)))
			return
		}
		if !utf8.ValidString(text) {
			add(knowledge.File, knowledge.Line, fmt.Sprintf("knowledge %q must use valid UTF-8 text: replace invalid text", knowledgeName(knowledge)))
			return
		}
		if strings.TrimSpace(text) == "" {
			add(knowledge.File, knowledge.Line, fmt.Sprintf("knowledge %q must contain text: add the reference material", knowledgeName(knowledge)))
			return
		}
		if structure.HasSlugLine(text) || strings.HasPrefix(filepath.Base(knowledge.Body.Path), "skill-") {
			findings := checkSkill(text)
			if strings.HasPrefix(filepath.Base(knowledge.Body.Path), "skill-") && !structure.HasSlugLine(text) {
				filtered := findings[:0]
				for _, finding := range findings {
					if !strings.Contains(finding.Rule, "write a slug line") {
						filtered = append(filtered, finding)
					}
				}
				findings = append(filtered, structure.Finding{Line: 1, Rule: "put the Slug: line back in " + filepath.Base(knowledge.Body.Path)})
			}
			if mismatch := structure.SkillFileNameFinding(text, knowledge.Body.Path); mismatch != nil {
				findings = append(findings, *mismatch)
			}
			for _, finding := range findings {
				findingFile, line := findingLocation(knowledge, file, offset, finding.Line)
				add(findingFile, line, fmt.Sprintf("knowledge %q: %s", knowledgeName(knowledge), finding.Rule))
			}
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
		add(link.File, link.Line, fmt.Sprintf("tool %q has an unreadable kind of work link: set it to `$app_record(\"task_type_key\")` for an app kind of work.", toolName))
	} else if !taskOK {
		add(link.File, link.Line, fmt.Sprintf("tool %q is linked to a kind of work that does not exist in this app: set its kind-of-work reference to `$app_record(\"task_type_key\")` for app work.", toolName))
	}
	if !link.LogicRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("%s has an unreadable tool reference: use `$app_record(\"action_key\")` for an app tool or a platform-tool lookup.", taskLabel(a, task, taskOK, link.TaskTypeRef)))
		return nil
	}
	if link.Tool.OwnAction {
		action := a.actionByKey(link.Tool.ActionKey)
		if action == nil {
			add(link.File, link.Line, fmt.Sprintf("tool %q is linked here but is not an action of this app: create it with 'simple new tool %s %s \"<Display name>\"'", link.Tool.Name, a.ID, link.Tool.Name))
			return nil
		}
		mark, err := a.actionToolMark(fsys, action)
		if err != nil {
			return err
		}
		switch mark {
		case toolNotBuilt:
			add(link.File, link.Line, fmt.Sprintf("tool %q has not been built, so its tool setting is unknown: run 'simple build %s' and check again", action.Name, a.ID))
		case toolUnreadable:
			add(link.File, link.Line, fmt.Sprintf("tool %q has a built description that cannot be read, so its tool setting is unknown: run 'simple build %s' and check again", action.Name, a.ID))
		case toolNotMarked:
			add(link.File, link.Line, fmt.Sprintf("tool %q is not marked as a tool in its action source: add @tool, and @shortdesc followed by one line saying what the tool gives an agent, to a comment there, then build again", action.Name))
		case toolMarked:
			// Nothing to report: the link names an action that is a tool.
		}
		return nil
	}
	if link.Tool.AppID != PlatformAppID || !contains(PlatformTools, link.Tool.Name) {
		add(link.File, link.Line, fmt.Sprintf("tool %q is unknown to this CLI; use one of these platform tools: %s, or update the CLI with 'simple version update'", link.Tool.Name, strings.Join(PlatformTools, ", ")))
		return nil
	}
	return nil
}

func (a *App) checkLogicSkillLink(fsys fsx.FileSystem, link *LogicSkillLink, add func(string, int, string)) {
	action := a.actionByKey(link.LogicRef.Key)
	name := actionName(a, action, link.LogicRef)
	if !link.LogicRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q has an unreadable action reference: set its tool to `$app_record(\"action_key\")` for an app action.", name))
	} else if action == nil {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q refers to an action that does not exist in this app: set its tool to `$app_record(\"action_key\")` for an app action.", name))
	}
	knowledge, ok := a.knowledgeForRef(link.KnowledgeRef)
	if !link.KnowledgeRef.Readable {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q has an unreadable knowledge reference: set it to `$app_record(\"knowledge_key\")` for app knowledge.", name))
		return
	}
	if !ok {
		add(link.File, link.Line, fmt.Sprintf("tool lesson for %q refers to a record that does not exist in this app: set it to `$app_record(\"knowledge_key\")` for app knowledge.", name))
		return
	}
	if !hasBody(knowledge) {
		add(link.File, link.Line, fmt.Sprintf("tool lesson %q has no body: add a file or inline text.", knowledgeName(knowledge)))
		return
	}
	text, file, offset, bodyOK := readBody(fsys, a, knowledge.Body)
	if !bodyOK {
		add(knowledge.File, knowledge.Line, fmt.Sprintf("tool lesson %q body cannot be read: add the referenced file or use inline text.", knowledgeName(knowledge)))
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
	var data []byte
	shapeLabel := "shape"
	if !exists {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q has no readable shape: provide an inline object or point it to `$file('records/task-types/%s.json') |> $json()`.", workName(item), Kebab(item.Name)))
		return nil
	}
	if readable && path != "" {
		shapeLabel = "shape file"
		file := filepath.Join(a.Dir, filepath.FromSlash(path))
		var err error
		data, err = fsys.ReadFile(file)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
				add(item.File, item.Line, fmt.Sprintf("kind of work %q shape file is missing: add %s.", workName(item), path))
				return nil
			}
			return fmt.Errorf("failed to read %s: %w", relative(a.RepoRoot, file), err)
		}
	} else {
		var inline bool
		data, inline = inlineShapeJSON(record.fields["schema"])
		if !inline {
			add(item.File, item.Line, fmt.Sprintf("kind of work %q has no readable shape: provide an inline object or point it to `$file('records/task-types/%s.json') |> $json()`.", workName(item), Kebab(item.Name)))
			return nil
		}
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q %s must be valid JSON with an object at its top level.", workName(item), shapeLabel))
		return nil
	}
	input, inputExists := value["input"]
	if inputExists {
		if !validShapeInput(input) {
			add(item.File, item.Line, fmt.Sprintf("kind of work %q shape input must have \"type\": \"object\", or be left out, null, true, or an empty object for free input", workName(item)))
		}
	}
	output, outputExists := value["output"]
	if !outputExists {
		return nil
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "null" || trimmed == `"null"` {
		return nil
	}
	if !validShapeObject(output) {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q shape output must have \"type\": \"object\", or be left out for any answer", workName(item)))
	}
	return nil
}

func validShapeInput(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" || trimmed == `"null"` || trimmed == "true" {
		return true
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return false
	}
	if len(object) == 0 {
		return true
	}
	return validShapeObject(raw)
}

func validShapeObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return false
	}
	var typeName string
	if err := json.Unmarshal(object["type"], &typeName); err != nil {
		return false
	}
	return typeName == "object"
}

func inlineShapeJSON(raw any) ([]byte, bool) {
	if text, ok := raw.(string); ok {
		trimmed := strings.TrimSpace(text)
		if strings.HasPrefix(trimmed, "$") {
			return nil, false
		}
		var value any
		if json.Unmarshal([]byte(trimmed), &value) == nil {
			data, err := json.Marshal(value)
			return data, err == nil
		}
	}
	data, err := json.Marshal(raw)
	return data, err == nil
}

func (a *App) checkStates(fsys fsx.FileSystem, item *TaskType, add func(string, int, string)) error {
	record := a.records[item.Key]
	raw, exists := record.fields["states"]
	if !exists {
		return nil
	}
	path, readable := parseFilePath(raw)
	if !readable || path == "" {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q has an unreadable states file reference: point its state list to `$file('records/task-types/states.json') |> $json()`.", workName(item)))
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
	var value map[string]json.RawMessage
	if json.Unmarshal(data, &value) != nil || value == nil {
		add(item.File, item.Line, fmt.Sprintf("kind of work %q states file must be valid JSON and name all seven states.", workName(item)))
		return nil
	}
	for _, state := range requiredStates {
		if _, declared := value[state]; !declared {
			add(item.File, item.Line, fmt.Sprintf("kind of work %q states file does not name %s: include all seven standard states.", workName(item), state))
		}
	}
	return nil
}

// toolMark is what an action's built description says about the action being
// a tool. The build writes that description, action.json, from the tags in the
// action's source.
type toolMark int

const (
	// toolNotBuilt: there is no built description yet.
	toolNotBuilt toolMark = iota
	// toolUnreadable: the file is there and is not a description the build
	// writes. That says nothing about the source, so the author is sent to
	// build again, not to add tags that may already be there.
	toolUnreadable
	// toolNotMarked: built, and the source does not offer the action as a tool.
	toolNotMarked
	// toolMarked: built, and the source offers the action as a tool.
	toolMarked
)

func (a *App) actionToolMark(fsys fsx.FileSystem, action *Action) (toolMark, error) {
	file := actionJSONPath(a, action)
	data, err := fsys.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return toolNotBuilt, nil
		}
		return toolNotBuilt, fmt.Errorf("failed to read %s: %w", relative(a.RepoRoot, file), err)
	}
	// A pointer, so that a file holding only `null` is told apart from a
	// description with no tool setting: both decode without an error.
	var value *struct {
		AI struct {
			Tool bool `json:"tool"`
		} `json:"ai"`
	}
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		return toolUnreadable, nil
	}
	if value.AI.Tool {
		return toolMarked, nil
	}
	return toolNotMarked, nil
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
					return knowledgeName(knowledge)
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

func knowledgeName(knowledge *Knowledge) string {
	if knowledge.Title != "" {
		return knowledge.Title
	}
	name := strings.TrimSuffix(filepath.Base(knowledge.Body.Path), filepath.Ext(knowledge.Body.Path))
	for _, prefix := range []string{"skill-", "tool-", "playbook-"} {
		name = strings.TrimPrefix(name, prefix)
	}
	if name != "" && name != "." {
		return name
	}
	key := knowledge.Key
	for _, prefix := range []string{"knowledge_", "skill_", "tool_", "playbook_"} {
		key = strings.TrimPrefix(key, prefix)
	}
	key = Kebab(key)
	if key != "" {
		return key
	}
	return "knowledge"
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
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rule), "."))
}

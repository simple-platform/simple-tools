package agents

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Canonical describes the app's agent wiring without record keys or source order.
func (a *App) Canonical() string {
	lines := make([]string, 0)
	appendLine := func(kind string, fields ...string) {
		values := append([]string{kind}, fields...)
		encoded, _ := json.Marshal(values)
		lines = append(lines, string(encoded))
	}
	for _, agent := range a.AgentRecords {
		persona := ""
		if agent.Persona != nil {
			persona = bodyMeaning(agent.Persona)
		}
		appendLine("agent", agent.DisplayName, agent.Description, agent.Status, persona)
	}
	for _, task := range a.TaskTypes {
		playbook := ""
		if task.Playbook != nil {
			playbook = bodyMeaning(task.Playbook)
		}
		defaultAgent := ""
		if task.DefaultAgent != nil {
			defaultAgent = task.DefaultAgent.DisplayName
		}
		appendLine("kind", task.Name, task.PluralName, task.Description, playbook, task.ShapePath, task.StatesPath, strconv.FormatInt(task.WakeIntervalSeconds, 10), defaultAgent)
	}
	for _, link := range a.TaskTypeSkillLinks {
		task := a.taskTypeByKey(link.TaskTypeRef.Key)
		knowledge, _ := a.knowledgeForRef(link.KnowledgeRef)
		if task == nil || knowledge == nil {
			continue
		}
		appendLine("skill-link", task.Name, bodyMeaning(knowledge), strconv.Itoa(link.Position))
	}
	for _, link := range a.TaskTypeToolLinks {
		task := a.taskTypeByKey(link.TaskTypeRef.Key)
		if task == nil {
			continue
		}
		if link.Tool.OwnAction {
			action := a.actionByKey(link.Tool.ActionKey)
			if action != nil {
				appendLine("tool-link", task.Name, action.Name)
			}
		} else if link.Tool.Name != "" {
			appendLine("tool-link", task.Name, link.Tool.AppID, link.Tool.Name)
		}
	}
	for _, link := range a.LogicSkillLinks {
		action := a.actionByKey(link.LogicRef.Key)
		knowledge, _ := a.knowledgeForRef(link.KnowledgeRef)
		if action != nil && knowledge != nil {
			appendLine("lesson", action.Name, bodyMeaning(knowledge))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func bodyMeaning(item *Knowledge) string {
	if item.Body.Path != "" {
		return item.Body.Path
	}
	if item.Title != "" {
		return "title:" + item.Title
	}
	return "inline:" + item.Body.Inline
}

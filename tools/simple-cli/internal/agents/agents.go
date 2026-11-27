// Package agents reads and checks an application's agent declarations.
package agents

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const Marker = "TODO(simple):"
const PlatformAppID = "dev.simple.system"

var PlatformTools = []string{"introspect-data", "process-data", "mutate-data", "load-knowledge", "read-tool-result", "read-task-output"}

func Snake(s string) string { return strings.ReplaceAll(s, "-", "_") }
func Kebab(s string) string { return strings.ReplaceAll(s, "_", "-") }

// Problem is one build-time finding. File is relative to the repository root.
type Problem struct {
	File string
	Line int
	Rule string
}

// Body describes guidance stored in a file or directly in a record.
type Body struct {
	Path     string
	Inline   string
	Raw      string
	Readable bool
}

// Reference preserves both a decoded record key and the source expression.
type Reference struct {
	Key      string
	Raw      string
	Readable bool
	Lookup   bool
	AppID    string
	Name     string
}

type Agent struct {
	Key, File                        string
	Line                             int
	DisplayName, Description, Status string
	Instructions                     Reference
	Persona                          *Knowledge
}

type TaskType struct {
	Key, File                     string
	Line                          int
	Name, PluralName, Description string
	PlaybookRef                   Reference
	Playbook                      *Knowledge
	ShapePath, StatesPath         string
	WakeIntervalSeconds           int64
	DefaultAgentRef               Reference
	DefaultAgent                  *Agent
}

type Knowledge struct {
	Key, File string
	Line      int
	Title     string
	Body      Body
}

type Action struct {
	Key, File                      string
	Line                           int
	Name, DisplayName, Description string
}

// ToolRef identifies either an action declared by this app or a platform tool.
type ToolRef struct {
	ActionKey string
	AppID     string
	Name      string
	OwnAction bool
}

type AgentTaskTypeLink struct {
	Key, File             string
	Line                  int
	AgentRef, TaskTypeRef Reference
	Role                  string
	IsDefault             bool
}

type TaskTypeSkillLink struct {
	Key, File                 string
	Line                      int
	TaskTypeRef, KnowledgeRef Reference
	Position                  int
}

type TaskTypeToolLink struct {
	Key, File             string
	Line                  int
	TaskTypeRef, LogicRef Reference
	Tool                  ToolRef
}

type LogicSkillLink struct {
	Key, File              string
	Line                   int
	LogicRef, KnowledgeRef Reference
}

type record struct {
	table, key, file string
	line             int
	fields           map[string]any
}

type lookup struct {
	name, appID string
	query       string
	file        string
	line        int
}

// LookupInfo describes the tool a record variable looks up.
type LookupInfo struct {
	AppID, Name, File string
	Line              int
}

// App is the semantic model of one app's declarations.
type App struct {
	ID, RepoRoot, Dir  string
	AgentRecords       []*Agent
	TaskTypes          []*TaskType
	Knowledge          []*Knowledge
	Actions            []*Action
	AgentTaskTypeLinks []*AgentTaskTypeLink
	TaskTypeSkillLinks []*TaskTypeSkillLink
	TaskTypeToolLinks  []*TaskTypeToolLink
	LogicSkillLinks    []*LogicSkillLink
	records            map[string]record
	lookups            map[string]map[string]lookup
}

func (a *App) DeclaresAgents() bool { return len(a.AgentRecords) > 0 }

func (a *App) hasAgentCheckRecords() bool {
	return len(a.AgentRecords) > 0 || len(a.AgentTaskTypeLinks) > 0
}

func (a *App) Agents() []*Agent {
	result := append([]*Agent(nil), a.AgentRecords...)
	sort.Slice(result, func(i, j int) bool { return result[i].DisplayName < result[j].DisplayName })
	return result
}

func (a *App) Agent(name string) (*Agent, bool) {
	key := Snake(name)
	for _, item := range a.AgentRecords {
		if item.Key == "agent_"+key || item.Key == key {
			return item, true
		}
	}
	return nil, false
}

func normalizeName(name string) string { return strings.ReplaceAll(name, "-", "_") }

func (a *App) TaskType(name string) (*TaskType, bool) {
	for _, item := range a.TaskTypes {
		if normalizeName(item.Name) == normalizeName(name) {
			return item, true
		}
	}
	return nil, false
}

func (a *App) Skill(name string) (*Knowledge, bool) {
	wantKey, wantPath := Snake(name), "knowledge/skill-"+Kebab(name)+".md"
	for _, item := range a.Knowledge {
		if item.Key == "skill_"+wantKey || item.Key == wantKey || filepath.ToSlash(item.Body.Path) == wantPath {
			return item, true
		}
	}
	return nil, false
}

// ReferenceKnowledge finds a knowledge record by its command name.
func (a *App) ReferenceKnowledge(name string) (*Knowledge, bool) {
	wantKey, wantPath := Snake(name), "knowledge/"+Kebab(name)+".md"
	for _, item := range a.Knowledge {
		if item.Key == "knowledge_"+wantKey || item.Key == wantKey || filepath.ToSlash(item.Body.Path) == wantPath {
			return item, true
		}
	}
	return nil, false
}

func (a *App) Action(name string) (*Action, bool) {
	for _, item := range a.Actions {
		if item.Name == name {
			return item, true
		}
	}
	return nil, false
}

func (a *App) KeyTaken(key string) (string, int, bool) {
	item, ok := a.records[key]
	return item.file, item.line, ok
}

func (a *App) HasSkillLink(taskTypeKey, knowledgeKey string) bool {
	for _, link := range a.TaskTypeSkillLinks {
		if link.TaskTypeRef.Key == taskTypeKey && link.KnowledgeRef.Key == knowledgeKey {
			return true
		}
	}
	return false
}

func (a *App) HasToolLink(taskTypeKey string, tool ToolRef) bool {
	for _, link := range a.TaskTypeToolLinks {
		if link.TaskTypeRef.Key == taskTypeKey && sameTool(link.Tool, tool) {
			return true
		}
	}
	return false
}

func sameTool(left, right ToolRef) bool {
	if left.OwnAction || right.OwnAction {
		return left.OwnAction && right.OwnAction && left.ActionKey == right.ActionKey
	}
	return left.AppID == right.AppID && left.Name == right.Name
}

func (a *App) Lesson(actionKey string) (*Knowledge, bool) {
	for _, link := range a.LogicSkillLinks {
		if link.LogicRef.Key != actionKey {
			continue
		}
		if knowledge := a.knowledgeByKey(link.KnowledgeRef.Key); knowledge != nil {
			return knowledge, true
		}
	}
	return nil, false
}

func (a *App) NextSkillPosition(taskTypeKey string) int {
	highest := 0
	found := false
	for _, link := range a.TaskTypeSkillLinks {
		if link.TaskTypeRef.Key == taskTypeKey && (!found || link.Position > highest) {
			highest = link.Position
			found = true
		}
	}
	if !found {
		return 10
	}
	return highest + 10
}

func (a *App) HasLookup(recordsFile, varName string) bool {
	_, ok := a.Lookup(recordsFile, varName)
	return ok
}

func (a *App) Lookup(recordsFile, varName string) (LookupInfo, bool) {
	for file, values := range a.lookups {
		if sameFile(file, recordsFile, a.Dir, a.RepoRoot) {
			if item, ok := values[varName]; ok {
				return LookupInfo{AppID: item.appID, Name: item.name, File: item.file, Line: item.line}, true
			}
		}
	}
	return LookupInfo{}, false
}

func (a *App) StatesFile() (string, bool) {
	for _, item := range a.TaskTypes {
		if item.StatesPath != "" {
			return filepath.ToSlash(filepath.Join("apps", a.ID, item.StatesPath)), true
		}
	}
	return "", false
}

func (a *App) knowledgeByKey(key string) *Knowledge {
	for _, item := range a.Knowledge {
		if item.Key == key {
			return item
		}
	}
	return nil
}

func (a *App) taskTypeByKey(key string) *TaskType {
	for _, item := range a.TaskTypes {
		if item.Key == key {
			return item
		}
	}
	return nil
}

func (a *App) actionByKey(key string) *Action {
	for _, item := range a.Actions {
		if item.Key == key {
			return item
		}
	}
	return nil
}

func sameFile(candidate, requested, appDir, repoRoot string) bool {
	clean := filepath.Clean(requested)
	if clean == filepath.Clean(candidate) {
		return true
	}
	if filepath.IsAbs(clean) {
		absoluteCandidate := filepath.Join(repoRoot, candidate)
		return clean == filepath.Clean(absoluteCandidate)
	}
	if filepath.ToSlash(clean) == strings.TrimPrefix(filepath.ToSlash(candidate), "./") {
		return true
	}
	appRelative, err := filepath.Rel(repoRoot, appDir)
	if err != nil {
		return false
	}
	return filepath.Clean(filepath.Join(appRelative, requested)) == filepath.Clean(candidate)
}

func resolveRecord(records map[string]record, ref Reference) (record, bool) {
	if !ref.Readable {
		return record{}, false
	}
	item, ok := records[ref.Key]
	return item, ok
}

func refFor(raw any, vars map[string]lookup, ownApp string, actionKeys map[string]string) Reference {
	text, ok := raw.(string)
	if !ok {
		return Reference{}
	}
	ref := Reference{Raw: text}
	if key, ok := parseAppRecord(text); ok {
		ref.Key, ref.Readable = key, true
		return ref
	}
	varName, query, ok := parseVarRef(text)
	if !ok {
		return ref
	}
	entry, ok := vars[varName]
	if !ok {
		return ref
	}
	name := queryToolName(query)
	if name == "" {
		name = entry.name
	}
	if entry.appID == "" || name == "" {
		return ref
	}
	ref.Lookup, ref.Readable, ref.AppID, ref.Name = true, true, entry.appID, name
	if entry.appID == ownApp {
		ref.Key = actionKeys[name]
	}
	return ref
}

func (t ToolRef) String() string {
	if t.OwnAction {
		return t.Name
	}
	return fmt.Sprintf("%s/%s", t.AppID, t.Name)
}

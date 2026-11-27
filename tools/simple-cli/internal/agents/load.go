package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"simple-cli/internal/home"
)

var parseSCL = func(filePath string) ([]byte, error) {
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure scl-parser: %w", err)
	}
	output, err := home.ToolOutput(parserPath, filePath)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("scl-parser rejected the file: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("failed to run scl-parser: %w", err)
	}
	return output, nil
}

type parsedBlock struct {
	Name     any           `json:"name"`
	Type     string        `json:"type"`
	Key      string        `json:"key"`
	Children []parsedChild `json:"children"`
}

type parsedChild struct {
	Type     string        `json:"type"`
	Key      string        `json:"key"`
	Value    any           `json:"value"`
	Children []parsedChild `json:"children"`
}

// SCLParser parses one SCL file into the parser's JSON output.
type SCLParser func(filePath string) ([]byte, error)

func (child parsedChild) value() any {
	if child.Type != "block" {
		if child.Type == "kv_multi" {
			if items, ok := child.Value.([]any); ok {
				unwrapped := make([]any, len(items))
				for i, item := range items {
					if entry, ok := item.(map[string]any); ok {
						if val, exists := entry["value"]; exists {
							unwrapped[i] = val
							continue
						}
					}
					unwrapped[i] = item
				}
				return unwrapped
			}
		}
		return child.Value
	}
	value := make(map[string]any, len(child.Children))
	for _, nested := range child.Children {
		value[nested.Key] = nested.value()
	}
	return value
}

var (
	setLine         = regexp.MustCompile(`\bset\s+([^,\s]+)\s*,\s*([^\s{]+)`)
	agentDeclLine   = regexp.MustCompile(`\bset\s+(?:"(?:dev_simple_system\.)?(?:agent|agent_task_type)"|'(?:dev_simple_system\.)?(?:agent|agent_task_type)'|(?:dev_simple_system\.)?(?:agent|agent_task_type))\s*,`)
	agentRecordLine = regexp.MustCompile(`\bset\s+(?:"(?:dev_simple_system\.)?(?:agent|agent_task_type)"|'(?:dev_simple_system\.)?(?:agent|agent_task_type)'|(?:dev_simple_system\.)?(?:agent|agent_task_type))\s*,`)
	varLine         = regexp.MustCompile(`^\s*var\s+([^\s{]+)`)
	appRecordExpr   = regexp.MustCompile(`^\s*\$app_record\(\s*["']([^"']+)["']\s*\)\s*$`)
	varExpr         = regexp.MustCompile(`^\s*\$var\(\s*["']([^"']+)["']\s*\)\s*\|>\s*\$jq\(\s*["'](.*)["']\s*\)\s*$`)
	fileExpr        = regexp.MustCompile(`^\s*\$file\(\s*["']([^"']+)["']\s*\)\s*$`)
	jsonFileExpr    = regexp.MustCompile(`^\s*\$file\(\s*["']([^"']+)["']\s*\)\s*\|>\s*\$json\(\s*\)\s*$`)
	appIDExpr       = regexp.MustCompile(`application_id\s*:\s*\{\s*_eq\s*:\s*["']([^"']+)["']`)
	nameExpr        = regexp.MustCompile(`name\s*:\s*\{\s*_eq\s*:\s*["']([^"']+)["']`)
	selectNameExpr  = regexp.MustCompile(`select\s*\(\s*\.name\s*==\s*["']([^"']+)["']\s*\)`)
)

func Load(fsys fsx.FileSystem, repoRoot, appID string) (*App, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root: %w", err)
	}
	appDir := filepath.Join(repoRoot, "apps", filepath.FromSlash(appID))
	return loadFromDir(fsys, repoRoot, appID, appDir)
}

// LoadAt reads an app from the supplied directory while preserving its app ID.
func LoadAt(fsys fsx.FileSystem, repoRoot, appID, appDir string) (*App, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root: %w", err)
	}
	if !filepath.IsAbs(appDir) {
		appDir = filepath.Join(repoRoot, filepath.FromSlash(appDir))
	}
	return loadFromDir(fsys, repoRoot, appID, filepath.Clean(appDir))
}

// HasAgentDeclarations uses a loose text scan to avoid parsing unrelated apps,
// then decides from parsed records whether agent checks apply.
func HasAgentDeclarations(fsys fsx.FileSystem, repoRoot, appID string) (bool, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return false, fmt.Errorf("failed to resolve repository root: %w", err)
	}
	appDir := filepath.Join(repoRoot, "apps", filepath.FromSlash(appID))
	candidate, err := hasAgentDeclarationsAt(fsys, repoRoot, appID, appDir)
	if err != nil || !candidate {
		return false, err
	}
	app, err := loadFromDir(fsys, repoRoot, appID, appDir)
	if err != nil {
		return false, fmt.Errorf("failed to parse declarations for app %q: %w", appID, err)
	}
	return app.hasAgentCheckRecords(), nil
}

func hasAgentDeclarationsAt(fsys fsx.FileSystem, repoRoot, appID, appDir string) (bool, error) {
	return recordTextMatches(fsys, repoRoot, appID, appDir, agentDeclLine)
}

func hasAgentRelatedRecordsAt(fsys fsx.FileSystem, repoRoot, appID, appDir string) (bool, error) {
	return recordTextMatches(fsys, repoRoot, appID, appDir, agentRecordLine)
}

func recordTextMatches(fsys fsx.FileSystem, repoRoot, appID, appDir string, pattern *regexp.Regexp) (bool, error) {
	files, err := sclFiles(fsys, filepath.Join(appDir, "records"))
	if err != nil {
		return false, fmt.Errorf("failed to list records for app %q: %w", appID, err)
	}
	for _, filePath := range files {
		data, readErr := fsys.ReadFile(filePath)
		if readErr != nil {
			return false, fmt.Errorf("failed to read %s: %w", relative(repoRoot, filePath), readErr)
		}
		if pattern.Match(data) {
			return true, nil
		}
	}
	return false, nil
}

func loadFromDir(fsys fsx.FileSystem, repoRoot, appID, appDir string) (*App, error) {
	return loadFromDirWithParser(fsys, repoRoot, appID, appDir, parseSCL)
}

func loadFromDirWithParser(fsys fsx.FileSystem, repoRoot, appID, appDir string, parser SCLParser) (*App, error) {
	if parser == nil {
		parser = parseSCL
	}
	recordsDir := filepath.Join(appDir, "records")
	files, err := sclFiles(fsys, recordsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list records for app %q: %w", appID, err)
	}
	a := &App{ID: appID, RepoRoot: repoRoot, Dir: appDir, records: make(map[string]record), lookups: make(map[string]map[string]lookup)}
	var all []record
	for _, filePath := range files {
		data, readErr := fsys.ReadFile(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read %s: %w", relative(repoRoot, filePath), readErr)
		}
		output, parseErr := parser(filePath)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", relative(repoRoot, filePath), parseErr)
		}
		var blocks []parsedBlock
		if unmarshalErr := json.Unmarshal(output, &blocks); unmarshalErr != nil {
			return nil, fmt.Errorf("failed to read scl-parser output for %s: %w", relative(repoRoot, filePath), unmarshalErr)
		}
		relFile := relative(repoRoot, filePath)
		for _, block := range blocks {
			switch block.Key {
			case "set":
				names, ok := block.Name.([]any)
				if !ok || len(names) < 2 {
					continue
				}
				table, tableOK := names[0].(string)
				key, keyOK := names[1].(string)
				if !tableOK || !keyOK {
					continue
				}
				fields := make(map[string]any, len(block.Children))
				for _, field := range block.Children {
					fields[field.Key] = field.value()
				}
				all = append(all, record{table: baseTable(table), key: key, file: relFile, line: findRecordLine(data, table, key), fields: fields})
			case "var":
				name, ok := block.Name.(string)
				if !ok {
					continue
				}
				query := ""
				for _, field := range block.Children {
					if field.Key == "query" {
						query, _ = field.Value.(string)
						break
					}
				}
				entry := lookup{name: queryField(nameExpr, query), appID: queryField(appIDExpr, query), query: query,
					file: relFile, line: findVarLine(data, name)}
				if a.lookups[relFile] == nil {
					a.lookups[relFile] = make(map[string]lookup)
				}
				if _, exists := a.lookups[relFile][name]; !exists {
					a.lookups[relFile][name] = entry
				}
			}
		}
	}
	for _, item := range all {
		if _, exists := a.records[item.key]; !exists {
			a.records[item.key] = item
		}
	}
	a.buildModel(all)
	return a, nil
}

// Check parses agent records when present and scans every app for unfinished markers.
func Check(fsys fsx.FileSystem, repoRoot, appID string) ([]Problem, error) {
	return CheckWithParser(fsys, repoRoot, appID, parseSCL)
}

// CheckWithParser checks an app using the supplied parser.
func CheckWithParser(fsys fsx.FileSystem, repoRoot, appID string, parser SCLParser) ([]Problem, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root: %w", err)
	}
	appDir := filepath.Join(repoRoot, "apps", filepath.FromSlash(appID))
	return checkFromDir(fsys, repoRoot, appID, appDir, parser)
}

// CheckAt checks an app from the supplied directory while preserving its app ID.
func CheckAt(fsys fsx.FileSystem, repoRoot, appID, appDir string) ([]Problem, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root: %w", err)
	}
	if !filepath.IsAbs(appDir) {
		appDir = filepath.Join(repoRoot, filepath.FromSlash(appDir))
	}
	return checkFromDir(fsys, repoRoot, appID, filepath.Clean(appDir), parseSCL)
}

func checkFromDir(fsys fsx.FileSystem, repoRoot, appID, appDir string, parser SCLParser) ([]Problem, error) {
	declared, err := hasAgentRelatedRecordsAt(fsys, repoRoot, appID, appDir)
	if err != nil {
		return nil, err
	}
	var a *App
	if declared {
		a, err = loadFromDirWithParser(fsys, repoRoot, appID, appDir, parser)
		if err != nil {
			return nil, err
		}
	} else {
		a = &App{ID: appID, RepoRoot: repoRoot, Dir: appDir, records: map[string]record{}, lookups: map[string]map[string]lookup{}}
	}
	return a.check(fsys)
}

func (a *App) buildModel(all []record) {
	actionsByName := make(map[string]string)
	for _, item := range all {
		switch item.table {
		case "knowledge":
			a.Knowledge = append(a.Knowledge, &Knowledge{Key: item.key, File: item.file, Line: item.line, Title: str(item.fields["title"]), Body: parseBody(item.fields["body"])})
		case "agent":
			a.AgentRecords = append(a.AgentRecords, &Agent{Key: item.key, File: item.file, Line: item.line, DisplayName: str(item.fields["display_name"]), Description: str(item.fields["description"]), Status: str(item.fields["status"]), Instructions: refFor(item.fields["instructions_id"], a.lookups[item.file], a.ID, actionsByName)})
		case "task_type":
			shape, _ := parseFilePath(item.fields["schema"])
			states, _ := parseFilePath(item.fields["states"])
			a.TaskTypes = append(a.TaskTypes, &TaskType{Key: item.key, File: item.file, Line: item.line, Name: str(item.fields["name"]), PluralName: str(item.fields["plural_name"]), Description: str(item.fields["description"]), PlaybookRef: refFor(item.fields["playbook_id"], a.lookups[item.file], a.ID, actionsByName), ShapePath: shape, StatesPath: states, WakeIntervalSeconds: int64(num(item.fields["wake_interval_seconds"]))})
		case "logic":
			name := str(item.fields["name"])
			a.Actions = append(a.Actions, &Action{Key: item.key, File: item.file, Line: item.line, Name: name, DisplayName: first(str(item.fields["display_name"]), str(item.fields["label"]), name), Description: str(item.fields["description"])})
			if _, exists := actionsByName[name]; !exists {
				actionsByName[name] = item.key
			}
		}
	}
	for _, agent := range a.AgentRecords {
		agent.Instructions = refFor(a.records[agent.Key].fields["instructions_id"], a.lookups[agent.File], a.ID, actionsByName)
		if item, ok := resolveRecord(a.records, agent.Instructions); ok && item.table == "knowledge" {
			agent.Persona = a.knowledgeByKey(item.key)
		}
	}
	for _, taskType := range a.TaskTypes {
		taskType.PlaybookRef = refFor(a.records[taskType.Key].fields["playbook_id"], a.lookups[taskType.File], a.ID, actionsByName)
		if item, ok := resolveRecord(a.records, taskType.PlaybookRef); ok && item.table == "knowledge" {
			taskType.Playbook = a.knowledgeByKey(item.key)
		}
	}
	for _, item := range all {
		a.buildLink(item, actionsByName)
	}
	for _, task := range a.TaskTypes {
		var defaults []*AgentTaskTypeLink
		for _, link := range a.AgentTaskTypeLinks {
			if link.TaskTypeRef.Key == task.Key && link.Role == "executor" && link.IsDefault {
				defaults = append(defaults, link)
			}
		}
		if len(defaults) == 1 {
			task.DefaultAgentRef = defaults[0].AgentRef
			task.DefaultAgent = a.resolveAgent(defaults[0].AgentRef)
		}
	}
	sort.Slice(a.AgentRecords, func(i, j int) bool { return a.AgentRecords[i].Key < a.AgentRecords[j].Key })
	sort.Slice(a.TaskTypes, func(i, j int) bool { return a.TaskTypes[i].Key < a.TaskTypes[j].Key })
	sort.Slice(a.Knowledge, func(i, j int) bool { return a.Knowledge[i].Key < a.Knowledge[j].Key })
	sort.Slice(a.Actions, func(i, j int) bool { return a.Actions[i].Key < a.Actions[j].Key })
}

func (a *App) buildLink(item record, actionKeys map[string]string) {
	fields := item.fields
	switch item.table {
	case "agent_task_type":
		a.AgentTaskTypeLinks = append(a.AgentTaskTypeLinks, &AgentTaskTypeLink{Key: item.key, File: item.file, Line: item.line, AgentRef: refFor(fields["agent_id"], a.lookups[item.file], a.ID, actionKeys), TaskTypeRef: refFor(fields["task_type_id"], a.lookups[item.file], a.ID, actionKeys), Role: str(fields["role"]), IsDefault: boolValue(fields["is_default"])})
	case "task_type_skill":
		a.TaskTypeSkillLinks = append(a.TaskTypeSkillLinks, &TaskTypeSkillLink{Key: item.key, File: item.file, Line: item.line, TaskTypeRef: refFor(fields["task_type_id"], a.lookups[item.file], a.ID, actionKeys), KnowledgeRef: refFor(fields["knowledge_id"], a.lookups[item.file], a.ID, actionKeys), Position: num(fields["position"])})
	case "task_type_tool":
		ref := refFor(fields["logic_id"], a.lookups[item.file], a.ID, actionKeys)
		tool := ToolRef{AppID: ref.AppID, Name: ref.Name}
		if !ref.Lookup && ref.Readable {
			target, ok := a.records[ref.Key]
			if !ok || target.table != "logic" {
				tool = ToolRef{ActionKey: ref.Key, Name: ref.Key, OwnAction: true}
			} else {
				tool = ToolRef{ActionKey: target.key, Name: str(target.fields["name"]), OwnAction: true}
			}
		}
		if ref.Lookup && ref.AppID == a.ID {
			tool = ToolRef{ActionKey: ref.Key, Name: ref.Name, OwnAction: true}
		}
		a.TaskTypeToolLinks = append(a.TaskTypeToolLinks, &TaskTypeToolLink{Key: item.key, File: item.file, Line: item.line, TaskTypeRef: refFor(fields["task_type_id"], a.lookups[item.file], a.ID, actionKeys), LogicRef: ref, Tool: tool})
	case "logic_skill":
		a.LogicSkillLinks = append(a.LogicSkillLinks, &LogicSkillLink{Key: item.key, File: item.file, Line: item.line, LogicRef: refFor(fields["logic_id"], a.lookups[item.file], a.ID, actionKeys), KnowledgeRef: refFor(fields["knowledge_id"], a.lookups[item.file], a.ID, actionKeys)})
	}
}

func (a *App) resolveAgent(ref Reference) *Agent {
	if item, ok := resolveRecord(a.records, ref); ok {
		for _, v := range a.AgentRecords {
			if v.Key == item.key {
				return v
			}
		}
	}
	return nil
}

func sclFiles(fsys fsx.FileSystem, root string) ([]string, error) {
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
				continue
			}
			if strings.EqualFold(filepath.Ext(entry.Name()), ".scl") {
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

func baseTable(table string) string {
	const platformPrefix = "dev_simple_system."
	table = strings.Trim(strings.TrimSpace(table), `"'`)
	if strings.HasPrefix(table, platformPrefix) {
		return strings.TrimPrefix(table, platformPrefix)
	}
	return table
}

func findVarLine(data []byte, name string) int {
	for index, line := range strings.Split(string(data), "\n") {
		match := varLine.FindStringSubmatch(line)
		if len(match) == 2 && match[1] == name {
			return index + 1
		}
	}
	return 0
}
func relative(root, path string) string {
	result, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(result)
}

func findRecordLine(data []byte, table, key string) int {
	for index, line := range strings.Split(string(data), "\n") {
		for _, match := range setLine.FindAllStringSubmatch(line, -1) {
			if len(match) == 3 && baseTable(match[1]) == baseTable(table) && (match[2] == key || strings.Trim(match[2], `"'`) == strings.Trim(key, `"'`)) {
				return index + 1
			}
		}
	}
	return 0
}

func parseAppRecord(raw string) (string, bool) {
	match := appRecordExpr.FindStringSubmatch(raw)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}
func parseVarRef(raw string) (string, string, bool) {
	match := varExpr.FindStringSubmatch(raw)
	if len(match) != 3 {
		return "", "", false
	}
	return match[1], match[2], true
}
func queryToolName(query string) string { return queryField(selectNameExpr, query) }
func queryField(pattern *regexp.Regexp, query string) string {
	match := pattern.FindStringSubmatch(query)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func parseBody(raw any) Body {
	text, ok := raw.(string)
	if !ok {
		return Body{}
	}
	body := Body{Raw: text}
	if match := fileExpr.FindStringSubmatch(text); len(match) == 2 {
		body.Path = filepath.ToSlash(filepath.Clean(match[1]))
		body.Readable = true
		return body
	}
	if strings.HasPrefix(strings.TrimSpace(text), "$") {
		return body
	}
	body.Inline, body.Readable = text, true
	return body
}

func parseFilePath(raw any) (string, bool) {
	text, ok := raw.(string)
	if !ok {
		return "", false
	}
	match := jsonFileExpr.FindStringSubmatch(text)
	if len(match) != 2 {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(match[1])), true
}

func str(value any) string { result, _ := value.(string); return result }
func num(value any) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := strconv.Atoi(string(value))
		return parsed
	}
	return 0
}
func boolValue(value any) bool { result, _ := value.(bool); return result }
func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func readBody(fsys fsx.FileSystem, a *App, body Body) (string, string, int, bool) {
	if !body.Readable {
		return "", "", 0, false
	}
	if body.Path == "" {
		return body.Inline, "", 0, true
	}
	fullPath := filepath.Join(a.Dir, filepath.FromSlash(body.Path))
	data, err := fsys.ReadFile(fullPath)
	if err != nil {
		return "", relative(a.RepoRoot, fullPath), 0, false
	}
	return string(data), relative(a.RepoRoot, fullPath), 0, true
}

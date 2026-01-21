// Package scaffold provides scaffolding utilities for Simple Platform monorepos.
package scaffold

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"strings"
	"text/template"
)

//go:embed templates
var TemplatesFS embed.FS

// MonorepoConfig holds configuration for creating a new monorepo.
type MonorepoConfig struct {
	ProjectName string
	TenantName  string
}

// CreateMonorepoStructure creates all directories and files for a new monorepo.
//
// It creates:
//   - apps/ directory (empty)
//   - .simple/context/ directory with documentation files
//   - AGENTS.md at root
//   - README.md at root (templated with project name)
//   - simple.scl (templated with tenant name)
//
// Returns an error if the target path already exists.
func CreateMonorepoStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string, cfg MonorepoConfig) error {
	// Validate: path must not exist
	if PathExists(fsys, rootPath) {
		return fmt.Errorf("path already exists: %s", rootPath)
	}

	// Create directories
	if err := createDirectories(fsys, rootPath); err != nil {
		return err
	}

	// Copy context documentation
	if err := copyContextDocs(fsys, tplFS, rootPath); err != nil {
		return err
	}

	// Copy Antigravity Agent Context (.agent/)
	if err := copyAgentDocs(fsys, tplFS, rootPath); err != nil {
		return err
	}

	// Copy AGENTS.md
	if err := copyTemplate(fsys, tplFS, "templates/AGENTS.md", filepath.Join(rootPath, "AGENTS.md")); err != nil {
		return err
	}

	// Copy .cursorrules (AI Context Bridge)
	if err := copyTemplate(fsys, tplFS, "templates/cursorrules", filepath.Join(rootPath, ".cursorrules")); err != nil {
		return err
	}

	// Generate README.md with project name
	data := map[string]string{"ProjectName": cfg.ProjectName}
	if err := renderTemplate(fsys, tplFS, "templates/README.md", filepath.Join(rootPath, "README.md"), data); err != nil {
		return err
	}

	// Copy .gitignore
	if err := copyTemplate(fsys, tplFS, "templates/gitignore", filepath.Join(rootPath, ".gitignore")); err != nil {
		return err
	}

	// Generate simple.scl with tenant configuration
	simpleSCL := generateSimpleSCL(cfg.TenantName)
	if err := fsys.WriteFile(filepath.Join(rootPath, "simple.scl"), []byte(simpleSCL), fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write simple.scl: %w", err)
	}

	return nil
}

// generateSimpleSCL creates the simple.scl content with proper endpoint URLs.
// URL structure:
//   - Non-prod: <tenant>-<env>.on.simple.dev
//   - Prod: <tenant>.on.simple.dev
func generateSimpleSCL(tenant string) string {
	return fmt.Sprintf(`tenant %s

env dev {
  endpoint %s-dev.on.simple.dev
  api_key $SIMPLE_DEV_API_KEY
}

env staging {
  endpoint %s-staging.on.simple.dev
  api_key $SIMPLE_STAGING_API_KEY
}

env prod {
  endpoint %s.on.simple.dev
  api_key $SIMPLE_PROD_API_KEY
}
`, tenant, tenant, tenant, tenant)
}

// CreateAppStructure scaffolds a new application inside the apps/ directory.
//
// It creates:
//   - apps/<appID>/
//   - apps/<appID>/app.scl
//   - apps/<appID>/tables.scl
//   - apps/<appID>/actions/
//   - apps/<appID>/records/
//   - apps/<appID>/scripts/
func CreateAppStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath, appID, displayName, description string) error {
	appPath := filepath.Join(rootPath, "apps", appID)

	// Validate: path must not exist
	if PathExists(fsys, appPath) {
		return fmt.Errorf("app already exists: %s", appID)
	}

	// Create app directory
	if err := fsys.MkdirAll(appPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Create standard subdirectories
	subDirs := []string{"actions", "records", "scripts"}
	for _, dir := range subDirs {
		if err := fsys.MkdirAll(filepath.Join(appPath, dir), fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", dir, err)
		}
	}

	// Render app.scl
	data := map[string]string{
		"AppID":       appID,
		"DisplayName": displayName,
		"Description": description,
	}
	if err := renderTemplate(fsys, tplFS, "templates/app/app.scl", filepath.Join(appPath, "app.scl"), data); err != nil {
		return err
	}

	// Copy tables.scl
	if err := copyTemplate(fsys, tplFS, "templates/app/tables.scl", filepath.Join(appPath, "tables.scl")); err != nil {
		return err
	}

	return nil
}

// ActionConfig holds the configuration for creating a new action.
type ActionConfig struct {
	AppID        string
	ActionName   string
	DisplayName  string
	Description  string
	Scope        string
	ExecutionEnv string
}

// TriggerConfig holds usage configuration for creating a new trigger.
type TriggerConfig struct {
	AppID          string
	TriggerName    string // kebab-case, e.g., "daily-sync"
	TriggerNameScl string // underscore, e.g., "daily_sync"
	DisplayName    string
	Description    string
	TriggerType    string // "timed", "db", "webhook"
	ActionName     string // action to link to

	// Timed trigger fields
	Frequency   string
	Interval    int
	Time        string
	Timezone    string
	Days        string // JSON array string
	Weekdays    bool
	Weekends    bool
	WeekOfMonth string
	StartAt     string
	EndAt       string
	OnOverlap   string

	// DB event fields
	TableName  string
	Operations string // JSON array string
	Condition  string

	// Webhook fields
	Method   string
	IsPublic bool
}

// CreateTriggerStructure scaffolds a new trigger inside an app.
//
// It creates or appends to:
//   - records/20_triggers.scl
//   - records/30_trigger_actions.scl
func CreateTriggerStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string, cfg TriggerConfig) error {
	appPath := filepath.Join(rootPath, "apps", cfg.AppID)

	// Validate: app must exist
	if !PathExists(fsys, appPath) {
		return fmt.Errorf("app does not exist: %s", cfg.AppID)
	}

	recordsPath := filepath.Join(appPath, "records")
	if err := fsys.MkdirAll(recordsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create records directory: %w", err)
	}

	// Check for duplicate trigger
	existingTriggersFile := filepath.Join(recordsPath, "20_triggers.scl")
	if PathExists(fsys, existingTriggersFile) {
		exists, err := checkSCLEntityExists(existingTriggersFile, "set", "dev_simple_system.trigger", cfg.TriggerNameScl)
		if err != nil {
			return fmt.Errorf("failed to check trigger existence for trigger %q in file %s: %w", cfg.TriggerNameScl, existingTriggersFile, err)
		}
		if exists {
			return fmt.Errorf("trigger already exists: %s", cfg.TriggerNameScl)
		}
	}

	// Determine template file based on type
	var templateFile string
	switch cfg.TriggerType {
	case "timed":
		templateFile = "templates/trigger/20_triggers_timed.scl"
	case "db":
		templateFile = "templates/trigger/20_triggers_db.scl"
	case "webhook":
		templateFile = "templates/trigger/20_triggers_webhook.scl"
	default:
		return fmt.Errorf("unknown trigger type: %s", cfg.TriggerType)
	}

	// Prepare data map for templates
	data := map[string]interface{}{
		"AppID":          cfg.AppID,
		"TriggerName":    cfg.TriggerName,
		"TriggerNameScl": cfg.TriggerNameScl,
		"DisplayName":    cfg.DisplayName,
		"Description":    cfg.Description,
		"TriggerType":    cfg.TriggerType,
		"ActionName":     cfg.ActionName,
		// Timed
		"Frequency":   cfg.Frequency,
		"Interval":    cfg.Interval,
		"Time":        cfg.Time,
		"Timezone":    cfg.Timezone,
		"Days":        cfg.Days,
		"Weekdays":    cfg.Weekdays,
		"Weekends":    cfg.Weekends,
		"WeekOfMonth": cfg.WeekOfMonth,
		"StartAt":     cfg.StartAt,
		"EndAt":       cfg.EndAt,
		"OnOverlap":   cfg.OnOverlap,
		// DB
		"TableName":  cfg.TableName,
		"Operations": cfg.Operations,
		"Condition":  cfg.Condition,
		// Webhook
		"Method":   cfg.Method,
		"IsPublic": cfg.IsPublic,
	}

	// 1. Append to 20_triggers.scl
	triggersFile := filepath.Join(recordsPath, "20_triggers.scl")
	if err := appendTriggerRecord(fsys, tplFS, templateFile, triggersFile, data); err != nil {
		return fmt.Errorf("failed to append trigger record: %w", err)
	}

	// 2. Append to 30_trigger_actions.scl
	triggerActionsFile := filepath.Join(recordsPath, "30_trigger_actions.scl")
	if err := appendTriggerRecord(fsys, tplFS, "templates/trigger/30_trigger_actions.scl", triggerActionsFile, data); err != nil {
		return fmt.Errorf("failed to append trigger action link: %w", err)
	}

	return nil
}

// appendTriggerRecord appends a record to a destination file using the specified template.
func appendTriggerRecord(fsys fsx.FileSystem, tplFS fsx.TemplateFS, templatePath, dst string, data interface{}) error {
	// Read the template
	content, err := tplFS.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}

	tmpl, err := template.New(filepath.Base(templatePath)).Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", templatePath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template %s: %w", templatePath, err)
	}

	// Check if file exists and append, otherwise create
	var existingContent []byte
	if PathExists(fsys, dst) {
		existingContent, err = fsys.ReadFile(dst)
		if err != nil {
			return fmt.Errorf("failed to read existing %s: %w", dst, err)
		}
	}

	var finalContent []byte
	if len(existingContent) > 0 {
		// Append with newline separator
		finalContent = append(existingContent, '\n')
		finalContent = append(finalContent, buf.Bytes()...)
	} else {
		finalContent = buf.Bytes()
	}

	if err := fsys.WriteFile(dst, finalContent, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// CreateActionStructure scaffolds a new action inside an app's actions/ directory.
//
// It creates:
//   - apps/<appID>/actions/<actionName>/
//   - apps/<appID>/actions/<actionName>/package.json
//   - apps/<appID>/actions/<actionName>/index.ts
//   - apps/<appID>/actions/<actionName>/tsconfig.json
//   - apps/<appID>/actions/<actionName>/vitest.config.ts
//   - apps/<appID>/actions/<actionName>/tests/helpers.ts
//   - apps/<appID>/actions/<actionName>/tests/index.test.ts
//   - apps/<appID>/records/10_actions.scl (appended or created)
func CreateActionStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string, cfg ActionConfig) error {
	appPath := filepath.Join(rootPath, "apps", cfg.AppID)

	// Validate: app must exist
	if !PathExists(fsys, appPath) {
		return fmt.Errorf("app does not exist: %s", cfg.AppID)
	}

	// Check for duplicate action in SCL
	recordsPath := filepath.Join(appPath, "records")
	actionsScl := filepath.Join(recordsPath, "10_actions.scl")
	if PathExists(fsys, actionsScl) {
		actionNameScl := strings.ReplaceAll(cfg.ActionName, "-", "_")
		exists, err := checkSCLEntityExists(actionsScl, "set", "dev_simple_system.logic", actionNameScl)
		if err != nil {
			return fmt.Errorf("failed to check action existence: %w", err)
		}
		if exists {
			return fmt.Errorf("action already exists: %s", actionNameScl)
		}
	}

	actionPath := filepath.Join(appPath, "actions", cfg.ActionName)

	// Validate: action must not exist
	if PathExists(fsys, actionPath) {
		return fmt.Errorf("action already exists: %s", cfg.ActionName)
	}

	// Create action directory and tests subdirectory
	if err := fsys.MkdirAll(actionPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create action directory: %w", err)
	}

	testsPath := filepath.Join(actionPath, "tests")
	if err := fsys.MkdirAll(testsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create tests directory: %w", err)
	}

	// Template data
	// ActionNameScl replaces hyphens with underscores for SCL identifiers
	data := map[string]string{
		"ActionName":    cfg.ActionName,
		"ActionNameScl": strings.ReplaceAll(cfg.ActionName, "-", "_"),
		"DisplayName":   cfg.DisplayName,
		"Description":   cfg.Description,
		"Scope":         cfg.Scope,
		"ExecutionEnv":  cfg.ExecutionEnv,
	}

	// Render action files
	actionFiles := []struct {
		src string
		dst string
	}{
		{"templates/action/package.json", filepath.Join(actionPath, "package.json")},
		{"templates/action/index.ts", filepath.Join(actionPath, "index.ts")},
		{"templates/action/tsconfig.json", filepath.Join(actionPath, "tsconfig.json")},
		{"templates/action/vitest.config.ts", filepath.Join(actionPath, "vitest.config.ts")},
		{"templates/action/tests/helpers.ts", filepath.Join(testsPath, "helpers.ts")},
		{"templates/action/tests/index.test.ts", filepath.Join(testsPath, "index.test.ts")},
	}

	for _, f := range actionFiles {
		if err := renderTemplate(fsys, tplFS, f.src, f.dst, data); err != nil {
			return err
		}
	}

	// Create or append to records/10_actions.scl
	if err := fsys.MkdirAll(recordsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create records directory: %w", err)
	}

	if err := appendActionRecord(fsys, tplFS, actionsScl, data); err != nil {
		return err
	}

	return nil
}

// appendActionRecord appends an action record to the 10_actions.scl file.
func appendActionRecord(fsys fsx.FileSystem, tplFS fsx.TemplateFS, dst string, data map[string]string) error {
	// Read the template
	content, err := tplFS.ReadFile("templates/action/10_actions.scl")
	if err != nil {
		return fmt.Errorf("failed to read action template: %w", err)
	}

	tmpl, err := template.New("action").Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse action template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute action template: %w", err)
	}

	// Check if file exists and append, otherwise create
	var existingContent []byte
	if PathExists(fsys, dst) {
		existingContent, err = fsys.ReadFile(dst)
		if err != nil {
			return fmt.Errorf("failed to read existing %s: %w", dst, err)
		}
	}

	var finalContent []byte
	if len(existingContent) > 0 {
		// Append with newline separator
		finalContent = append(existingContent, '\n')
		finalContent = append(finalContent, buf.Bytes()...)
	} else {
		finalContent = buf.Bytes()
	}

	if err := fsys.WriteFile(dst, finalContent, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// PathExists checks if a path exists on the filesystem.
func PathExists(fsys fsx.FileSystem, path string) bool {
	_, err := fsys.Stat(path)
	return !os.IsNotExist(err)
}

// createDirectories creates the required directory structure.
func createDirectories(fsys fsx.FileSystem, rootPath string) error {
	dirs := []string{
		"apps",
		".simple/context",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(rootPath, dir)
		if err := fsys.MkdirAll(fullPath, fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// copyContextDocs copies all files from templates/context/ to .simple/context/.
func copyContextDocs(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string) error {
	srcDir := "templates/context"
	dstDir := filepath.Join(rootPath, ".simple/context")

	entries, err := tplFS.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read templates/context: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue // Skip directories
		}

		srcPath := srcDir + "/" + entry.Name()
		dstPath := filepath.Join(dstDir, entry.Name())

		if err := copyTemplate(fsys, tplFS, srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// copyAgentDocs copies the entire .agent context structure recursively.
func copyAgentDocs(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string) error {
	srcDir := "templates/agent"
	dstDir := filepath.Join(rootPath, ".agent")

	// Create .agent root
	if err := fsys.MkdirAll(dstDir, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create .agent directory: %w", err)
	}

	return copyRecursive(fsys, tplFS, srcDir, dstDir)
}

// copyRecursive copies a directory recursively from embedded FS to disk.
func copyRecursive(fsys fsx.FileSystem, tplFS fsx.TemplateFS, srcDir, dstDir string) error {
	entries, err := tplFS.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", srcDir, err)
	}

	for _, entry := range entries {
		srcPath := srcDir + "/" + entry.Name()
		dstPath := filepath.Join(dstDir, entry.Name())

		if entry.IsDir() {
			if err := fsys.MkdirAll(dstPath, fsx.DirPerm); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
			}
			if err := copyRecursive(fsys, tplFS, srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyTemplate(fsys, tplFS, srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyTemplate copies a file from the embedded filesystem to disk.
func copyTemplate(fsys fsx.FileSystem, tplFS fsx.TemplateFS, src, dst string) error {
	content, err := tplFS.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", src, err)
	}

	if err := fsys.WriteFile(dst, content, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// renderTemplate renders a Go template with data and writes to disk.
func renderTemplate(fsys fsx.FileSystem, tplFS fsx.TemplateFS, src, dst string, data map[string]string) error {
	content, err := tplFS.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", src, err)
	}

	tmpl, err := template.New(filepath.Base(src)).Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", src, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template %s: %w", src, err)
	}

	if err := fsys.WriteFile(dst, buf.Bytes(), fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// BehaviorConfig holds configuration for creating a new record behavior.
type BehaviorConfig struct {
	AppID     string
	TableName string
}

// CreateBehaviorStructure scaffolds a new record behavior.
//
// It creates:
//   - apps/<appID>/scripts/record-behaviors/<tableName>.js
//   - apps/<appID>/scripts/record-behaviors/<tableName>.test.js
//   - apps/<appID>/records/10_behaviors.scl (appended or created)
func CreateBehaviorStructure(fsys fsx.FileSystem, tplFS fsx.TemplateFS, rootPath string, cfg BehaviorConfig) error {
	appPath := filepath.Join(rootPath, "apps", cfg.AppID)

	// Validate: app must exist
	if !PathExists(fsys, appPath) {
		return fmt.Errorf("app does not exist: %s", cfg.AppID)
	}

	scriptsPath := filepath.Join(appPath, "scripts", "record-behaviors")
	if err := fsys.MkdirAll(scriptsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create scripts directory: %w", err)
	}

	recordsPath := filepath.Join(appPath, "records")
	if err := fsys.MkdirAll(recordsPath, fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to create records directory: %w", err)
	}

	// Check for duplicate behavior script
	scriptFile := filepath.Join(scriptsPath, cfg.TableName+".js")
	if PathExists(fsys, scriptFile) {
		return fmt.Errorf("behavior script already exists: %s", scriptFile)
	}

	// Template data
	data := map[string]string{
		"AppID":     cfg.AppID,
		"TableName": cfg.TableName,
	}

	// Render script.js
	if err := renderTemplate(fsys, tplFS, "templates/behavior/script.js", scriptFile, data); err != nil {
		return err
	}

	// Render script.test.js
	testFile := filepath.Join(scriptsPath, cfg.TableName+".test.js")
	if err := renderTemplate(fsys, tplFS, "templates/behavior/script.test.js", testFile, data); err != nil {
		return err
	}

	// Append to 10_behaviors.scl
	behaviorsScl := filepath.Join(recordsPath, "10_behaviors.scl")

	// Read existing content to check for duplicates in SCL
	if PathExists(fsys, behaviorsScl) {
		behaviorName := fmt.Sprintf("%s_behavior", cfg.TableName)
		exists, err := checkSCLEntityExists(behaviorsScl, "set", "dev_simple_system.record_behavior", behaviorName)
		if err != nil {
			return fmt.Errorf("failed to check behavior existence for %s in %s: %w", behaviorName, behaviorsScl, err)
		}
		if exists {
			return fmt.Errorf("behavior registration already exists in SCL for table: %s", cfg.TableName)
		}
	}

	if err := appendBehaviorRecord(fsys, tplFS, behaviorsScl, data); err != nil {
		return err
	}

	return nil
}

// appendBehaviorRecord appends a behavior record to the 10_behaviors.scl file.
func appendBehaviorRecord(fsys fsx.FileSystem, tplFS fsx.TemplateFS, dst string, data map[string]string) error {
	// Read the template
	content, err := tplFS.ReadFile("templates/behavior/10_behaviors.scl")
	if err != nil {
		return fmt.Errorf("failed to read behavior template: %w", err)
	}

	tmpl, err := template.New("behavior").Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse behavior template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute behavior template: %w", err)
	}

	// Check if file exists and append, otherwise create
	var existingContent []byte
	if PathExists(fsys, dst) {
		existingContent, err = fsys.ReadFile(dst)
		if err != nil {
			return fmt.Errorf("failed to read existing %s: %w", dst, err)
		}
	}

	var finalContent []byte
	if len(existingContent) > 0 {
		// Append with newline separator
		finalContent = append(existingContent, '\n')
		finalContent = append(finalContent, buf.Bytes()...)
	} else {
		finalContent = buf.Bytes()
	}

	if err := fsys.WriteFile(dst, finalContent, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}

	return nil
}

// checkSCLEntityExists uses scl-parser CLI to check if a specific entity exists in an SCL file.
// It is defined as a package-level variable (rather than a regular function) so tests can
// replace it with a stub or mock implementation when needed.
var checkSCLEntityExists = func(filePath string, entityName string, entityType string, blockKey string) (bool, error) {
	// check if scl-parser is installed and get path
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return false, fmt.Errorf("failed to ensure scl-parser: %w", err)
	}

	cmd := exec.Command(parserPath, filePath)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return false, fmt.Errorf("scl-parser failed for %s: %s", filePath, string(exitErr.Stderr))
		}
		return false, fmt.Errorf("failed to run scl-parser: %w", err)
	}

	var blocks []map[string]interface{}
	if err := json.Unmarshal(output, &blocks); err != nil {
		return false, fmt.Errorf("failed to parse scl-parser output: %w", err)
	}

	for _, block := range blocks {
		if block["type"] == "block" {
			key, ok := block["key"].(string)
			if ok && key == blockKey {
				// Name can be a string or a list of strings
				nameVal := block["name"]

				// For 'set type, name', nameVal should be a list ["type", "name"]
				if nameList, ok := nameVal.([]interface{}); ok {
					if len(nameList) >= 2 {
						// Need to cast interface{} to string
						if typeStr, ok := nameList[0].(string); ok && typeStr == entityType {
							if nameStr, ok := nameList[1].(string); ok && nameStr == entityName {
								return true, nil
							}
						}
					}
				} else if nameStr, ok := nameVal.(string); ok {
					// Fallback for simple blocks like 'table user'
					if entityType == "" && nameStr == entityName {
						return true, nil
					}
				}
			}
		}
	}

	return false, nil
}

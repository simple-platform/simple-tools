package scaffold

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"strings"
	"testing"
	"time"
)

// Tests follow...

func TestCreateMonorepoStructure_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mockFS  *fsx.MockFileSystem
		mockTpl *fsx.MockTemplateFS
		wantErr string
	}{
		{
			name: "mkdir failed",
			mockFS: &fsx.MockFileSystem{
				MkdirAllErr: errors.New("mkdir failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "failed to create directory apps: mkdir failed",
		},
		{
			name: "write failed",
			mockFS: &fsx.MockFileSystem{
				WriteFileErr: errors.New("write failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "write failed",
		},
		{
			name:   "agents copy failed",
			mockFS: &fsx.MockFileSystem{},
			mockTpl: &fsx.MockTemplateFS{
				ReadErrors: map[string]error{
					"templates/AGENTS.md": errors.New("read agents failed"),
				},
			},
			wantErr: "read agents failed",
		},
		{
			name:   "readme render failed",
			mockFS: &fsx.MockFileSystem{},
			mockTpl: &fsx.MockTemplateFS{
				ReadErrors: map[string]error{
					"templates/README.md": errors.New("read readme failed"),
				},
			},
			wantErr: "read readme failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateMonorepoStructure(tt.mockFS, tt.mockTpl, "/path/to/project", MonorepoConfig{ProjectName: "project", TenantName: "test"})
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %v, want substring %v", err, tt.wantErr)
			}
		})
	}
}

func TestRenderTemplate_Error(t *testing.T) {
	// Tests that renderTemplate correctly propagates write errors.
	mockFS := &fsx.MockFileSystem{
		WriteFileErr: errors.New("write failed"),
	}
	mockTpl := &fsx.MockTemplateFS{}

	err := renderTemplate(mockFS, mockTpl, "templates/README.md", "README.md", nil)
	if err == nil {
		t.Error("Expected error from renderTemplate write")
	}
}

func TestRenderTemplate_ParseError(t *testing.T) {
	mockFS := &fsx.MockFileSystem{}
	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"bad.tmpl": []byte("{{ .Unclosed "),
		},
	}

	err := renderTemplate(mockFS, mockTpl, "bad.tmpl", "out", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse template") {
		t.Errorf("Expected parse error, got: %v", err)
	}
}

func TestCreateAppStructure_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mockFS  *fsx.MockFileSystem
		mockTpl *fsx.MockTemplateFS
		wantErr string
	}{
		{
			name: "mkdir failed",
			mockFS: &fsx.MockFileSystem{
				MkdirAllErr: errors.New("mkdir failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "failed to create app directory: mkdir failed",
		},
		{
			name:   "tables copy failed",
			mockFS: &fsx.MockFileSystem{},
			mockTpl: &fsx.MockTemplateFS{
				ReadErrors: map[string]error{
					"templates/app/tables.scl": errors.New("read tables failed"),
				},
			},
			wantErr: "read tables failed",
		},
		{
			name: "render failed",
			mockFS: &fsx.MockFileSystem{
				WriteFileErr: errors.New("write failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "failed to write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateAppStructure(tt.mockFS, tt.mockTpl, "/root", "com.test", "Test", "Desc")
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %v, want substring %v", err, tt.wantErr)
			}
		})
	}
}

func TestPathExists_Error(t *testing.T) {
	// Tests that pathExists returns true for errors other than os.ErrNotExist.
	// This simulates permission issues or other filesystem errors where the path effectively "exists" or blocks creation.
	mockFS := &fsx.MockFileSystem{
		StatErr: errors.New("permission denied"),
	}
	exists := PathExists(mockFS, "/foo")
	if !exists {
		t.Error("pathExists should return true if error is not IsNotExist")
	}
}

func TestTemplateReadError(t *testing.T) {
	mockFS := &fsx.MockFileSystem{}
	mockTpl := &fsx.MockTemplateFS{
		ReadFileErr: errors.New("read failed"),
	}

	// Test copyTemplate read error
	err := copyTemplate(mockFS, mockTpl, "src", "dst")
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("Expected read error, got: %v", err)
	}

	// Test renderTemplate read error
	err = renderTemplate(mockFS, mockTpl, "src", "dst", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to read template") {
		t.Errorf("Expected read template error, got: %v", err)
	}
}

func TestContextReadDirError(t *testing.T) {
	mockFS := &fsx.MockFileSystem{}
	mockTpl := &fsx.MockTemplateFS{
		ReadDirErr: errors.New("read dir failed"),
	}

	err := copyContextDocs(mockFS, mockTpl, "/root")
	if err == nil || !strings.Contains(err.Error(), "failed to read templates/context") {
		t.Errorf("Expected read dir error, got: %v", err)
	}
}

// Tests for CreateActionStructure

func TestCreateActionStructure_Success(t *testing.T) {
	// Use a mock that tracks what was written
	written := make(map[string][]byte)

	mockFS := &mockWriteTrackingFS{
		written: written,
		statFn: func(name string) bool {
			// Simulate that app exists but action and 10_actions.scl don't
			if strings.Contains(name, "10_actions.scl") {
				return false
			}
			return strings.Contains(name, "apps/com.test") && !strings.Contains(name, "actions/my-action")
		},
	}

	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"templates/action/package.json":        []byte(`{"name": "@{{.Scope}}/action-{{.ActionName}}"}`),
			"templates/action/index.ts":            []byte("// {{.ActionName}}"),
			"templates/action/tsconfig.json":       []byte("{}"),
			"templates/action/vitest.config.ts":    []byte("export default {}"),
			"templates/action/tests/helpers.ts":    []byte("// helpers"),
			"templates/action/tests/index.test.ts": []byte("// test {{.ActionName}}"),
			"templates/action/10_actions.scl":      []byte("set logic, {{.ActionName}} {}"),
		},
	}

	cfg := ActionConfig{
		AppID:        "com.test",
		ActionName:   "my-action",
		DisplayName:  "My Action",
		Description:  "Test description",
		Scope:        "mycompany",
		ExecutionEnv: "server",
	}

	err := CreateActionStructure(mockFS, mockTpl, "/root", cfg)
	if err != nil {
		t.Fatalf("CreateActionStructure failed: %v", err)
	}

	// Verify package.json was written with correct content
	pkgJson := written["/root/apps/com.test/actions/my-action/package.json"]
	if !strings.Contains(string(pkgJson), "@mycompany/action-my-action") {
		t.Errorf("package.json doesn't contain correct scope, got: %s", string(pkgJson))
	}

	// Verify 10_actions.scl was created
	actionsScl := written["/root/apps/com.test/records/10_actions.scl"]
	if !strings.Contains(string(actionsScl), "my-action") {
		t.Errorf("10_actions.scl doesn't contain action name, got: %s", string(actionsScl))
	}
}

func TestCreateActionStructure_AppNotExists(t *testing.T) {
	mockFS := &fsx.MockFileSystem{} // Default: everything returns NotExist

	cfg := ActionConfig{
		AppID:      "nonexistent",
		ActionName: "test",
	}

	err := CreateActionStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected error for non-existent app")
	}
	if !strings.Contains(err.Error(), "app does not exist") {
		t.Errorf("Expected 'app does not exist' error, got: %v", err)
	}
}

func TestCreateActionStructure_ActionExists(t *testing.T) {
	// Mock SCL check
	origCheck := checkSCLEntityMatchType
	defer func() { checkSCLEntityMatchType = origCheck }()
	checkSCLEntityMatchType = func(filePath, entityName, entityType, blockKey string) (bool, error) {
		return true, nil // Simulate exists
	}

	mockFS := &mockWriteTrackingFS{
		statFn: func(name string) bool {
			// Both app and action exist
			return strings.Contains(name, "apps/com.test")
		},
		files: map[string][]byte{
			"/root/apps/com.test/records/10_actions.scl": []byte("set dev_simple_system.logic, existing {"),
		},
	}

	cfg := ActionConfig{
		AppID:      "com.test",
		ActionName: "existing",
	}

	err := CreateActionStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected error for existing action")
	}
	if !strings.Contains(err.Error(), "action already exists") {
		t.Errorf("Expected 'action already exists' error, got: %v", err)
	}
}

func TestCreateActionStructure_DuplicateSCL(t *testing.T) {
	// Mock SCL check
	origCheck := checkSCLEntityMatchType
	defer func() { checkSCLEntityMatchType = origCheck }()
	checkSCLEntityMatchType = func(path, block, typ, name string) (bool, error) {
		if name == "my_action" || name == "my-action" {
			return true, nil
		}
		return false, nil
	}

	// Action directory doesn't exist, but it's present in SCL
	mockFS := &mockWriteTrackingFS{
		statFn: func(name string) bool {
			// App exists, SCL exists, action dir doesn't
			if strings.Contains(name, "10_actions.scl") {
				return true
			}
			return strings.Contains(name, "apps/com.test") && !strings.Contains(name, "actions/my-action")
		},
		files: map[string][]byte{
			"/root/apps/com.test/records/10_actions.scl": []byte("set dev_simple_system.logic, my_action {"),
		},
	}

	cfg := ActionConfig{
		AppID:      "com.test",
		ActionName: "my-action",
	}

	err := CreateActionStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected error for duplicate action in SCL")
	}
	if !strings.Contains(err.Error(), "action already exists") {
		t.Errorf("Expected 'action already exists' error, got: %v", err)
	}
}

func TestCreateActionStructure_MkdirError(t *testing.T) {
	mockFS := &mockWriteTrackingFS{
		statFn: func(name string) bool {
			return name == "/root/apps/com.test"
		},
		mkdirErr: errors.New("permission denied"),
	}

	cfg := ActionConfig{
		AppID:      "com.test",
		ActionName: "my-action",
	}

	err := CreateActionStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected mkdir error")
	}
	if !strings.Contains(err.Error(), "failed to create action directory") {
		t.Errorf("Expected 'failed to create action directory' error, got: %v", err)
	}
}

func TestCreateActionStructure_TemplateReadError(t *testing.T) {
	mockFS := &mockWriteTrackingFS{
		statFn: func(name string) bool {
			return strings.Contains(name, "apps/com.test") && !strings.Contains(name, "actions/my-action")
		},
		written: make(map[string][]byte),
	}

	mockTpl := &fsx.MockTemplateFS{
		ReadErrors: map[string]error{
			"templates/action/package.json": errors.New("read failed"),
		},
	}

	cfg := ActionConfig{
		AppID:        "com.test",
		ActionName:   "my-action",
		Scope:        "test",
		ExecutionEnv: "server",
	}

	err := CreateActionStructure(mockFS, mockTpl, "/root", cfg)
	if err == nil {
		t.Error("Expected template read error")
	}
}

func TestAppendActionRecord_NewFile(t *testing.T) {
	written := make(map[string][]byte)

	mockFS := &mockWriteTrackingFS{
		written: written,
		statFn:  func(name string) bool { return false }, // File doesn't exist
	}

	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"templates/action/10_actions.scl": []byte("set logic, {{.ActionName}} {}"),
		},
	}

	data := map[string]string{"ActionName": "test"}
	err := appendActionRecord(mockFS, mockTpl, "/path/10_actions.scl", data)
	if err != nil {
		t.Fatalf("appendActionRecord failed: %v", err)
	}

	content := written["/path/10_actions.scl"]
	if !strings.Contains(string(content), "test") {
		t.Errorf("Expected action name in output, got: %s", string(content))
	}
}

func TestAppendActionRecord_AppendToExisting(t *testing.T) {
	written := make(map[string][]byte)

	mockFS := &mockWriteTrackingFS{
		written: written,
		statFn:  func(name string) bool { return true }, // File exists
		files: map[string][]byte{
			"/path/10_actions.scl": []byte("# existing content"),
		},
	}

	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"templates/action/10_actions.scl": []byte("set logic, {{.ActionName}} {}"),
		},
	}

	data := map[string]string{"ActionName": "new-action"}
	err := appendActionRecord(mockFS, mockTpl, "/path/10_actions.scl", data)
	if err != nil {
		t.Fatalf("appendActionRecord failed: %v", err)
	}

	content := written["/path/10_actions.scl"]
	if !strings.Contains(string(content), "# existing content") {
		t.Errorf("Expected existing content preserved, got: %s", string(content))
	}
	if !strings.Contains(string(content), "new-action") {
		t.Errorf("Expected new action added, got: %s", string(content))
	}
}

type mockWriteTrackingFS struct {
	written  map[string][]byte
	files    map[string][]byte
	statFn   func(string) bool
	mkdirErr error
	mkdirFn  func(string) error // More granular mkdir control
	writeErr error
	readErr  error
}

func (m *mockWriteTrackingFS) Stat(name string) (fs.FileInfo, error) {
	if m.statFn != nil && m.statFn(name) {
		return &mockFileInfoSimple{}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockWriteTrackingFS) MkdirAll(path string, perm os.FileMode) error {
	if m.mkdirFn != nil {
		return m.mkdirFn(path)
	}
	if m.mkdirErr != nil {
		return m.mkdirErr
	}
	return nil
}

func (m *mockWriteTrackingFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	if m.written != nil {
		m.written[name] = data
	}
	return nil
}

func (m *mockWriteTrackingFS) ReadFile(name string) ([]byte, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	if m.files != nil {
		if content, ok := m.files[name]; ok {
			return content, nil
		}
	}
	return nil, errors.New("file not found")
}

func (m *mockWriteTrackingFS) ReadDir(name string) ([]os.DirEntry, error) {
	return []os.DirEntry{}, nil
}

func (m *mockWriteTrackingFS) Remove(name string) error {
	delete(m.files, name)
	delete(m.written, name)

	return nil
}

type mockFileInfoSimple struct{}

func (m *mockFileInfoSimple) Name() string       { return "mock" }
func (m *mockFileInfoSimple) Size() int64        { return 0 }
func (m *mockFileInfoSimple) Mode() os.FileMode  { return 0755 }
func (m *mockFileInfoSimple) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfoSimple) IsDir() bool        { return true }
func (m *mockFileInfoSimple) Sys() any           { return nil }

// Trigger Tests

func TestCreateTriggerStructure_Errors(t *testing.T) {
	// Mock SCL check
	origCheck := checkSCLEntityMatchType
	defer func() { checkSCLEntityMatchType = origCheck }()
	checkSCLEntityMatchType = func(path, block, typ, name string) (bool, error) {
		if name == "daily_sync" {
			return true, nil
		}
		return false, nil
	}

	tests := []struct {
		name    string
		mockFS  *mockWriteTrackingFS
		mockTpl *fsx.MockTemplateFS
		cfg     TriggerConfig
		wantErr string
	}{
		{
			name:    "app not exists",
			mockFS:  &mockWriteTrackingFS{statFn: func(s string) bool { return false }},
			cfg:     TriggerConfig{AppID: "com.test"},
			wantErr: "app does not exist",
		},
		{
			name: "records mkdir failed",
			mockFS: &mockWriteTrackingFS{
				statFn:   func(s string) bool { return strings.Contains(s, "apps/com.test") },
				mkdirErr: errors.New("mkdir failed"),
			},
			cfg:     TriggerConfig{AppID: "com.test", TriggerType: "timed"},
			wantErr: "failed to create records directory",
		},
		{
			name:    "unknown trigger type",
			mockFS:  &mockWriteTrackingFS{statFn: func(s string) bool { return s == "/root/apps/com.test" }},
			cfg:     TriggerConfig{AppID: "com.test", TriggerType: "unknown"},
			wantErr: "unknown trigger type",
		},
		{
			name: "append trigger record failed",
			mockFS: &mockWriteTrackingFS{
				statFn:   func(s string) bool { return s == "/root/apps/com.test" },
				writeErr: errors.New("write failed"),
			},
			mockTpl: &fsx.MockTemplateFS{
				Files: map[string][]byte{"templates/trigger/20_triggers_timed.scl": []byte("template")},
			},
			cfg:     TriggerConfig{AppID: "com.test", TriggerType: "timed"},
			wantErr: "failed to append trigger record",
		},
		{
			name: "duplicate trigger",
			mockFS: &mockWriteTrackingFS{
				statFn: func(s string) bool { return true },
				files: map[string][]byte{
					"/root/apps/com.test/records/20_triggers.scl": []byte("set dev_simple_system.trigger, daily_sync {"),
				},
			},
			cfg: TriggerConfig{
				AppID:          "com.test",
				TriggerType:    "timed",
				TriggerNameScl: "daily_sync",
			},
			wantErr: "trigger already exists: daily_sync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockTpl == nil {
				tt.mockTpl = &fsx.MockTemplateFS{}
			}
			err := CreateTriggerStructure(tt.mockFS, tt.mockTpl, "/root", tt.cfg)
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %v, want substring %v", err, tt.wantErr)
			}
		})
	}
}

func TestAppendTriggerRecord_Errors(t *testing.T) {
	// Template read error
	mockTpl := &fsx.MockTemplateFS{ReadFileErr: errors.New("read failed")}
	mockFS := &mockWriteTrackingFS{}
	err := appendTriggerRecord(mockFS, mockTpl, "path", "dst", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to read template") {
		t.Errorf("Expected read template error, got: %v", err)
	}

	// Parse error
	mockTpl = &fsx.MockTemplateFS{Files: map[string][]byte{"path": []byte("{{ .Bad")}}
	err = appendTriggerRecord(mockFS, mockTpl, "path", "dst", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse template") {
		t.Errorf("Expected parse template error, got: %v", err)
	}

	// Read existing file error
	mockTpl = &fsx.MockTemplateFS{Files: map[string][]byte{"path": []byte("content")}}
	mockFS = &mockWriteTrackingFS{
		statFn: func(s string) bool { return true },
		files:  nil, // This will cause ReadFile to return error in mockWriteTrackingFS
	}
	err = appendTriggerRecord(mockFS, mockTpl, "path", "dst", nil)
	// mockWriteTrackingFS.ReadFile returns "file not found" if files map is nil or key missing
	if err == nil || !strings.Contains(err.Error(), "failed to read existing") {
		t.Errorf("Expected read existing error, got: %v", err)
	}
}

// THE EDITOR IS TOLD THE SAME VOCABULARY THE BUILD ENFORCES.
//
// `@tool` is what makes an action reachable by an agent, and TSDoc reports a tag
// it has not been told about as an unknown one. An author writing the single
// line that exposes their action would be underlined for it — which teaches the
// wrong lesson at exactly the wrong moment, and the lesson sticks because the
// build says nothing until the tag is already missing.
//
// So the names are read from the package that enforces them rather than restated
// here: a tag added to the vocabulary and not to this file fails, instead of
// shipping as a squiggle under every author who uses it.
func TestScaffoldedSpaceDeclaresTheExposureVocabulary(t *testing.T) {
	raw, err := TemplatesFS.ReadFile("templates/tsdoc.json")
	if err != nil {
		t.Fatalf("a scaffolded space carries no tsdoc.json: %v", err)
	}

	var config struct {
		Schema         string `json:"$schema"`
		TagDefinitions []struct {
			TagName       string `json:"tagName"`
			SyntaxKind    string `json:"syntaxKind"`
			AllowMultiple bool   `json:"allowMultiple"`
		} `json:"tagDefinitions"`
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("tsdoc.json is not readable as the format TSDoc defines: %v", err)
	}

	if config.Schema != "https://developer.microsoft.com/json-schemas/tsdoc/v0/tsdoc.schema.json" {
		t.Fatalf("tsdoc.json names no TSDoc schema, got %q", config.Schema)
	}

	type declaration struct {
		kind     string
		multiple bool
	}

	declared := map[string]declaration{}
	for _, definition := range config.TagDefinitions {
		declared[definition.TagName] = declaration{kind: definition.SyntaxKind, multiple: definition.AllowMultiple}
	}

	if len(declared) != len(config.TagDefinitions) {
		t.Fatalf("tsdoc.json declares a tag twice: %#v", config.TagDefinitions)
	}

	for _, name := range build.ActionTagNames() {
		// `@tool` carries no value, so it is a modifier tag; every other name in
		// the vocabulary carries one, so they are block tags. Declaring a
		// modifier as a block tag makes TSDoc read the next line as its content.
		want := "block"
		if name == "tool" {
			want = "modifier"
		}

		// `@usewhen` is the one name an author may write more than once — a tool
		// is reached for in more than one situation, and the vocabulary carries
		// up to ten of them. A block tag TSDoc has not been told is repeatable is
		// reported as a duplicate from the second line on, which underlines a
		// statement the build accepts.
		wantMultiple := name == "usewhen"

		definition, known := declared["@"+name]
		if !known {
			t.Fatalf("the build claims @%s and tsdoc.json does not declare it", name)
		}

		if definition.kind != want {
			t.Fatalf("@%s is declared as a %s tag and is a %s tag", name, definition.kind, want)
		}

		if definition.multiple != wantMultiple {
			t.Fatalf("@%s is declared repeatable=%t and is repeatable=%t", name, definition.multiple, wantMultiple)
		}

		delete(declared, "@"+name)
	}

	if len(declared) != 0 {
		t.Fatalf("tsdoc.json declares tags the build does not claim: %#v", declared)
	}
}

// THE VOCABULARY IS REACHABLE FROM WHERE AN AUTHOR WRITES IT.
//
// A TSDoc reader walks up from the source file and stops at the first folder
// holding a package.json or a tsconfig.json, then looks for tsdoc.json THERE and
// nowhere further up. Every scaffolded action holds both of those files, so the
// walk always stops inside the action — and a vocabulary kept only at the space
// root is never found, leaving `@tool` an undefined tag in every action of a
// space that carries a perfectly good declaration one directory above.
//
// So the action's own file inherits from the space's, and the path between them
// is a property of the layout this package creates. It is computed here from the
// two paths the scaffold actually writes, so moving either one fails rather than
// silently pointing at nothing.
func TestAnActionInheritsTheVocabularyFromItsSpace(t *testing.T) {
	raw, err := TemplatesFS.ReadFile("templates/action/tsdoc.json")
	if err != nil {
		t.Fatalf("a scaffolded action carries no tsdoc.json: %v", err)
	}

	var config struct {
		Extends        []string `json:"extends"`
		TagDefinitions []any    `json:"tagDefinitions"`
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("an action's tsdoc.json is not readable as the format TSDoc defines: %v", err)
	}

	if len(config.TagDefinitions) != 0 {
		t.Fatal("an action restates the vocabulary instead of inheriting it, so the two can disagree")
	}

	if len(config.Extends) != 1 {
		t.Fatalf("expected an action to inherit from exactly one file, got %#v", config.Extends)
	}

	// TSDoc reads a base path that does not start with `./` as an NPM package
	// name, so a relative one that omits it resolves to nothing installed.
	if !strings.HasPrefix(config.Extends[0], "./") {
		t.Fatalf("a relative base path must begin with \"./\" or it is read as a package name, got %q", config.Extends[0])
	}

	// The two paths the scaffold writes, from a space root, as it writes them.
	spaceConfig := filepath.Join("/space", "tsdoc.json")
	actionConfig := filepath.Join("/space", "apps", "com.acme.app", "actions", "read-things", "tsdoc.json")

	inherited := filepath.Join(filepath.Dir(actionConfig), config.Extends[0])
	if filepath.Clean(inherited) != spaceConfig {
		t.Fatalf("an action inherits from %q, and its space declares the vocabulary at %q",
			filepath.Clean(inherited), spaceConfig)
	}
}

// THE SCAFFOLDED RUST ACTION DECLARES ITS PAYLOAD UNDER THE NAME THE GENERATOR
// LOOKS FOR.
//
// A Rust action's input schema is read off a struct found BY NAME. A struct
// called anything else is not found, and nothing is reported when it is not: an
// action that genuinely takes no input is ordinary, so the generator answers
// with the no-input schema and exits zero.
//
// That makes this the quiet failure. The template shipped a payload called
// `Input` with a required field on it, and every action scaffolded from it
// advertised an empty schema beside a handler that refuses an empty call — a
// model reading the contract is told there is nothing to send, sends nothing,
// and is refused for a reason the contract does not contain. The build said
// Done and the action.json was well-formed.
//
// The name is read from the generator rather than written here, so the day it
// changes upstream this fails instead of quietly scaffolding actions that
// advertise no input.
func TestTheScaffoldedRustActionDeclaresThePayloadTheGeneratorLooksFor(t *testing.T) {
	source, err := TemplatesFS.ReadFile("templates/action-rust/main.rs")
	if err != nil {
		t.Fatalf("a scaffolded Rust action carries no main.rs: %v", err)
	}

	wanted := build.PayloadStructName()
	if wanted == "" {
		t.Fatal("the generator's payload struct name could not be read, so this check is not checking anything")
	}

	declaration := "struct " + wanted + " {"
	if !strings.Contains(string(source), declaration) {
		t.Fatalf("a scaffolded Rust action declares no %q, so its schema is read off nothing and it advertises no input", declaration)
	}

	// The handler has to deserialize the same type, or the schema describes one
	// struct while the action reads another.
	if !strings.Contains(string(source), "Request<"+wanted+">") {
		t.Fatalf("a scaffolded Rust action declares %q but its handler does not take Request<%s>", declaration, wanted)
	}

	// A payload with no members would satisfy the two checks above and still
	// advertise nothing, which is the failure they exist to catch.
	if !strings.Contains(string(source), "name: String,") {
		t.Fatal("a scaffolded Rust action's payload declares no member, so it advertises no input whatever it is called")
	}
}

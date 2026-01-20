package scaffold

import (
	"errors"
	"io/fs"
	"os"
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
	origCheck := checkSCLEntityExists
	defer func() { checkSCLEntityExists = origCheck }()
	checkSCLEntityExists = func(path, block, typ, name string) (bool, error) {
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
	origCheck := checkSCLEntityExists
	defer func() { checkSCLEntityExists = origCheck }()
	checkSCLEntityExists = func(path, block, typ, name string) (bool, error) {
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

// mockWriteTrackingFS is a custom mock that tracks writes and allows custom stat behavior
type mockWriteTrackingFS struct {
	written  map[string][]byte
	files    map[string][]byte
	statFn   func(string) bool
	mkdirErr error
	writeErr error
}

func (m *mockWriteTrackingFS) Stat(name string) (fs.FileInfo, error) {
	if m.statFn != nil && m.statFn(name) {
		return &mockFileInfoSimple{}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockWriteTrackingFS) MkdirAll(path string, perm os.FileMode) error {
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
	origCheck := checkSCLEntityExists
	defer func() { checkSCLEntityExists = origCheck }()
	checkSCLEntityExists = func(path, block, typ, name string) (bool, error) {
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

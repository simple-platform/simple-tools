package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAppCmd_Success(t *testing.T) {
	// Setup: Create a temp "monorepo" root
	tmpDir := t.TempDir()

	// Create "apps" dir to satisfy check
	_ = os.Mkdir(filepath.Join(tmpDir, "apps"), 0755)

	// Change CWD to tmpDir for the test
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	// Invoke: simple new app com.example.test "Test App"
	args := []string{"new", "app", "com.example.test", "Test App"}
	out, _, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("New App failed: %v", err)
	}

	if !strings.Contains(out, "Created app Test App (com.example.test)") {
		t.Errorf("Unexpected output: %s", out)
	}

	// Verify Files
	appScl := filepath.Join(tmpDir, "apps", "com.example.test", "app.scl")
	if _, err := os.Stat(appScl); os.IsNotExist(err) {
		t.Error("app.scl not created")
	} else {
		content, _ := os.ReadFile(appScl)
		if !strings.Contains(string(content), "id com.example.test") ||
			!strings.Contains(string(content), `display_name "Test App"`) {
			t.Error("app.scl content incorrect")
		}
	}

	tablesScl := filepath.Join(tmpDir, "apps", "com.example.test", "tables.scl")
	if _, err := os.Stat(tablesScl); os.IsNotExist(err) {
		t.Error("tables.scl not created")
	}
}

func TestNewAppCmd_MissingAppsDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Don't create apps dir

	// Change CWD
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "app", "com.example.fail", "Fail App"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error when apps dir is missing")
	}
	if !strings.Contains(err.Error(), "apps directory not found") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewAppCmd_AppExists(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, "apps", "com.example.exists"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "app", "com.example.exists", "Exists App"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error when app already exists")
	}
	if !strings.Contains(err.Error(), "app already exists") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewAppCmd_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "apps"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "app", "com.example.json", "JSON App", "--json"}
	out, _, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("New App JSON failed: %v", err)
	}

	if !strings.Contains(out, `"status": "success"`) {
		t.Errorf("Expected JSON success status, got: %s", out)
	}
	if !strings.Contains(out, `"app_id": "com.example.json"`) {
		t.Errorf("Expected app_id in output, got: %s", out)
	}
}

// Action command tests

func TestNewActionCmd_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create app structure
	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "actions"), 0755)
	_ = os.MkdirAll(filepath.Join(appDir, "records"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "action", "com.example.test", "send-email", "Send Email", "--scope", "mycompany", "--desc", "Sends emails"}
	out, _, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("New Action failed: %v", err)
	}

	if !strings.Contains(out, "Created action Send Email (send-email)") {
		t.Errorf("Unexpected output: %s", out)
	}

	// Verify files created
	actionDir := filepath.Join(appDir, "actions", "send-email")
	filesToCheck := []string{
		"package.json",
		"index.ts",
		"tsconfig.json",
		"vitest.config.ts",
		"tests/helpers.ts",
		"tests/index.test.ts",
	}

	for _, file := range filesToCheck {
		path := filepath.Join(actionDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("%s not created", file)
		}
	}

	// Verify package.json content
	pkgJson, _ := os.ReadFile(filepath.Join(actionDir, "package.json"))
	if !strings.Contains(string(pkgJson), "@mycompany/action-send-email") {
		t.Error("package.json doesn't contain correct package name")
	}

	// Verify 10_actions.scl created
	actionsScl := filepath.Join(appDir, "records", "10_actions.scl")
	if _, err := os.Stat(actionsScl); os.IsNotExist(err) {
		t.Error("10_actions.scl not created")
	} else {
		content, _ := os.ReadFile(actionsScl)
		contentStr := string(content)
		// Check that SCL identifier uses underscores (send_email)
		if !strings.Contains(contentStr, "set dev_simple_system.logic, send_email {") {
			t.Error("10_actions.scl should use underscores in SCL identifier")
		}
		// Check that name field keeps hyphens (send-email)
		if !strings.Contains(contentStr, `name "send-email"`) {
			t.Error("10_actions.scl should keep hyphens in name field")
		}
		if !strings.Contains(contentStr, "execution_environment server") {
			t.Error("10_actions.scl content incorrect")
		}
	}
}

func TestNewActionCmd_MissingApp(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "apps"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "action", "com.example.nonexistent", "test-action", "Test Action", "--scope", "test"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error when app doesn't exist")
	}
	if !strings.Contains(err.Error(), "app does not exist") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewActionCmd_ActionExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create app and action structure
	actionDir := filepath.Join(tmpDir, "apps", "com.example.test", "actions", "existing-action")
	_ = os.MkdirAll(actionDir, 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "action", "com.example.test", "existing-action", "Existing Action", "--scope", "test"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error when action already exists")
	}
	if !strings.Contains(err.Error(), "action already exists") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewActionCmd_InvalidLang(t *testing.T) {
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "actions"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "action", "com.example.test", "test-action", "Test Action", "--lang", "go", "--scope", "test"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewActionCmd_InvalidEnv(t *testing.T) {
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "actions"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "action", "com.example.test", "test-action", "Test Action", "--lang", "ts", "--env", "invalid", "--scope", "test"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error for invalid execution environment")
	}
	if !strings.Contains(err.Error(), "invalid execution environment") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewActionCmd_EmptyScope(t *testing.T) {
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "actions"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	// Explicitly pass empty scope to test our validation
	args := []string{"new", "action", "com.example.test", "test-action", "Test Action", "--lang", "ts", "--env", "server", "--scope", ""}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error when scope is empty")
	}
	if err != nil && !strings.Contains(err.Error(), "scope") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestNewActionCmd_JSON(t *testing.T) {
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "actions"), 0755)
	_ = os.MkdirAll(filepath.Join(appDir, "records"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	args := []string{"new", "action", "com.example.test", "json-action", "JSON Action", "--lang", "ts", "--env", "server", "--scope", "test", "--json"}
	out, _, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("New Action JSON failed: %v", err)
	}

	if !strings.Contains(out, `"status": "success"`) {
		t.Errorf("Expected JSON success status, got: %s", out)
	}
	if !strings.Contains(out, `"action_name": "json-action"`) {
		t.Errorf("Expected action_name in output, got: %s", out)
	}
}

func TestNewActionCmd_InvalidActionName(t *testing.T) {
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "apps", "com.example.test")
	_ = os.MkdirAll(filepath.Join(appDir, "actions"), 0755)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	testCases := []struct {
		name        string
		actionName  string
		shouldError bool
		errorMsg    string
	}{
		{"uppercase", "Send-Email", true, "invalid action name"},
		{"starts with number", "1action", true, "invalid action name"},
		{"contains underscore", "send_email", true, "invalid action name"},
		{"contains special char", "send@email", true, "invalid action name"},
		{"valid lowercase", "send-email", false, ""},
		{"valid simple", "myaction", false, ""},
		{"valid with numbers", "action123", false, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up any previous action
			_ = os.RemoveAll(filepath.Join(appDir, "actions", tc.actionName))

			args := []string{"new", "action", "com.example.test", tc.actionName, "Test Action", "--scope", "test"}
			_, _, err := invokeCmd(args...)

			if tc.shouldError {
				if err == nil {
					t.Errorf("Expected error for action name %q", tc.actionName)
				} else if !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error containing %q, got: %v", tc.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for action name %q: %v", tc.actionName, err)
				}
			}
		})
	}
}

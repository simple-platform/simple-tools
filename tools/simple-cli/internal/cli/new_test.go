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
	os.Mkdir(filepath.Join(tmpDir, "apps"), 0755)

	// Change CWD to tmpDir for the test
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

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
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

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
	os.MkdirAll(filepath.Join(tmpDir, "apps", "com.example.exists"), 0755)

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

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
	os.Mkdir(filepath.Join(tmpDir, "apps"), 0755)

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

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

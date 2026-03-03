package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSpaceCmd_Integration(t *testing.T) {
	// Setup a temporary monorepo structure
	tmpDir := t.TempDir()

	err := os.MkdirAll(filepath.Join(tmpDir, "apps", "com.test.app"), 0755)
	if err != nil {
		t.Fatalf("Failed to setup test dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "apps", "com.test.app", "app.scl"), []byte("id com.test.app"), 0644)
	if err != nil {
		t.Fatalf("Failed to write app.scl: %v", err)
	}

	// Change working directory to the temp monorepo
	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	// Case 1: Success - Create new space
	args := []string{"new", "space", "com.test.app", "my-dashboard", "My Dashboard", "--desc", "Test"}
	out, _, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("new space failed: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Created space My Dashboard") {
		t.Errorf("Expected success message, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "apps/com.test.app/spaces/my-dashboard/package.json")); os.IsNotExist(err) {
		t.Error("Space package.json not created")
	}

	// Case 2: JSON Output validation
	args = []string{"new", "space", "com.test.app", "analytics-view", "Analytics", "--json"}
	out, _, err = invokeCmd(args...)
	if err != nil {
		t.Fatalf("new space JSON failed: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, out)
	}
	if result["status"] != "success" {
		t.Errorf("Expected status=success, got %v", result["status"])
	}
	if result["name"] != "analytics-view" {
		t.Errorf("Expected name=analytics-view, got %v", result["name"])
	}
}

func TestNewSpaceCmd_MissingAppsDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Change working directory to empty dir (no apps/)
	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	args := []string{"new", "space", "com.test.app", "my-dashboard", "My Dashboard"}
	_, _, err := invokeCmd(args...)
	if err == nil || !strings.Contains(err.Error(), "apps directory not found") {
		t.Errorf("Expected apps dir error, got: %v", err)
	}
}

func TestNewSpaceCmd_InvalidSpaceName(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, "apps"), 0755)

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	args := []string{"new", "space", "com.test", "Invalid_Space", "Name"}
	_, _, err := invokeCmd(args...)
	if err == nil || !strings.Contains(err.Error(), "invalid space name") {
		t.Errorf("Expected invalid space name error, got: %v", err)
	}
}

func TestNewSpaceCmd_SpaceExists(t *testing.T) {
	tmpDir := t.TempDir()
	spacePath := filepath.Join(tmpDir, "apps", "com.test.app", "spaces", "existing")
	_ = os.MkdirAll(spacePath, 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "apps", "com.test.app", "app.scl"), []byte("id com.test.app"), 0644)

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	args := []string{"new", "space", "com.test.app", "existing", "Exists"}
	_, _, err := invokeCmd(args...)
	if err == nil || !strings.Contains(err.Error(), "space directory already exists") {
		t.Errorf("Expected already exists error, got: %v", err)
	}
}

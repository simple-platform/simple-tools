package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// setupTestRepo creates a temporary directory with an "apps" subdirectory
// to simulate a valid workspace root.
func setupTestRepo(t *testing.T) string {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "apps"), 0755); err != nil {
		t.Fatalf("Failed to create apps directory: %v", err)
	}
	return tmpDir
}

func TestNewTriggerTimedCmd_Success(t *testing.T) {
	tmpDir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	appID := "com.example.test"
	triggerName := "daily-sync"

	// Create app and records dir
	_ = os.MkdirAll(filepath.Join(tmpDir, "apps", appID, "records"), 0755)

	args := []string{
		"new", "trigger:timed", appID, triggerName, "Daily Sync",
		"--action", "sync-data",
		"--frequency", "daily",
		"--time", "09:00:00",
		"--desc", "Runs daily at 9am",
	}

	out, errOut, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("Command failed: %v\nStderr: %s", err, errOut)
	}

	if !strings.Contains(out, "Created timed trigger Daily Sync") {
		t.Errorf("Expected success message, got: %s", out)
	}

	// Verify file content
	recordsFile := filepath.Join(tmpDir, "apps", appID, "records", "20_triggers.scl")
	content, err := os.ReadFile(recordsFile)
	if err != nil {
		t.Fatalf("Failed to read records file: %v", err)
	}

	sclContent := string(content)
	if !strings.Contains(sclContent, "daily_sync_schedule") {
		t.Errorf("Expected schedule definition in SCL")
	}
	if !strings.Contains(sclContent, `"frequency": "daily"`) {
		t.Errorf("Expected frequency in schedule")
	}
	if !strings.Contains(sclContent, `"time": "09:00:00"`) {
		t.Errorf("Expected time in schedule")
	}
}

func TestNewTriggerDbCmd_Success(t *testing.T) {
	tmpDir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	appID := "com.example.test"
	_ = os.MkdirAll(filepath.Join(tmpDir, "apps", appID, "records"), 0755)

	args := []string{
		"new", "trigger:db", appID, "on-update", "On Update",
		"--action", "process",
		"--table", "orders",
		"--ops", "update,delete",
		"--condition", ".status == 'pending'",
	}

	out, errOut, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("Command failed: %v\nStderr: %s", err, errOut)
	}

	if !strings.Contains(out, "Created db trigger On Update") {
		t.Errorf("Expected success message, got: %s", out)
	}

	recordsFile := filepath.Join(tmpDir, "apps", appID, "records", "20_triggers.scl")
	content, _ := os.ReadFile(recordsFile)
	sclContent := string(content)

	if !strings.Contains(sclContent, "db_event, on_update") {
		t.Errorf("Expected db_event definition")
	}
	// Check JSON array formatting for ops
	opsRegex := regexp.MustCompile(`"insert"`)
	if opsRegex.MatchString(sclContent) {
		t.Errorf("Should not contain insert")
	}
	if !strings.Contains(sclContent, `"update", "delete"`) && !strings.Contains(sclContent, `"update","delete"`) {
		// Just check for presence of both words to be safe against whitespace
		if !strings.Contains(sclContent, "update") || !strings.Contains(sclContent, "delete") {
			t.Errorf("Expected update and delete ops")
		}
	}
}

func TestNewTriggerWebhookCmd_Success(t *testing.T) {
	tmpDir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	appID := "com.example.test"
	_ = os.MkdirAll(filepath.Join(tmpDir, "apps", appID, "records"), 0755)

	args := []string{
		"new", "trigger:webhook", appID, "payment-hook", "Payment Hook",
		"--action", "handle-payment",
		"--method", "post",
		"--public",
	}

	out, errOut, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("Command failed: %v\nStderr: %s", err, errOut)
	}

	if !strings.Contains(out, "Created webhook trigger Payment Hook") {
		t.Errorf("Expected success message, got: %s", out)
	}

	recordsFile := filepath.Join(tmpDir, "apps", appID, "records", "20_triggers.scl")
	content, _ := os.ReadFile(recordsFile)
	sclContent := string(content)

	if !strings.Contains(sclContent, "webhook, payment_hook") {
		t.Errorf("Expected webhook definition")
	}
	if !strings.Contains(sclContent, "is_public true") {
		t.Errorf("Expected is_public true")
	}
	if !strings.Contains(sclContent, "method post") {
		t.Errorf("Expected method post")
	}
}

func TestNewTriggerCmd_InvalidFrequency(t *testing.T) {
	tmpDir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	appID := "com.example.test"
	_ = os.MkdirAll(filepath.Join(tmpDir, "apps", appID, "records"), 0755)

	args := []string{
		"new", "trigger:timed", appID, "bad-freq", "Bad Freq",
		"--action", "sync",
		"--frequency", "hourlyy", // Typo
	}

	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Fatal("Expected error for invalid frequency")
	}
	if !strings.Contains(err.Error(), "invalid frequency") {
		t.Errorf("Expected invalid frequency error, got: %v", err)
	}
}

func TestNewTriggerCmd_MissingAction(t *testing.T) {
	// Flags are required by Cobra validation, so we expect error before run
	tmpDir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	appID := "com.example.test"
	_ = os.MkdirAll(filepath.Join(tmpDir, "apps", appID, "records"), 0755)

	args := []string{
		"new", "trigger:timed", appID, "no-action", "No Action",
		"--frequency", "daily",
		// Missing --action
	}

	// Reset flags to ensure persistence doesn't hide the error
	_ = newTriggerTimedCmd.Flags().Set("action", "")
	newTriggerTimedCmd.Flags().Lookup("action").Changed = false

	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Fatal("Expected error for missing action flag")
	}
}

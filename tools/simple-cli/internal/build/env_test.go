package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectActionLanguage(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  ActionLanguage
	}{
		{"typescript under src", []string{"src/index.ts"}, LanguageTypeScript},
		{"typescript at the root", []string{"index.ts"}, LanguageTypeScript},
		{"rust", []string{"Cargo.toml", "src/main.rs"}, LanguageRust},
		{"go", []string{"main.go"}, LanguageGo},
		// Both spellings of TypeScript are one answer, not an ambiguity.
		{"both typescript spellings", []string{"index.ts", "src/index.ts"}, LanguageTypeScript},
		// A manifest names dependencies; the source names the language. A
		// TypeScript action that happens to carry a Cargo.toml is still
		// TypeScript, and must not be handed to cargo.
		{"typescript beside a stray manifest", []string{"Cargo.toml", "src/index.ts"}, LanguageTypeScript},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeActionSources(t, dir, tt.files...)

			got, err := DetectActionLanguage(dir)
			if err != nil {
				t.Fatalf("DetectActionLanguage() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("DetectActionLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDetectActionLanguage_None pins that an action with no source is told what
// was looked for. "Unsupported language" sends a developer hunting through a
// file that does not exist.
func TestDetectActionLanguage_None(t *testing.T) {
	dir := t.TempDir()
	writeActionSources(t, dir, "README.md")

	_, err := DetectActionLanguage(dir)
	if err == nil {
		t.Fatal("Expected an error for an action with no source, got nil")
	}
	for _, want := range []string{"src/index.ts", "src/main.rs", "main.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error should name %s as one of the sources looked for, got: %v", want, err)
		}
	}
}

// TestDetectActionLanguage_Ambiguous pins that two sources are refused rather
// than resolved by precedence. Picking one would leave the other compiled by
// nothing, with nothing said about it.
func TestDetectActionLanguage_Ambiguous(t *testing.T) {
	dir := t.TempDir()
	writeActionSources(t, dir, "src/index.ts", "src/main.rs")

	_, err := DetectActionLanguage(dir)
	if err == nil {
		t.Fatal("Expected an error for an action with two sources, got nil")
	}
	if !strings.Contains(err.Error(), "src/index.ts") || !strings.Contains(err.Error(), "src/main.rs") {
		t.Errorf("Error should name both sources it found, got: %v", err)
	}
}

func writeActionSources(t *testing.T, dir string, relPaths ...string) {
	t.Helper()
	for _, rel := range relPaths {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseExecutionEnvironment_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	// No 10_actions.scl

	env, err := ParseExecutionEnvironment("dummy-parser", tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if env != "server" {
		t.Errorf("got %s, want server (fallback)", env)
	}
}

func TestParseExecutionEnvironment_MultiAction(t *testing.T) {
	// Create a temporary mock scl-parser script
	tmpDir := t.TempDir()
	mockParserPath := filepath.Join(tmpDir, "mock-scl-parser")

	// JSON output simulating two actions: one server, one client.
	// We now test matching by the inner "name" property.
	// The order matters: put "server" first so the buggy implementation picks it up.
	jsonOutput := `[
		{
			"type": "block",
			"key": "set",
			"name": ["sys.logic", "action_server_id"],
			"children": [
				{ "type": "assignment", "key": "name", "value": "action_server" },
				{ "type": "assignment", "key": "execution_environment", "value": "server" }
			]
		},
		{
			"type": "block",
			"key": "set",
			"name": ["sys.logic", "action_client_id"],
			"children": [
				{ "type": "assignment", "key": "name", "value": "action_client" },
				{ "type": "assignment", "key": "execution_environment", "value": "client" }
			]
		}
	]`

	// Use a simple shell script to mock the parser
	scriptContent := fmt.Sprintf("#!/bin/sh\necho '%s'", jsonOutput)
	if err := os.WriteFile(mockParserPath, []byte(scriptContent), 0755); err != nil {
		t.Fatal(err)
	}

	// Create dummy SCL file path logic
	// actionDir is .../apps/my-app/actions/action_client
	// so appDir is .../apps/my-app
	// sclPath is .../apps/my-app/records/10_actions.scl

	appDir := filepath.Join(tmpDir, "apps", "my-app")
	recordsDir := filepath.Join(appDir, "records")
	if err := os.MkdirAll(recordsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create dummy SCL file
	if err := os.WriteFile(filepath.Join(recordsDir, "10_actions.scl"), []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	// Test case 1: building action_client. Should return "client".
	actionClientDir := filepath.Join(appDir, "actions", "action_client")

	env, err := ParseExecutionEnvironment(mockParserPath, actionClientDir)
	if err != nil {
		t.Fatalf("ParseExecutionEnvironment failed: %v", err)
	}

	if env != "client" {
		t.Errorf("for action_client: expected 'client', got '%s'", env)
	}

	// Test case 2: building action_server. Should return "server".
	actionServerDir := filepath.Join(appDir, "actions", "action_server")
	env, err = ParseExecutionEnvironment(mockParserPath, actionServerDir)
	if err != nil {
		t.Fatalf("ParseExecutionEnvironment failed: %v", err)
	}
	if env != "server" {
		t.Errorf("for action_server: expected 'server', got '%s'", env)
	}
}

// TestParseExecutionEnvironment_Comprehensive covers:
// - Single action: server, client, both
// - Multiple actions: mixed combinations
func TestParseExecutionEnvironment_Comprehensive(t *testing.T) {
	tmpDir := t.TempDir()
	mockParserPath := filepath.Join(tmpDir, "mock-scl-parser")

	// construct JSON for multiple actions
	// Action 1: server
	// Action 2: client
	// Action 3: both
	jsonOutput := `[
		{
			"type": "block",
			"key": "set",
			"name": ["sys.logic", "action_server_id"],
			"children": [
				{ "type": "assignment", "key": "name", "value": "action_server" },
				{ "type": "assignment", "key": "execution_environment", "value": "server" }
			]
		},
		{
			"type": "block",
			"key": "set",
			"name": ["sys.logic", "action_client_id"],
			"children": [
				{ "type": "assignment", "key": "name", "value": "action_client" },
				{ "type": "assignment", "key": "execution_environment", "value": "client" }
			]
		},
		{
			"type": "block",
			"key": "set",
			"name": ["sys.logic", "action_both_id"],
			"children": [
				{ "type": "assignment", "key": "name", "value": "action_both" },
				{ "type": "assignment", "key": "execution_environment", "value": "both" }
			]
		},
		{
			"type": "block",
			"key": "set",
			"name": ["sys.logic", "action_default_id"],
			"children": [
				{ "type": "assignment", "key": "name", "value": "action_default" }
			]
		}
	]`

	scriptContent := fmt.Sprintf("#!/bin/sh\necho '%s'", jsonOutput)
	if err := os.WriteFile(mockParserPath, []byte(scriptContent), 0755); err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(tmpDir, "apps", "my-app")
	recordsDir := filepath.Join(appDir, "records")
	if err := os.MkdirAll(recordsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordsDir, "10_actions.scl"), []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		actionName string
		wantEnv    string
	}{
		{"action_server", "server"},
		{"action_client", "client"},
		{"action_both", "both"},
		{"action_default", "server"}, // Missing execution_environment defaults to server
		{"action_unknown", "server"}, // Unknown action defaults to server
	}

	for _, tt := range tests {
		t.Run(tt.actionName, func(t *testing.T) {
			actionDir := filepath.Join(appDir, "actions", tt.actionName)
			gotEnv, err := ParseExecutionEnvironment(mockParserPath, actionDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotEnv != tt.wantEnv {
				t.Errorf("got %s, want %s", gotEnv, tt.wantEnv)
			}
		})
	}
}

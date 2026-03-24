package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAuthStatus_LoadErr(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission tests are unreliable")
	}

	tmpConfig := t.TempDir()
	t.Setenv("HOME", tmpConfig)

	// Create an unreadable tokens.json to force permission error
	simpleDir := filepath.Join(tmpConfig, ".simple")
	_ = os.MkdirAll(simpleDir, 0755)
	tokenFile := filepath.Join(simpleDir, "tokens.json")
	_ = os.WriteFile(tokenFile, []byte("{}"), 0000)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	// Defer a safety close to prevent leaked FDs on t.Fatalf panic aborts.
	defer func() { _ = w.Close() }()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	err := runAuthStatus(nil, nil)

	// Explicitly close the write end here to send EOF, otherwise io.Copy below will block forever.
	// Note: this assumes runAuthStatus does not spawn background goroutines that write to stderr.
	_ = w.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Fatalf("runAuthStatus expected to gracefully handle error, got: %v", err)
	}

	if !strings.Contains(output, "Failed to read session cache") {
		t.Errorf("Expected warning about cache read failure, got output: %s", output)
	}
}

func TestRunAuthStatus_JSONLoadErr(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission tests are unreliable")
	}

	tmpConfig := t.TempDir()
	t.Setenv("HOME", tmpConfig)

	// Create an unreadable tokens.json to force permission error
	simpleDir := filepath.Join(tmpConfig, ".simple")
	_ = os.MkdirAll(simpleDir, 0755)
	tokenFile := filepath.Join(simpleDir, "tokens.json")
	_ = os.WriteFile(tokenFile, []byte("{}"), 0000)

	// Enable JSON mode
	jsonOutput = true
	defer func() { jsonOutput = false }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	// Defer a safety close to prevent leaked FDs on t.Fatalf panic aborts.
	defer func() { _ = w.Close() }()
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	err := runAuthStatus(nil, nil)

	// Explicitly close the write end here to send EOF, otherwise io.Copy below will block forever.
	// Note: this assumes runAuthStatus does not spawn background goroutines that write to stdout.
	_ = w.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.Bytes()

	// printJSON technically returns an error or nil, but we capture the buffer
	if err != nil {
		t.Fatalf("runAuthStatus json expected to handle error successfully, got: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v, raw output: %s", err, string(output))
	}

	if msg, ok := result["warning"].(string); !ok || msg == "" {
		t.Errorf("Expected JSON to include 'warning' key with non-empty string")
	}
	if arr, ok := result["keys"].([]interface{}); !ok || arr == nil {
		t.Error("Expected JSON 'keys' to be an array and not null")
	}
	if arr, ok := result["tokens"].([]interface{}); !ok || arr == nil {
		t.Error("Expected JSON 'tokens' to be an array and not null")
	}
}

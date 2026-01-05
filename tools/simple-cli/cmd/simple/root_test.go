package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// invokeCmd executes the root command with specific arguments and captures output
func invokeCmd(args ...string) (string, string, error) {
	// Reset commands for testing to avoid side effects
	jsonOutput = false

	// Create buffers for stdout/stderr
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)

	// Swap stdout/stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w // Simple redirection for test simplicity
	// Note: Proper separating needs more work, but Cobra allows SetOut/SetErr

	RootCmd.SetOut(outBuf)
	RootCmd.SetErr(errBuf)
	RootCmd.SetArgs(args)

	// Execute
	err := RootCmd.Execute()

	// Restore stdout/stderr
	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	out, _ := io.ReadAll(r)

	// RootCmd execution output might go to SetOut/SetErr or direct formatted print depending on implementation
	// In root.go: printJSON uses os.Stdout directly. execute uses os.Stderr directly.
	// So checking the pipe capture 'out' is most reliable for printJSON.

	// Combine pipe output with buffer output
	fullOutput := string(out) + outBuf.String()
	fullErr := errBuf.String()

	return fullOutput, fullErr, err
}

func TestInitCmd_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	// Test 1: Normal initialization
	args := []string{"init", tmpDir + "/proj1"}
	out, _, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if !strings.Contains(out, "Initialized Simple Platform monorepo") {
		t.Errorf("Expected success message, got: %s", out)
	}

	// Verify it actually created files
	if _, err := os.Stat(tmpDir + "/proj1/AGENTS.md"); os.IsNotExist(err) {
		t.Error("AGENTS.md not created")
	}

	// Test 2: JSON Output
	args = []string{"init", tmpDir + "/proj2", "--json"}
	out, _, err = invokeCmd(args...)
	if err != nil {
		t.Fatalf("Init JSON failed: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, out)
	}
	if result["status"] != "success" {
		t.Errorf("Expected status=success, got %v", result["status"])
	}
	if result["project"] != "proj2" { // filepath.Base(path)
		t.Errorf("Expected project=proj2, got %v", result["project"])
	}
}

func TestInitCmd_ErrorIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(tmpDir+"/exists", 0755)

	// Test 1: Error Normal
	args := []string{"init", tmpDir + "/exists"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error for existing path")
	}

	// Test 2: Error JSON
	// We have to simulate the main Execute() error handling logic because
	// invokeCmd calls RootCmd.Execute() which returns error, but
	// the handling in main.go (which calls Execute()) contains the printJSONError logic.
	// Wait, root.go defines Execute().
	// But invokeCmd calls RootCmd.Execute() directly, bypassing root.go Execute().

	// We need to verify printErrorJSON.
	// We can manually call printErrorJSON for test coverage or structural refactor.
	// Or we can mock the behavior.

	// Let's test the helper functions directly ensures 100% on them.

	// Test printJSON
	func() {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		printJSON(map[string]string{"foo": "bar"})

		w.Close()
		os.Stdout = old
		out, _ := io.ReadAll(r)

		if !strings.Contains(string(out), `"foo": "bar"`) {
			t.Errorf("printJSON failed, got: %s", string(out))
		}
	}()

	// Test printErrorJSON
	func() {
		// Capture stderr
		old := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		printErrorJSON(io.EOF) // Simple error

		w.Close()
		os.Stderr = old
		out, _ := io.ReadAll(r)

		if !strings.Contains(string(out), `"error": "EOF"`) {
			t.Errorf("printErrorJSON failed, got: %s", string(out))
		}
	}()
}

func TestPrintJSON_Error(t *testing.T) {
	// Capture stderr to avoid polluting test output
	oldStderr := os.Stderr
	os.Stderr, _, _ = os.Pipe()
	defer func() { os.Stderr = oldStderr }()

	// Pass an unsupported type (channel) to trigger encoding error
	err := printJSON(make(chan int))
	if err == nil {
		t.Error("Expected error for unserializable type")
	}
}

func TestExecute_Success(t *testing.T) {
	// Reset SetArgs from previous tests
	RootCmd.SetArgs(nil)

	// Capture output to suppress it
	oldStdout := os.Stdout
	os.Stdout, _, _ = os.Pipe()
	defer func() { os.Stdout = oldStdout }()

	// Set args to --help which always succeeds
	os.Args = []string{"simple", "--help"}

	code := Execute()
	if code != 0 {
		t.Errorf("Expected exit code 0, got %d", code)
	}
}

func TestExecute_Failure(t *testing.T) {
	// Capture stderr
	oldStderr := os.Stderr
	os.Stderr, _, _ = os.Pipe()
	defer func() { os.Stderr = oldStderr }()

	// "init" without args fails
	os.Args = []string{"simple", "init"}

	code := Execute()
	if code != 1 {
		t.Errorf("Expected exit code 1, got %d", code)
	}
}

func TestExecute_FailureJSON(t *testing.T) {
	// Reset
	RootCmd.SetArgs(nil)

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		w.Close()
		os.Stderr = oldStderr
	}()

	// "init" without args fails, with --json
	os.Args = []string{"simple", "init", "--json"}

	code := Execute()
	if code != 1 {
		t.Errorf("Expected exit code 1, got %d", code)
	}

	w.Close()
	os.Stderr = oldStderr // Restore early to read
	out, _ := io.ReadAll(r)

	if !strings.Contains(string(out), `"error":`) {
		t.Errorf("Expected JSON error, got: %s", string(out))
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// invokeCmd is a test helper that executes the root command with specific arguments
// and captures both stdout and stderr.
// It redirects os.Stdout and os.Stderr to buffers to inspect CLI output.
func invokeCmd(args ...string) (string, string, error) {
	// Reset commands for testing to avoid side effects
	jsonOutput = false

	// Create buffers for stdout/stderr which we will inspect later
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)

	// Swap stdout/stderr to capture output from functions that write directly to os.Stdout/Err
	// (like fmt.Println called deep in the command logic)
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w // Redirect stderr to same pipe for simplicity in this helper

	// Configure Cobra command to write to our buffers
	// Note: We do both pipe capture (for fmt.Print) and SetOut (for cmd.Print) to be safe.
	RootCmd.SetOut(outBuf)
	RootCmd.SetErr(errBuf)
	RootCmd.SetArgs(args)

	// Execute the command
	err := RootCmd.Execute()

	// Restore original stdout/stderr
	_ = w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	out, _ := io.ReadAll(r)

	// Combine pipe output with buffer output to get the complete picture of what was printed.
	fullOutput := string(out) + outBuf.String()
	fullErr := errBuf.String()

	return fullOutput, fullErr, err
}

// TestInitCmd_Integration verifies the 'simple init' command flow.
// It checks that:
// 1. The command accepts valid arguments and flags (e.g., --tenant).
// 2. Essential files like simple.scl and AGENTS.md are created.
// 3. JSON output format is correct when requested.
func TestInitCmd_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	// Case 1: Normal initialization with --tenant flag
	args := []string{"init", tmpDir + "/proj1", "--tenant", "acme"}
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

	// Verify simple.scl was created with tenant
	if _, err := os.Stat(tmpDir + "/proj1/simple.scl"); os.IsNotExist(err) {
		t.Error("simple.scl not created")
	}

	// Case 2: JSON Output validation
	args = []string{"init", tmpDir + "/proj2", "--tenant", "myco", "--json"}
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
	if result["project"] != "proj2" {
		t.Errorf("Expected project=proj2, got %v", result["project"])
	}
	if result["tenant"] != "myco" {
		t.Errorf("Expected tenant=myco, got %v", result["tenant"])
	}
}

// TestInitCmd_ErrorIntegration checks how the CLI handles initialization errors.
// Examples include trying to initialize in an existing directory.
func TestInitCmd_ErrorIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Mkdir(tmpDir+"/exists", 0755)

	// Case 1: Error Normal (path already exists)
	args := []string{"init", tmpDir + "/exists", "--tenant", "test"}
	_, _, err := invokeCmd(args...)
	if err == nil {
		t.Error("Expected error for existing path")
	}

	// We also verify our internal helper functions for JSON error printing
	// to ensure consistency even if the Cobra execution flow differs.

	// Helper verification: printJSON
	func() {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		_ = printJSON(map[string]string{"foo": "bar"})

		_ = w.Close()
		os.Stdout = old
		out, _ := io.ReadAll(r)

		if !strings.Contains(string(out), `"foo": "bar"`) {
			t.Errorf("printJSON failed, got: %s", string(out))
		}
	}()

	// Helper verification: printErrorJSON
	func() {
		old := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		printErrorJSON(io.EOF) // Simple error

		_ = w.Close()
		os.Stderr = old
		out, _ := io.ReadAll(r)

		if !strings.Contains(string(out), `"error": "EOF"`) {
			t.Errorf("printErrorJSON failed, got: %s", string(out))
		}
	}()
}

// TestPrintJSON_Error ensures we handle encoding errors gracefully (though unlikely with strings).
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

// TestExecute_Scenarios table-drives tests for top-level command execution issues.
// It covers missing arguments, unknown commands, and bad flag combinations.
func TestExecute_Scenarios(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantOutput string // simplified check (substring)
	}{
		{
			name:     "success --help",
			args:     []string{"simple", "--help"},
			wantCode: 0,
		},
		{
			name:     "failure unknown cmd",
			args:     []string{"simple", "init"}, // requires args
			wantCode: 1,
		},
		{
			name:       "failure json unknown cmd",
			args:       []string{"simple", "init", "--json"},
			wantCode:   1,
			wantOutput: `"error":`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Cobra args
			RootCmd.SetArgs(nil)

			// Capture stderr for output checking
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Capture stdout to suppress success output
			oldStdout := os.Stdout
			os.Stdout, _, _ = os.Pipe()

			defer func() {
				_ = w.Close()
				os.Stderr = oldStderr
				os.Stdout = oldStdout
			}()

			os.Args = tt.args
			code := Execute()

			if code != tt.wantCode {
				t.Errorf("Exit code = %d, want %d", code, tt.wantCode)
			}

			if tt.wantOutput != "" {
				_ = w.Close()
				// Restore early to read
				os.Stderr = oldStderr
				os.Stdout = oldStdout

				out, _ := io.ReadAll(r)
				if !strings.Contains(string(out), tt.wantOutput) {
					t.Errorf("Output = %s, want substring %s", string(out), tt.wantOutput)
				}
			}
		})
	}
}

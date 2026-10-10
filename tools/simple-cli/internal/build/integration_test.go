package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"simple-cli/internal/home"
)

func TestIntegration_DownloadTools(t *testing.T) {
	if os.Getenv("SIMPLE_CLI_INTEGRATION") == "" {
		t.Skip("Skipping integration test (set SIMPLE_CLI_INTEGRATION=1 to run)")
	}

	// Real network calls - careful
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Since we mock ensure functions in manager tests, here we want to test that
	// EnsureSCLParser etc. actually work with EnsureTool.
	// But ensureSCLParser etc. are vars in manager.go, but defined as functions in respective files.
	// This test calls the REAL functions.

	// Use EnsureSCLParser directly
	path, err := EnsureSCLParser(nil)
	if err != nil {
		t.Fatalf("EnsureSCLParser failed: %v", err)
	}

	if !fileExists(path) {
		t.Error("SCL Parser binary not found after ensure")
	}
}

// THE REAL scl-parser, STARTED SEVERAL TIMES AT ONCE ON A MACHINE THAT HAS
// NEVER RUN IT.
//
// scl-parser unpacks its own runtime the first time it runs, and two first
// runs at once collide. Every command reaches it through home.ToolOutput,
// which makes first runs take turns. This starts it the same way, on a clean
// home, and expects every start to parse the file.
func TestIntegration_SCLParserFirstRunsAtOnce(t *testing.T) {
	if os.Getenv("SIMPLE_CLI_INTEGRATION") == "" {
		t.Skip("Skipping integration test (set SIMPLE_CLI_INTEGRATION=1 to run)")
	}

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("SIMPLE_CLI_HOME", tmpDir)

	parser, err := EnsureSCLParser(nil)
	if err != nil {
		t.Fatalf("EnsureSCLParser failed: %v", err)
	}

	sample := filepath.Join(tmpDir, "sample.scl")
	if err := os.WriteFile(sample, []byte("tenant \"test\"\n"), 0644); err != nil {
		t.Fatalf("failed to write a file to parse: %v", err)
	}

	const runs = 6
	failures := make(chan string, runs)
	start := make(chan struct{})
	var wait sync.WaitGroup

	for range runs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start

			output, err := home.ToolOutput(parser, sample)
			if err != nil {
				failures <- err.Error()
				return
			}

			var blocks []map[string]any
			if err := json.Unmarshal(output, &blocks); err != nil || len(blocks) != 1 {
				failures <- "not the parsed file: " + string(output)
			}
		}()
	}

	close(start)
	wait.Wait()
	close(failures)

	for failure := range failures {
		t.Errorf("a first run failed: %s", failure)
	}
}

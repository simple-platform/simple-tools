package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirUsesSimpleCLIHome(t *testing.T) {
	t.Setenv("SIMPLE_CLI_HOME", "/workspace/simple-devstudio/acme")
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if dir != "/workspace/simple-devstudio/acme" {
		t.Fatalf("Dir() = %q", dir)
	}
}

func TestToolEnvAndCommand(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SIMPLE_CLI_HOME", tmpDir)

	env := ToolEnv()
	var hasXDG, hasTMPDIR bool
	expectedXDG := "XDG_DATA_HOME=" + tmpDir + "/.simple/data"
	expectedTMPDIR := "TMPDIR=" + tmpDir + "/.simple/tmp"

	for _, item := range env {
		if item == expectedXDG {
			hasXDG = true
		}
		if item == expectedTMPDIR {
			hasTMPDIR = true
		}
	}

	if !hasXDG {
		t.Errorf("ToolEnv() missing expected %q", expectedXDG)
	}
	if !hasTMPDIR {
		t.Errorf("ToolEnv() missing expected %q", expectedTMPDIR)
	}

	cmd := ToolCommand("echo", "test")
	if cmd == nil {
		t.Fatal("ToolCommand returned nil")
	}
	if len(cmd.Env) == 0 {
		t.Errorf("ToolCommand cmd.Env is empty")
	}
}

func TestToolCommandHomeFollowsSimpleCLIHome(t *testing.T) {
	realHome := t.TempDir()
	tests := []struct {
		name     string
		cliHome  string
		wantHome func(cliHome string) string
	}{
		// DevStudio's sandbox lets a tool write only under SIMPLE_CLI_HOME.
		{"set", t.TempDir(), func(cliHome string) string { return filepath.Join(cliHome, ".simple", "home") }},
		{"unset", "", func(string) string { return realHome }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", realHome)
			t.Setenv("SIMPLE_CLI_HOME", test.cliHome)
			want := test.wantHome(test.cliHome)

			var homes []string
			for _, item := range ToolCommand("scl-parser", "simple.scl").Env {
				if strings.HasPrefix(item, "HOME=") {
					homes = append(homes, strings.TrimPrefix(item, "HOME="))
				}
			}
			if len(homes) != 1 || homes[0] != want {
				t.Fatalf("tool HOME = %v, want exactly %q", homes, want)
			}
			if info, err := os.Stat(want); err != nil || !info.IsDir() {
				t.Fatalf("tool HOME %q is not a folder: %v", want, err)
			}
			if got := os.Getenv("HOME"); got != realHome {
				t.Fatalf("the CLI's own HOME = %q, want it left as %q", got, realHome)
			}
		})
	}
}

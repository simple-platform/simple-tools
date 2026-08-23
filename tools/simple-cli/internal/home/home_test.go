package home

import "testing"

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

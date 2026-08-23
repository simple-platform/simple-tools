package home

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Dir returns the home directory used by Simple CLI state and tool caches.
// DevStudio sets SIMPLE_CLI_HOME to the writable instance workspace while
// preserving HOME for Codex's own authentication and configuration.
func Dir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("SIMPLE_CLI_HOME")); configured != "" {
		return configured, nil
	}
	return os.UserHomeDir()
}

// ToolEnv returns an environment slice suitable for running external or self-extracting tools (such as scl-parser).
// It ensures that XDG_DATA_HOME and TMPDIR are set to writable directories within the Simple CLI directory.
func ToolEnv() []string {
	env := os.Environ()
	homeDir, err := Dir()
	if err != nil {
		return env
	}

	dataDir := filepath.Join(homeDir, ".simple", "data")
	tmpDir := filepath.Join(homeDir, ".simple", "tmp")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(tmpDir, 0755)

	var filtered []string
	for _, item := range env {
		if strings.HasPrefix(item, "XDG_DATA_HOME=") || strings.HasPrefix(item, "TMPDIR=") {
			continue
		}
		filtered = append(filtered, item)
	}

	return append(filtered,
		fmt.Sprintf("XDG_DATA_HOME=%s", dataDir),
		fmt.Sprintf("TMPDIR=%s", tmpDir),
	)
}

// ToolCommand creates an *exec.Cmd configured with ToolEnv for running internal CLI tools.
func ToolCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = ToolEnv()
	return cmd
}

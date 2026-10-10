package home

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"simple-cli/internal/fsx"
)

// How runs of a tool take turns before it has finished its first run. They are
// variables so a test can shorten them.
var (
	// turnPoll is how often a waiting run looks again.
	turnPoll = 25 * time.Millisecond

	// turnAbandoned is how old a turn has to be before it is taken to belong
	// to a run that died holding it. A first run unpacks a few tens of
	// megabytes, which takes about a second.
	turnAbandoned = 2 * time.Minute

	// turnPatience is how long a run waits for its turn before it goes ahead
	// without one, so that nothing here can stop a command for good.
	turnPatience = 2 * time.Minute
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

// ToolOutput runs a tool with ToolEnv and returns what it wrote to standard
// output, as ToolCommand(name, args...).Output() does.
//
// A SELF-EXTRACTING TOOL IS RUN ALONE UNTIL IT HAS RUN ONCE. scl-parser unpacks
// its own runtime under the data directory ToolEnv names the first time it
// runs. Two first runs at once collide: one is still writing the runtime while
// the other starts it, and that one fails with "FileBusy". Two commands started
// together on a machine that has never run the tool is all it takes.
//
// So until a tool has finished one run, runs of it take turns, and after that
// nothing waits. "Has run" is kept beside the runtime, under the same data
// directory, so clearing that directory clears both. It is kept for the file
// the tool is now: a newer version of the tool is a different file, unpacks a
// runtime of its own, and takes turns again for its first run.
//
// THE TURN IS THE TOOL'S, WHICHEVER FILE THE TOOL IS RIGHT NOW. Two commands
// that both find the tool missing each download it, and the one that finishes
// second puts its file in place of the first's. That file has never run, so
// its first run takes a turn, while the first command is still unpacking from
// the file it downloaded. Both unpack the same runtime. A turn kept for each
// file would let the two go at once, so there is one for the tool's name.
func ToolOutput(name string, args ...string) ([]byte, error) {
	ran := ranMarker(name)
	if ran == "" || fileExists(ran) {
		return ToolCommand(name, args...).Output()
	}

	// The first run on a new machine is the one this is for, and on a new
	// machine the data directory is not there yet.
	if err := os.MkdirAll(filepath.Dir(ran), fsx.DirPerm); err != nil {
		return ToolCommand(name, args...).Output()
	}

	done := takeTurn(turnFile(name))
	defer done()

	output, err := ToolCommand(name, args...).Output()
	if err == nil {
		// A run that failed is not counted: it may be the one that was
		// interrupted while unpacking.
		_ = os.WriteFile(ran, nil, fsx.FilePerm)
	}

	return output, err
}

// ranMarker is the file that says this tool has finished a run under this
// home. It is empty when that cannot be said of the tool: one found on PATH
// rather than named by its file, or a home that cannot be found. Such a tool
// is simply run.
func ranMarker(tool string) string {
	info, err := os.Stat(tool)
	if err != nil {
		return ""
	}

	homeDir, err := Dir()
	if err != nil {
		return ""
	}

	name := fmt.Sprintf("%s-%d-%d.ran", filepath.Base(tool), info.Size(), info.ModTime().UnixNano())

	return filepath.Join(homeDir, ".simple", "data", name)
}

// turnFile is the turn that first runs of this tool wait on, beside its
// markers. It is called only for a tool ranMarker has a marker for.
func turnFile(tool string) string {
	homeDir, _ := Dir()

	return filepath.Join(homeDir, ".simple", "data", filepath.Base(tool)+".turn")
}

// takeTurn waits until no other run holds the turn, takes it, and returns what
// gives it back.
//
// The turn is a file that only one run can create. A run that cannot have it
// in time goes ahead anyway, and so does one that cannot create it at all:
// taking turns exists to avoid a failure, and must not become one.
func takeTurn(turn string) func() {
	patience := time.Now().Add(turnPatience)

	for {
		file, err := os.OpenFile(turn, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fsx.FilePerm)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(turn) }
		}
		if !os.IsExist(err) {
			return func() {}
		}

		// A turn nobody has given back for this long belongs to a run that
		// is gone.
		if held, err := os.Stat(turn); err == nil && time.Since(held.ModTime()) > turnAbandoned {
			_ = os.Remove(turn)
			continue
		}

		if time.Now().After(patience) {
			return func() {}
		}

		time.Sleep(turnPoll)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

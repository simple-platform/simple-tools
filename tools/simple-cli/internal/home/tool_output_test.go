package home

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// selfExtractingVariable makes this test binary behave as a self-extracting
// tool when it is started with it set. The tests start the binary as that
// tool, so nothing they run has to be downloaded or built first.
const selfExtractingVariable = "HOME_TEST_SELF_EXTRACTING_TOOL"

// keepUnpacking is the file, in the tool's data directory, that keeps a first
// run unpacking for as long as it exists.
const keepUnpacking = "keep-unpacking"

func TestMain(m *testing.M) {
	switch os.Getenv(selfExtractingVariable) {
	case "unpacks":
		os.Exit(selfExtractingTool())
	case "fails":
		fmt.Fprintln(os.Stderr, "this tool always fails")
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// selfExtractingTool does what scl-parser does on its first run, as far as the
// collision goes: it unpacks a runtime under the data directory it is given,
// and a copy started while another is still unpacking fails. Once the runtime
// is there, any number can run at once.
func selfExtractingTool() int {
	data := os.Getenv("XDG_DATA_HOME")
	runtime := filepath.Join(data, "runtime")
	if _, err := os.Stat(runtime); err == nil {
		fmt.Print("parsed")
		return 0
	}

	unpacking, err := os.OpenFile(filepath.Join(data, "unpacking"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: FileBusy")
		return 1
	}
	_ = unpacking.Close()

	time.Sleep(150 * time.Millisecond)

	// A test keeps the unpacking going for as long as this file is there.
	for fileExists(filepath.Join(data, keepUnpacking)) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := os.WriteFile(runtime, []byte("unpacked"), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.Remove(filepath.Join(data, "unpacking"))

	fmt.Print("parsed")

	return 0
}

// asTool sets a clean home and makes this test binary the tool to run.
func asTool(t *testing.T, behaviour string) (tool, home string) {
	t.Helper()

	home = t.TempDir()
	t.Setenv("SIMPLE_CLI_HOME", home)
	t.Setenv(selfExtractingVariable, behaviour)

	tool, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to find this test binary: %v", err)
	}

	return tool, home
}

// atOnce starts a run several times together and counts the ones that failed.
func atOnce(t *testing.T, runs int, run func() ([]byte, error)) (failed int) {
	t.Helper()

	var (
		wait  sync.WaitGroup
		start = make(chan struct{})
		lock  sync.Mutex
	)

	for range runs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start

			output, err := run()
			if err != nil || string(output) != "parsed" {
				lock.Lock()
				failed++
				lock.Unlock()
			}
		}()
	}

	close(start)
	wait.Wait()

	return failed
}

// THE COLLISION IS REAL, AND TAKING TURNS ENDS IT. The first case starts the
// tool the way the CLI used to and sees first runs fail, which is what makes
// the second case mean something: the same starts through ToolOutput all
// succeed.
func TestToolOutput_FirstRunsAtOnce(t *testing.T) {
	const runs = 8

	t.Run("started with no turns, first runs collide", func(t *testing.T) {
		tool, _ := asTool(t, "unpacks")
		// The data directory is made here because nothing has run yet to
		// make it.
		ToolEnv()

		failed := atOnce(t, runs, func() ([]byte, error) { return ToolCommand(tool).Output() })

		if failed == 0 {
			t.Fatalf("none of %d first runs at once failed, so this tool does not collide and the next case proves nothing", runs)
		}
	})

	t.Run("started through ToolOutput, every one succeeds", func(t *testing.T) {
		tool, home := asTool(t, "unpacks")

		if failed := atOnce(t, runs, func() ([]byte, error) { return ToolOutput(tool) }); failed != 0 {
			t.Errorf("%d of %d first runs at once failed", failed, runs)
		}

		// Nobody is left holding a turn, and the tool is recorded as having run.
		left, err := filepath.Glob(filepath.Join(home, ".simple", "data", "*.turn"))
		if err != nil || len(left) != 0 {
			t.Errorf("turns left behind: %v (%v)", left, err)
		}
		if ran := ranMarker(tool); !fileExists(ran) {
			t.Errorf("the tool is not recorded as having run: %s is missing", ran)
		}

		// Having run once, it no longer takes turns: a turn held by somebody
		// else does not hold it up.
		held := turnFile(tool)
		if err := os.WriteFile(held, nil, 0644); err != nil {
			t.Fatalf("failed to hold the turn: %v", err)
		}
		began := time.Now()
		if _, err := ToolOutput(tool); err != nil {
			t.Errorf("ToolOutput() after the first run error = %v", err)
		}
		if waited := time.Since(began); waited > 2*time.Second {
			t.Errorf("a tool that has run before waited %v for a turn", waited)
		}
	})
}

// THE TURN IS THE TOOL'S, WHICHEVER FILE THE TOOL IS RIGHT NOW. Two commands
// that both find the tool missing each download it, and the one that finishes
// second puts its file in place of the first's. The two files are one tool and
// unpack one runtime, yet they read as different files: the same name and
// size, another time. A first run of one still has to wait for a first run of
// the other.
func TestToolOutput_FirstRunsOfAToolDownloadedTwice(t *testing.T) {
	type finished struct {
		output []byte
		err    error
	}

	tool, home := asTool(t, "unpacks")
	copies := []string{copyOf(t, tool), copyOf(t, tool)}

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(copies[1], later, later); err != nil {
		t.Fatalf("failed to give the second copy another time: %v", err)
	}
	if ranMarker(copies[0]) == ranMarker(copies[1]) {
		t.Fatal("the two copies read as one file, so this proves nothing")
	}

	// Each copy is started once before anything is timed. An operating system
	// can take its time over a program it has never started, and the wait
	// below is only long enough for one it has.
	for _, program := range copies {
		warm := exec.Command(program)
		warm.Env = append(os.Environ(), selfExtractingVariable+"=fails")
		_ = warm.Run()
	}

	ToolEnv()
	data := filepath.Join(home, ".simple", "data")
	keep := filepath.Join(data, keepUnpacking)
	if err := os.WriteFile(keep, nil, 0644); err != nil {
		t.Fatalf("failed to keep the first run unpacking: %v", err)
	}
	release := sync.OnceFunc(func() { _ = os.Remove(keep) })
	defer release()

	run := func(program string) chan finished {
		done := make(chan finished, 1)
		go func() {
			output, err := ToolOutput(program)
			done <- finished{output, err}
		}()

		return done
	}

	first := run(copies[0])

	unpacking := filepath.Join(data, "unpacking")
	for began := time.Now(); !fileExists(unpacking); time.Sleep(5 * time.Millisecond) {
		if time.Since(began) > 30*time.Second {
			t.Fatal("the first run never began to unpack")
		}
	}

	second := run(copies[1])

	select {
	case got := <-second:
		release()
		<-first
		t.Fatalf("the second copy did not wait for the first to finish its first run: it ended with output %q and error %v", got.output, got.err)
	case <-time.After(300 * time.Millisecond):
	}

	release()

	for name, done := range map[string]chan finished{"first": first, "second": second} {
		if got := <-done; got.err != nil || string(got.output) != "parsed" {
			t.Errorf("the %s copy's run ended with output %q and error %v", name, got.output, got.err)
		}
	}
}

// copyOf puts a copy of a program in a directory of its own, under the
// program's own name.
func copyOf(t *testing.T, program string) string {
	t.Helper()

	content, err := os.ReadFile(program)
	if err != nil {
		t.Fatalf("failed to read %s: %v", program, err)
	}

	copied := filepath.Join(t.TempDir(), filepath.Base(program))
	if err := os.WriteFile(copied, content, 0755); err != nil {
		t.Fatalf("failed to write %s: %v", copied, err)
	}

	return copied
}

func TestToolOutput_ARunThatFailsIsNotCountedAsTheFirstRun(t *testing.T) {
	tool, _ := asTool(t, "fails")

	if _, err := ToolOutput(tool); err == nil {
		t.Fatal("ToolOutput() error = nil, want the tool's failure")
	}

	if ran := ranMarker(tool); fileExists(ran) {
		t.Errorf("a tool that failed is recorded as having run: %s", ran)
	}
	if turn := turnFile(tool); fileExists(turn) {
		t.Errorf("the turn was not given back after a failure: %s is still there", turn)
	}
}

// A tool found on PATH is not a file this package can say anything about. It
// is run as it always was.
func TestToolOutput_AToolNamedWithoutAPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIMPLE_CLI_HOME", home)

	if marker := ranMarker("a-tool-that-is-not-a-file"); marker != "" {
		t.Errorf("ranMarker() = %q, want none for a name that is not a file", marker)
	}
	if _, err := ToolOutput("a-tool-that-is-not-a-file"); err == nil {
		t.Error("ToolOutput() error = nil, want the failure to start it")
	}
}

func TestTakeTurn(t *testing.T) {
	shorten := func(t *testing.T, abandoned, patience time.Duration) {
		t.Helper()

		oldPoll, oldAbandoned, oldPatience := turnPoll, turnAbandoned, turnPatience
		turnPoll, turnAbandoned, turnPatience = 5*time.Millisecond, abandoned, patience
		t.Cleanup(func() { turnPoll, turnAbandoned, turnPatience = oldPoll, oldAbandoned, oldPatience })
	}

	t.Run("a free turn is taken, and given back", func(t *testing.T) {
		turn := filepath.Join(t.TempDir(), "tool.turn")

		done := takeTurn(turn)
		if !fileExists(turn) {
			t.Error("the turn is not held after it was taken")
		}
		done()
		if fileExists(turn) {
			t.Error("the turn is still held after it was given back")
		}
	})

	t.Run("a held turn is waited for", func(t *testing.T) {
		shorten(t, time.Minute, time.Minute)
		turn := filepath.Join(t.TempDir(), "tool.turn")

		first := takeTurn(turn)
		go func() {
			time.Sleep(100 * time.Millisecond)
			first()
		}()

		began := time.Now()
		second := takeTurn(turn)
		defer second()

		if waited := time.Since(began); waited < 80*time.Millisecond {
			t.Errorf("the second run waited %v, want it to wait for the first to finish", waited)
		}
		if !fileExists(turn) {
			t.Error("the second run does not hold the turn it waited for")
		}
	})

	t.Run("a turn left by a run that died is taken over", func(t *testing.T) {
		shorten(t, 50*time.Millisecond, time.Minute)
		turn := filepath.Join(t.TempDir(), "tool.turn")
		if err := os.WriteFile(turn, nil, 0644); err != nil {
			t.Fatalf("failed to leave a turn behind: %v", err)
		}
		long := time.Now().Add(-time.Hour)
		if err := os.Chtimes(turn, long, long); err != nil {
			t.Fatalf("failed to age the turn: %v", err)
		}

		done := takeTurn(turn)
		if !fileExists(turn) {
			t.Error("the abandoned turn was not taken over")
		}
		done()
		if fileExists(turn) {
			t.Error("the turn is still held after it was given back")
		}
	})

	t.Run("a run that cannot have a turn in time goes ahead, and leaves the holder's turn alone", func(t *testing.T) {
		shorten(t, time.Minute, 40*time.Millisecond)
		turn := filepath.Join(t.TempDir(), "tool.turn")
		holder := takeTurn(turn)
		defer holder()

		began := time.Now()
		done := takeTurn(turn)
		if waited := time.Since(began); waited < 30*time.Millisecond || waited > 2*time.Second {
			t.Errorf("waited %v, want about the patience of 40ms", waited)
		}
		done()
		if !fileExists(turn) {
			t.Error("giving back a turn that was never had removed the holder's")
		}
	})

	t.Run("a turn that cannot be created does not stop the run", func(t *testing.T) {
		turn := filepath.Join(t.TempDir(), "no-such-directory", "tool.turn")

		done := takeTurn(turn)
		done()

		if fileExists(turn) {
			t.Error("a turn appeared where it could not be created")
		}
	})
}

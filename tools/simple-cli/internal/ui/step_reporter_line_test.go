package ui

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// syncBuffer is a bytes.Buffer the test can read while the reporter's
// ticker goroutine writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *syncBuffer) lines() []string {
	out := strings.TrimSuffix(s.String(), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// sleep advances the bubble's clock and lets the ticker catch up.
func sleep(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestLineReporter_Transcript(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		step := func(id StepID, d time.Duration, detail string) {
			r.Start(id, "transient detail")
			sleep(d)
			r.Done(id, detail)
		}

		step("config", 400*time.Millisecond, "com.acme.crm · tenant acme")
		step("auth", 900*time.Millisecond, "")
		step("version", 200*time.Millisecond, "1.4.3-dev.5 · app.scl updated")
		r.Start("collect", "")
		r.Progress("collect", Progress{Items: 142, ItemsTotal: 496, Noun: "files"})
		sleep(600 * time.Millisecond)
		r.Done("collect", "496 files · 18.2 MB")
		step("connect", 300*time.Millisecond, "devops.acme.simple.dev")
		step("manifest", 41200*time.Millisecond, "312 to upload · 184 already on server")

		r.Start("upload", "")
		sleep(9500 * time.Millisecond)
		r.Progress("upload", Progress{Items: 118, ItemsTotal: 312, Bytes: 4_600_000, BytesTotal: 12_400_000, Noun: "files"})
		sleep(10 * time.Second)
		r.Progress("upload", Progress{Items: 231, ItemsTotal: 312, Bytes: 9_300_000, BytesTotal: 12_400_000, Noun: "files"})
		sleep(9200 * time.Millisecond)
		r.Done("upload", "312 files · 12.4 MB")

		step("publish", 18300*time.Millisecond, "com.acme.crm@1.4.3-dev.5")
		step("install", 112*time.Second, "1.4.3-dev.5")
		r.Close()

		want := `[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✓ Authenticate (0.9s)
[3/9] Bump version
[3/9] ✓ Bump version: 1.4.3-dev.5 · app.scl updated (0.2s)
[4/9] Collect files
[4/9] ✓ Collect files: 496 files · 18.2 MB (0.6s)
[5/9] Connect
[5/9] ✓ Connect: devops.acme.simple.dev (0.3s)
[6/9] Compare with server
      still running (30s)
[6/9] ✓ Compare with server: 312 to upload · 184 already on server (41.2s)
[7/9] Upload files
      118/312 files · 4.6/12.4 MB (37%)
      231/312 files · 9.3/12.4 MB (75%)
[7/9] ✓ Upload files: 312 files · 12.4 MB (28.7s)
[8/9] Publish version
[8/9] ✓ Publish version: com.acme.crm@1.4.3-dev.5 (18.3s)
[9/9] Install to dev
      still running (30s)
      still running (1m00s)
      still running (1m30s)
[9/9] ✓ Install to dev: 1.4.3-dev.5 (1m52s)
`
		if got := out.String(); got != want {
			t.Errorf("transcript mismatch\n got:\n%s\nwant:\n%s", got, want)
		}
	})
}

func TestLineReporter_LineForms(t *testing.T) {
	tests := []struct {
		name string
		run  func(r *LineReporter)
		want []string
	}{
		{
			name: "skip",
			run:  func(r *LineReporter) { r.Skip("upload", "all files already on server") },
			want: []string{"[7/9] - Upload files: all files already on server"},
		},
		{
			name: "skip of a running step has no duration",
			run: func(r *LineReporter) {
				r.Start("install", "")
				sleep(time.Second / 2)
				r.Skip("install", "--no-install")
			},
			want: []string{"[9/9] Install to dev", "[9/9] - Install to dev: --no-install"},
		},
		{
			name: "fail",
			run: func(r *LineReporter) {
				r.Start("upload", "")
				sleep(4200 * time.Millisecond)
				r.Fail("upload", errors.New("upload a.wasm: connection lost"))
			},
			want: []string{"[7/9] Upload files", "[7/9] ✗ Upload files: upload a.wasm: connection lost (4.2s)"},
		},
		{
			name: "fail with nil error",
			run: func(r *LineReporter) {
				r.Start("upload", "")
				r.Fail("upload", nil)
			},
			want: []string{"[7/9] Upload files", "[7/9] ✗ Upload files (0s)"},
		},
		{
			name: "done without start has no duration",
			run:  func(r *LineReporter) { r.Done("auth", "") },
			want: []string{"[2/9] ✓ Authenticate"},
		},
		{
			name: "unknown steps are ignored",
			run: func(r *LineReporter) {
				r.Start("nope", "")
				r.Note("nope", "x")
				r.Progress("nope", Progress{Items: 1})
				r.Done("nope", "")
				r.Skip("nope", "")
				r.Fail("nope", errors.New("x"))
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var out syncBuffer
				r := NewLineReporter(&out, deployPlan)
				tt.run(r)
				r.Close()
				if got := out.lines(); !slices.Equal(got, tt.want) {
					t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
				}
			})
		})
	}
}

func TestLineReporter_ProgressEveryTenSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		defer r.Close()
		sleep(300 * time.Millisecond)
		r.Start("upload", "")
		r.Progress("upload", Progress{Items: 1, ItemsTotal: 10, Noun: "files"})

		sleep(9500 * time.Millisecond) // 9.5s into the step
		if n := len(out.lines()); n != 1 {
			t.Fatalf("progress printed before 10s:\n%s", out.String())
		}
		sleep(1500 * time.Millisecond) // the tick at 11s, 10.7s in, prints it
		if got := out.lines(); len(got) != 2 || got[1] != "      1/10 files (10%)" {
			t.Fatalf("lines after 10s:\n%s", out.String())
		}

		// An unchanged value is not repeated.
		sleep(15 * time.Second)
		if n := len(out.lines()); n != 2 {
			t.Fatalf("an unchanged value was printed again:\n%s", out.String())
		}

		// A change is printed at the next tick, since 10s have passed.
		r.Progress("upload", Progress{Items: 4, ItemsTotal: 10})
		// A late, smaller snapshot does not move the log backwards.
		r.Progress("upload", Progress{Items: 3, ItemsTotal: 10})
		sleep(time.Second)
		if got := out.lines(); len(got) != 3 || got[2] != "      4/10 files (40%)" {
			t.Fatalf("lines after a change:\n%s", out.String())
		}

		// A change within 10s of the last line waits.
		r.Progress("upload", Progress{Items: 5, ItemsTotal: 10})
		sleep(5 * time.Second)
		if n := len(out.lines()); n != 3 {
			t.Fatalf("progress printed within 10s of the last line:\n%s", out.String())
		}
		sleep(5 * time.Second)
		if got := out.lines(); len(got) != 4 || got[3] != "      5/10 files (50%)" {
			t.Fatalf("lines 10s later:\n%s", out.String())
		}
	})
}

func TestLineReporter_ProgressIgnoredForIdleSteps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Progress("upload", Progress{Items: 1, ItemsTotal: 2})
		r.Start("upload", "")
		r.Done("upload", "")
		r.Progress("upload", Progress{Items: 2, ItemsTotal: 2})
		sleep(time.Minute)
		r.Close()
		want := []string{"[7/9] Upload files", "[7/9] ✓ Upload files (0s)"}
		if got := out.lines(); !slices.Equal(got, want) {
			t.Errorf("lines:\n%s", out.String())
		}
	})
}

func TestLineReporter_ProgressWithoutCounters(t *testing.T) {
	tests := []struct {
		name string
		p    Progress
	}{
		{"noun only", Progress{Noun: "files"}},
		{"bytes without a total", Progress{Bytes: 500, Noun: "files"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// Nothing in the value can be drawn, so the step reads as
				// silent: no blank progress line, and the heartbeat still
				// comes at 30s.
				var out syncBuffer
				r := NewLineReporter(&out, deployPlan)
				defer r.Close()
				sleep(500 * time.Millisecond)
				r.Start("upload", "")
				r.Progress("upload", tt.p)
				sleep(31 * time.Second)
				want := []string{"[7/9] Upload files", "      still running (30s)"}
				if got := out.lines(); !slices.Equal(got, want) {
					t.Errorf("lines:\n%q\nwant:\n%q", got, want)
				}
			})
		})
	}
}

func TestLineReporter_HeartbeatAfterThirtySeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		defer r.Close()
		sleep(500 * time.Millisecond)
		r.Start("install", "")

		sleep(29 * time.Second) // 29s into the step
		if n := len(out.lines()); n != 1 {
			t.Fatalf("heartbeat before 30s:\n%s", out.String())
		}
		sleep(2 * time.Second) // the tick at 31s, 30.5s in
		if got := out.lines(); len(got) != 2 || got[1] != "      still running (30s)" {
			t.Fatalf("lines at 30s:\n%s", out.String())
		}

		// A note counts as a line, so it postpones the next heartbeat.
		sleep(20 * time.Second)
		r.Note("install", "↻ retrying")
		sleep(20 * time.Second) // 20s since the note
		if got := out.lines(); len(got) != 3 || got[2] != "      ↻ retrying" {
			t.Fatalf("lines after the note:\n%s", out.String())
		}
		sleep(11 * time.Second) // the tick 30.5s after the note, 81.5s in
		if got := out.lines(); len(got) != 4 || got[3] != "      still running (1m21s)" {
			t.Fatalf("lines 30s after the note:\n%s", out.String())
		}
	})
}

func TestLineReporter_IgnoresDetail(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Start("config", "scl-parser: downloading 1%")
		for i := range 100 {
			r.Detail("config", strings.Repeat("x", i))
		}
		r.Close()
		if got := out.lines(); !slices.Equal(got, []string{"[1/9] Load project config"}) {
			t.Errorf("lines:\n%s", out.String())
		}
	})
}

func TestLineReporter_PrintsNotes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Note("config", "warning: failed to load .env file .env, continuing without it: bad line") // before Start
		r.Start("connect", "")
		r.Note("connect", "server rejected the saved session; signing in again")
		r.Note("connect", "  \n") // blank: nothing to print
		r.Note("connect", "first line\nsecond line")
		r.Close()
		want := []string{
			"      warning: failed to load .env file .env, continuing without it: bad line",
			"[5/9] Connect",
			"      server rejected the saved session; signing in again",
			"      first line",
		}
		if got := out.lines(); !slices.Equal(got, want) {
			t.Errorf("lines:\n%s\nwant:\n%s", out.String(), strings.Join(want, "\n"))
		}
	})
}

func TestLineReporter_Interrupt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Start("publish", "")
		sleep(1500 * time.Millisecond)
		r.Done("publish", "com.acme.crm@1.4.3-dev.5")
		r.Start("install", "")
		sleep(48200 * time.Millisecond)
		r.Interrupt(time.Now())

		// The abandoned worker keeps reporting; none of it may print.
		r.Done("install", "1.4.3-dev.5")
		r.Note("install", "late")
		r.Interrupt(time.Now())
		sleep(time.Minute)

		want := []string{
			"[8/9] Publish version",
			"[8/9] ✓ Publish version: com.acme.crm@1.4.3-dev.5 (1.5s)",
			"[9/9] Install to dev",
			"      still running (30s)",
			"[9/9] ■ Install to dev: interrupted (48.2s)",
		}
		if got := out.lines(); !slices.Equal(got, want) {
			t.Errorf("lines:\n%s\nwant:\n%s", out.String(), strings.Join(want, "\n"))
		}
	})
}

func TestLineReporter_InterruptIsTimedFromTheInterrupt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The runner calls Interrupt only once the work has stopped, up to
		// its grace period later. The ■ line must say when the interrupt
		// came, as the live view and the error do.
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Start("collect", "")
		sleep(300 * time.Millisecond)
		interruptedAt := time.Now()
		sleep(1500 * time.Millisecond)
		// A step that starts after the interrupt ran for no time at all.
		r.Start("connect", "")
		sleep(500 * time.Millisecond)
		r.Interrupt(interruptedAt)

		want := []string{
			"[4/9] Collect files",
			"[5/9] Connect",
			"[4/9] ■ Collect files: interrupted (0.3s)",
			"[5/9] ■ Connect: interrupted (0s)",
		}
		if got := out.lines(); !slices.Equal(got, want) {
			t.Errorf("lines:\n%s\nwant:\n%s", out.String(), strings.Join(want, "\n"))
		}
	})
}

func TestLineReporter_InterruptBetweenSteps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Start("auth", "")
		r.Done("auth", "")
		r.Interrupt(time.Now())
		want := []string{"[2/9] Authenticate", "[2/9] ✓ Authenticate (0s)"}
		if got := out.lines(); !slices.Equal(got, want) {
			t.Errorf("lines:\n%s", out.String())
		}
	})
}

// hookWriter calls onWrite with each write before storing it. The reporter
// writes under its lock, so onWrite runs while that lock is held.
type hookWriter struct {
	syncBuffer
	onWrite func(p []byte)
}

func (h *hookWriter) Write(p []byte) (int, error) {
	h.onWrite(p)
	return h.syncBuffer.Write(p)
}

func TestLineReporter_InterruptClosesBeforeUnlocking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// If Interrupt released the lock between writing the ■ line and
		// closing, a report already waiting on the lock could take it in
		// that gap and print ✓ under ■. So by the time the ■ line is
		// written, every report must already be refused.
		var r *LineReporter
		var closedAtMark, sawMark bool
		out := &hookWriter{onWrite: func(p []byte) {
			if strings.Contains(string(p), "■") {
				sawMark = true
				closedAtMark = r.closed // safe: the reporter holds its lock here
			}
		}}
		r = NewLineReporter(out, deployPlan)
		r.Start("install", "")
		r.Interrupt(time.Now())
		if !sawMark {
			t.Fatalf("no ■ line was written:\n%s", out.String())
		}
		if !closedAtMark {
			t.Error("the reporter still took reports while it wrote the ■ line")
		}
	})
}

func TestLineReporter_NothingPrintsAfterInterrupt(t *testing.T) {
	// Abandoned workers keep reporting while Interrupt runs. Whatever they
	// manage to print must come before the ■ line, never after it.
	for range 100 {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Start("install", "")
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for range 20 {
					r.Note("install", "late note")
				}
				r.Done("install", "1.4.3-dev.5")
			})
		}
		r.Interrupt(time.Now())
		wg.Wait()

		lines := out.lines()
		mark := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "■") })
		if mark >= 0 && mark != len(lines)-1 {
			t.Fatalf("lines after the ■ line:\n%s", out.String())
		}
	}
}

func TestLineReporter_CloseStopsTicker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Start("install", "")
		r.Close()
		// synctest fails the test if the ticker goroutine outlives the
		// bubble; the sleep also proves no heartbeat follows Close.
		sleep(time.Minute)
		if got := out.lines(); !slices.Equal(got, []string{"[9/9] Install to dev"}) {
			t.Errorf("lines:\n%s", out.String())
		}
	})
}

func TestLineReporter_CallsAfterCloseAreNoOps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Close()
		calls := []func(){
			func() { r.Start("config", "") },
			func() { r.Detail("config", "x") },
			func() { r.Note("config", "x") },
			func() { r.Progress("config", Progress{Items: 1}) },
			func() { r.Done("config", "x") },
			func() { r.Skip("config", "x") },
			func() { r.Fail("config", errors.New("x")) },
			func() { r.Interrupt(time.Now()) },
			r.Close,
		}
		for _, call := range calls {
			call()
		}
		if got := out.String(); got != "" {
			t.Errorf("printed after Close:\n%s", got)
		}
	})
}

func TestLineReporter_ConcurrentCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, deployPlan)
		r.Start("collect", "")
		var wg sync.WaitGroup
		for w := range 8 {
			wg.Go(func() {
				for i := range 50 {
					r.Progress("collect", Progress{Items: int64(w*50 + i), ItemsTotal: 400, Noun: "files"})
					time.Sleep(100 * time.Millisecond)
				}
			})
		}
		wg.Wait()
		r.Done("collect", "400 files")
		r.Close()
		lines := out.lines()
		if lines[0] != "[4/9] Collect files" || !strings.HasPrefix(lines[len(lines)-1], "[4/9] ✓ Collect files: 400 files") {
			t.Errorf("lines:\n%s", out.String())
		}
	})
}

func TestLineReporter_WritesNoANSI(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		red := func(s string) string { return "\x1b[31m" + s + "\x1b[0m" }
		plan := []Step{{ID: "a", Title: red("Alpha")}, {ID: "b", Title: "Beta"}}
		r := NewLineReporter(&out, plan)
		r.Start("a", red("detail"))
		r.Note("a", red("note"))
		r.Progress("a", Progress{Items: 1, ItemsTotal: 2, Noun: red("files")})
		sleep(time.Minute)
		r.Done("a", red("done"))
		r.Start("b", "")
		r.Fail("b", errors.New(red("failed")))
		r.Close()
		if got := out.String(); strings.ContainsAny(got, "\x1b\r\t") {
			t.Errorf("output contains control characters: %q", got)
		}
		if !strings.Contains(out.String(), "[1/2] ✓ Alpha: done") {
			t.Errorf("styled text was not stripped:\n%s", out.String())
		}
	})
}

func TestLineReporter_SanitizesErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"keeps the first line", errors.New("record sync failed\n\ngoroutine 1 [running]:\nmain.main()"), "[1/1] ✗ Install: record sync failed (0s)"},
		{"tabs and carriage returns become spaces", errors.New("a\tb\rc"), "[1/1] ✗ Install: a b c (0s)"},
		{"escape sequences are stripped", errors.New("\x1b[1mbold\x1b[0m failure"), "[1/1] ✗ Install: bold failure (0s)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var out syncBuffer
				r := NewLineReporter(&out, []Step{{ID: "install", Title: "Install"}})
				r.Start("install", "")
				r.Fail("install", tt.err)
				r.Close()
				if got := out.lines(); len(got) != 2 || got[1] != tt.want {
					t.Errorf("lines:\n%s\nwant last line %q", out.String(), tt.want)
				}
			})
		})
	}
}

func TestLineReporter_DuplicateIDsUseTheFirstStep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out syncBuffer
		r := NewLineReporter(&out, []Step{{ID: "a", Title: "First"}, {ID: "a", Title: "Second"}})
		r.Start("a", "")
		sleep(31500 * time.Millisecond)
		r.Interrupt(time.Now())
		want := []string{"[1/2] First", "      still running (30s)", "[1/2] ■ First: interrupted (31.5s)"}
		if got := out.lines(); !slices.Equal(got, want) {
			t.Errorf("lines:\n%s\nwant:\n%s", out.String(), strings.Join(want, "\n"))
		}
	})
}

func TestProgressLine(t *testing.T) {
	tests := []struct {
		name string
		p    Progress
		want string
	}{
		{"items and bytes follow bytes", Progress{Items: 118, ItemsTotal: 312, Bytes: 4_600_000, BytesTotal: 12_400_000, Noun: "files"}, "118/312 files · 4.6/12.4 MB (37%)"},
		{"items only", Progress{Items: 142, ItemsTotal: 496, Noun: "files"}, "142/496 files (28%)"},
		{"bytes only", Progress{Bytes: 500, BytesTotal: 1000}, "0.5/1.0 kB (50%)"},
		{"no totals", Progress{Items: 7, Noun: "files"}, "7 files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progressLine(tt.p); got != tt.want {
				t.Errorf("progressLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

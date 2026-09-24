package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"simple-cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeSignals stands in for signal.NotifyContext: interrupt does what a
// SIGINT would. signal.Notify must never run inside a synctest bubble.
type fakeSignals struct {
	mu      sync.Mutex
	fire    context.CancelFunc
	stopped atomic.Bool
}

func (s *fakeSignals) notify(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.fire = cancel
	s.mu.Unlock()
	return c, func() {
		s.stopped.Store(true)
		cancel()
	}
}

func (s *fakeSignals) interrupt() {
	s.mu.Lock()
	fire := s.fire
	s.mu.Unlock()
	fire()
}

var runnerPlan = []ui.Step{
	{ID: "one", Title: "Step one"},
	{ID: "two", Title: "Step two"},
	{ID: "three", Title: "Step three"},
}

func runnerFor(out io.Writer, mode progressMode, signals *fakeSignals) stepRun {
	return stepRun{
		runner: runnerDeps{notifySignals: signals.notify},
		out:    out,
		mode:   mode,
		verb:   "deploy",
		header: "🚀 Testing the runner",
		plan:   runnerPlan,
	}
}

// returning wraps work so a test can tell whether the runner waited for it:
// the flag is set once work has returned.
func returning(work stepWork) (stepWork, *atomic.Bool) {
	var returned atomic.Bool
	return func(ctx context.Context, steps ui.StepReporter) error {
		defer returned.Store(true)
		return work(ctx, steps)
	}, &returned
}

// timedStep is a runStep whose fn sleeps for d and succeeds with detail.
func timedStep(ctx context.Context, steps ui.StepReporter, id ui.StepID, d time.Duration, detail string) error {
	return runStep(ctx, steps, id, "", func() (string, error) {
		time.Sleep(d)
		return detail, nil
	})
}

// waitForInterrupt is a step body that runs for d, raises an interrupt,
// and returns once the work's context is cancelled, as a server request does.
func waitForInterrupt(ctx context.Context, signals *fakeSignals, d time.Duration) (string, error) {
	time.Sleep(d)
	signals.interrupt()
	<-ctx.Done()
	return "", ctx.Err()
}

func TestStepRun_Plain(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name            string
		work            func(signals *fakeSignals) stepWork
		want            string
		wantErr         string
		wantReturned    bool
		wantInterrupted bool
	}{
		{
			name: "success",
			work: func(*fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					for _, id := range []ui.StepID{"one", "two", "three"} {
						if err := timedStep(ctx, steps, id, 1500*time.Millisecond, "ok"); err != nil {
							return err
						}
					}
					return nil
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✓ Step one: ok (1.5s)
[2/3] Step two
[2/3] ✓ Step two: ok (1.5s)
[3/3] Step three
[3/3] ✓ Step three: ok (1.5s)
`,
			wantReturned: true,
		},
		{
			name: "a step fails",
			work: func(*fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					if err := timedStep(ctx, steps, "one", time.Second, ""); err != nil {
						return err
					}
					return runStep(ctx, steps, "two", "", func() (string, error) {
						time.Sleep(2 * time.Second)
						return "", fmt.Errorf("upload a.wasm: %w\nstack", boom)
					})
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✓ Step one (1s)
[2/3] Step two
[2/3] ✗ Step two: upload a.wasm: boom (2s)
`,
			wantErr:      "upload a.wasm: boom\nstack",
			wantReturned: true,
		},
		{
			name: "an interrupt while a step runs",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					if err := timedStep(ctx, steps, "one", time.Second, ""); err != nil {
						return err
					}
					return runStep(ctx, steps, "two", "", func() (string, error) {
						return waitForInterrupt(ctx, signals, 4200*time.Millisecond)
					})
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✓ Step one (1s)
[2/3] Step two
[2/3] ■ Step two: interrupted (4.2s)
`,
			wantErr:         `deploy interrupted during "Step two" after 4.2s`,
			wantReturned:    true,
			wantInterrupted: true,
		},
		{
			name: "an interrupt between steps",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					if err := timedStep(ctx, steps, "one", time.Second, ""); err != nil {
						return err
					}
					signals.interrupt()
					return timedStep(ctx, steps, "two", time.Second, "")
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✓ Step one (1s)
`,
			wantErr:         `deploy interrupted before "Step two"`,
			wantReturned:    true,
			wantInterrupted: true,
		},
		{
			name: "an interrupt before the first step",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					signals.interrupt()
					return timedStep(ctx, steps, "one", time.Second, "")
				}
			},
			want:            "🚀 Testing the runner\n",
			wantErr:         `deploy interrupted before "Step one"`,
			wantReturned:    true,
			wantInterrupted: true,
		},
		{
			name: "an interrupt after the last step, with an error",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					for _, id := range []ui.StepID{"one", "two", "three"} {
						if err := timedStep(ctx, steps, id, time.Second, ""); err != nil {
							return err
						}
					}
					signals.interrupt()
					return ctx.Err()
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✓ Step one (1s)
[2/3] Step two
[2/3] ✓ Step two (1s)
[3/3] Step three
[3/3] ✓ Step three (1s)
`,
			wantErr:         "deploy interrupted",
			wantReturned:    true,
			wantInterrupted: true,
		},
		{
			name: "work that succeeds despite an interrupt succeeded",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					return runStep(ctx, steps, "one", "", func() (string, error) {
						signals.interrupt()
						// The reply raced the interrupt and won.
						return "done anyway", nil
					})
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✓ Step one: done anyway (0s)
`,
			wantReturned: true,
		},
		{
			name: "a failure before an interrupt stays a failure",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					err := runStep(ctx, steps, "one", "", func() (string, error) { return "", boom })
					signals.interrupt()
					return err
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✗ Step one: boom (0s)
`,
			wantErr:      "boom",
			wantReturned: true,
		},
		{
			name: "work that ignores the interrupt is abandoned after the grace period",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					return runStep(ctx, steps, "one", "", func() (string, error) {
						time.Sleep(time.Second)
						signals.interrupt()
						// Deaf to ctx, like a subprocess the CLI waits on.
						time.Sleep(10 * time.Second)
						steps.Note("one", "too late")
						return "too late", nil
					})
				}
			},
			// Timed from the interrupt, not from the end of the grace wait,
			// as the live view does.
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ■ Step one: interrupted (1s)
`,
			wantErr:         `deploy interrupted during "Step one" after 1s`,
			wantInterrupted: true,
		},
		{
			name: "a panic fails the running step",
			work: func(*fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					return runStep(ctx, steps, "one", "", func() (string, error) { panic("kaboom") })
				}
			},
			want: `🚀 Testing the runner
[1/3] Step one
[1/3] ✗ Step one: internal error: kaboom (0s)
`,
			wantErr:      "internal error: kaboom\n\n",
			wantReturned: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				signals := &fakeSignals{}
				var out bytes.Buffer
				work, returned := returning(tt.work(signals))
				result := runnerFor(&out, progressPlain, signals).execute(context.Background(), work)

				checkRunResult(t, result, tt.wantErr, tt.wantInterrupted)
				if returned.Load() != tt.wantReturned {
					t.Errorf("the work had returned = %v, want %v", returned.Load(), tt.wantReturned)
				}
				if got := out.String(); got != tt.want {
					t.Errorf("transcript:\n%s\nwant:\n%s", got, tt.want)
				}

				// Anything an abandoned worker reports later is dropped.
				time.Sleep(time.Minute)
				if got := out.String(); got != tt.want {
					t.Errorf("an abandoned worker printed:\n%s", strings.TrimPrefix(got, tt.want))
				}
			})
		})
	}
}

func checkRunResult(t *testing.T, result stepRunResult, wantErr string, wantInterrupted bool) {
	t.Helper()
	switch {
	case wantErr == "" && result.err != nil:
		t.Errorf("err = %v, want nil", result.err)
	case wantErr != "" && result.err == nil:
		t.Errorf("err = nil, want %q", wantErr)
	case wantErr != "" && !strings.HasPrefix(result.err.Error(), wantErr):
		t.Errorf("err = %q, want %q", result.err, wantErr)
	}
	if result.interrupted != wantInterrupted {
		t.Errorf("interrupted = %v, want %v", result.interrupted, wantInterrupted)
	}
	if wantInterrupted && !errors.Is(result.err, errInterrupted) {
		t.Errorf("err %v does not wrap errInterrupted", result.err)
	}
}

func TestStepRun_StopsListeningAtTheFirstInterrupt(t *testing.T) {
	// Once an interrupt is seen, a second ctrl+c must kill the process, so
	// the signal handler is removed at once, not when the grace wait ends.
	tests := []struct {
		name string
		mode progressMode
	}{
		{"plain", progressPlain},
		{"tty", progressTTY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := &fakeSignals{}
			var stoppedInTime atomic.Bool
			work := func(ctx context.Context, steps ui.StepReporter) error {
				return runStep(ctx, steps, "one", "", func() (string, error) {
					signals.interrupt()
					// Deaf to ctx for a moment, like BumpVersion's write.
					deadline := time.Now().Add(time.Second)
					for !signals.stopped.Load() && time.Now().Before(deadline) {
						time.Sleep(10 * time.Millisecond)
					}
					stoppedInTime.Store(signals.stopped.Load())
					return "", ctx.Err()
				})
			}
			result := runnerFor(io.Discard, tt.mode, signals).execute(context.Background(), work)
			if !result.interrupted {
				t.Fatalf("result = %+v, want interrupted", result)
			}
			if !stoppedInTime.Load() {
				t.Error("signals were still being caught while the work wound down")
			}
		})
	}
}

func TestStepRun_PanicIsAWorkPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		signals := &fakeSignals{}
		result := runnerFor(io.Discard, progressNone, signals).execute(context.Background(), func(context.Context, ui.StepReporter) error {
			panic("no step running")
		})
		var failure *workPanic
		if !errors.As(result.err, &failure) || failure.value != "no step running" {
			t.Fatalf("err = %v, want a *workPanic", result.err)
		}
		if !strings.Contains(failure.Error(), "progress_runner_test.go") {
			t.Errorf("the error has no stack:\n%s", failure.Error())
		}
		if result.interrupted {
			t.Error("a panic was reported as an interrupt")
		}
	})
}

func TestStepRun_None(t *testing.T) {
	tests := []struct {
		name    string
		work    func(signals *fakeSignals) stepWork
		wantErr string
	}{
		{
			name: "success",
			work: func(*fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					return timedStep(ctx, steps, "one", time.Second, "ok")
				}
			},
		},
		{
			name: "interrupt",
			work: func(signals *fakeSignals) stepWork {
				return func(ctx context.Context, steps ui.StepReporter) error {
					return runStep(ctx, steps, "one", "", func() (string, error) {
						return waitForInterrupt(ctx, signals, 1500*time.Millisecond)
					})
				}
			},
			wantErr: `deploy interrupted during "Step one" after 1.5s`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				signals := &fakeSignals{}
				var out bytes.Buffer
				work, returned := returning(tt.work(signals))
				result := runnerFor(&out, progressNone, signals).execute(context.Background(), work)
				checkRunResult(t, result, tt.wantErr, tt.wantErr != "")
				if !returned.Load() {
					t.Error("the runner did not wait for the work")
				}
				if out.Len() != 0 {
					t.Errorf("none mode wrote:\n%s", out.String())
				}
			})
		})
	}
}

// finalFrame returns the plain lines of the final view: everything after
// the program's last frame, which is the last header line before the end.
func finalFrame(t *testing.T, output string) []string {
	t.Helper()
	plain := ansi.Strip(output)
	i := strings.LastIndex(plain, "🚀 Testing the runner")
	if i < 0 {
		t.Fatalf("no final view in:\n%q", plain)
	}
	return strings.Split(strings.TrimSuffix(plain[i:], "\n"), "\n")
}

// assertRows checks each line of frame starts with the matching prefix.
func assertRows(t *testing.T, frame []string, prefixes ...string) {
	t.Helper()
	if len(frame) != len(prefixes) {
		t.Fatalf("final view has %d lines, want %d:\n%s", len(frame), len(prefixes), strings.Join(frame, "\n"))
	}
	for i, prefix := range prefixes {
		if !strings.HasPrefix(frame[i], prefix) {
			t.Errorf("line %d = %q, want prefix %q", i, frame[i], prefix)
		}
	}
}

func TestStepRun_TTY_Success(t *testing.T) {
	signals := &fakeSignals{}
	var out bytes.Buffer
	result := runnerFor(&out, progressTTY, signals).execute(context.Background(), func(ctx context.Context, steps ui.StepReporter) error {
		if err := timedStep(ctx, steps, "one", 0, "first"); err != nil {
			return err
		}
		steps.Note("two", "a note worth keeping")
		if err := timedStep(ctx, steps, "two", 0, "second"); err != nil {
			return err
		}
		return skipStep(ctx, steps, "three", "not needed")
	})
	checkRunResult(t, result, "", false)
	assertRows(t, finalFrame(t, out.String()),
		"🚀 Testing the runner",
		"",
		"  ✅ Step one ",
		"  ✅ Step two ",
		"     a note worth keeping",
		"  –  Step three",
		"",
	)
	if frame := finalFrame(t, out.String()); !strings.HasSuffix(frame[2], "first") || !strings.HasSuffix(frame[5], "not needed") {
		t.Errorf("details missing:\n%s", strings.Join(frame, "\n"))
	}
}

func TestStepRun_TTY_Interrupt(t *testing.T) {
	signals := &fakeSignals{}
	var out bytes.Buffer
	result := runnerFor(&out, progressTTY, signals).execute(context.Background(), func(ctx context.Context, steps ui.StepReporter) error {
		if err := timedStep(ctx, steps, "one", 0, ""); err != nil {
			return err
		}
		return runStep(ctx, steps, "two", "", func() (string, error) {
			return waitForInterrupt(ctx, signals, 50*time.Millisecond)
		})
	})
	checkRunResult(t, result, `deploy interrupted during "Step two" after `, true)
	// The work stopped at once, so there was nothing to wait for.
	if strings.Contains(out.String(), "Waiting for") {
		t.Errorf("a prompt stop printed the waiting notice:\n%q", out.String())
	}
	assertRows(t, finalFrame(t, out.String()),
		"🚀 Testing the runner",
		"",
		"  ✅ Step one",
		"  ■  Step two ",
		"  ○  Step three",
		"",
	)
}

func TestStepRun_TTY_InterruptTimesAgree(t *testing.T) {
	// The work returns as soon as it sees the interrupt, so its end races
	// the forwarded signal to the model. Whichever wins, the ■ row and the
	// error must name the same duration.
	for range 10 {
		signals := &fakeSignals{}
		var out bytes.Buffer
		result := runnerFor(&out, progressTTY, signals).execute(context.Background(), func(ctx context.Context, steps ui.StepReporter) error {
			return runStep(ctx, steps, "one", "", func() (string, error) {
				return waitForInterrupt(ctx, signals, 180*time.Millisecond)
			})
		})
		row := finalFrame(t, out.String())[2]
		fields := strings.Fields(row)
		if len(fields) < 4 || fields[0] != "■" {
			t.Fatalf("row = %q, want an interrupted step", row)
		}
		duration := fields[3]
		if want := `deploy interrupted during "Step one" after ` + duration; result.err == nil || result.err.Error() != want {
			t.Fatalf("err = %v, want %q to match the row %q", result.err, want, row)
		}
	}
}

func TestStepRun_TTY_CtrlCKey(t *testing.T) {
	// On a terminal, raw mode delivers ctrl+c as a key rather than a
	// SIGINT. The model's OnInterrupt is then the only thing that cancels
	// the work, and the ■ row and the error must agree on its duration.
	for range 5 {
		signals := &fakeSignals{}
		var out bytes.Buffer
		stepStarted := make(chan struct{})
		run := runnerFor(&out, progressTTY, signals)
		run.runner.runProgram = func(p *tea.Program) (tea.Model, error) {
			go func() {
				<-stepStarted
				time.Sleep(180 * time.Millisecond)
				p.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
			}()
			return p.Run()
		}
		work, returned := returning(func(ctx context.Context, steps ui.StepReporter) error {
			return runStep(ctx, steps, "one", "", func() (string, error) {
				close(stepStarted)
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(5 * time.Second):
					return "", errors.New("ctrl+c did not cancel the work")
				}
			})
		})
		result := run.execute(context.Background(), work)

		checkRunResult(t, result, `deploy interrupted during "Step one" after `, true)
		if !returned.Load() {
			t.Error("the work was not waited for")
		}
		if !signals.stopped.Load() {
			t.Error("signals were still caught after the key interrupted the run")
		}
		row := finalFrame(t, out.String())[2]
		fields := strings.Fields(row)
		if len(fields) < 4 || fields[0] != "■" {
			t.Fatalf("row = %q, want an interrupted step", row)
		}
		if want := `deploy interrupted during "Step one" after ` + fields[3]; result.err.Error() != want {
			t.Fatalf("err = %v, want %q to match the row %q", result.err, want, row)
		}
	}
}

func TestStepRun_TTY_WaitingNotice(t *testing.T) {
	// Once the frame is gone, a step that is slow to stop must not leave
	// the screen blank: a notice says what the run is waiting for, and the
	// final frame replaces it.
	signals := &fakeSignals{}
	var out bytes.Buffer
	result := runnerFor(&out, progressTTY, signals).execute(context.Background(), func(ctx context.Context, steps ui.StepReporter) error {
		return runStep(ctx, steps, "one", "", func() (string, error) {
			signals.interrupt()
			// Deaf to ctx for a while, like hashing files: well past the
			// notice, and well inside the grace period.
			time.Sleep(4 * slowStopAfter)
			return "", ctx.Err()
		})
	})
	checkRunResult(t, result, `deploy interrupted during "Step one" after `, true)

	const notice = `Waiting for "Step one" to stop (Ctrl+C again to quit now)`
	output := out.String()
	at := strings.Index(output, notice)
	if at < 0 {
		t.Fatalf("no waiting notice in:\n%q", output)
	}
	rest := output[at+len(notice):]
	if !strings.HasPrefix(rest, "\r"+ansi.EraseEntireLine+"🚀 Testing the runner") {
		t.Errorf("the notice is not replaced by the final frame:\n%q", rest)
	}
	assertRows(t, finalFrame(t, output),
		"🚀 Testing the runner",
		"",
		"  ■  Step one ",
		"  ○  Step two",
		"  ○  Step three",
		"",
	)
}

func TestStoppingNotice(t *testing.T) {
	long := strings.Repeat("Very long step title ", 6)
	run := stepRun{out: &bytes.Buffer{}, verb: "install", plan: []ui.Step{{ID: "one", Title: "Install to dev"}, {ID: "two", Title: long}}}
	tests := []struct {
		name    string
		running ui.StepID
		want    string
	}{
		{"between steps", "", "Waiting for the install to stop (Ctrl+C again to quit now)"},
		{"a step running", "one", `Waiting for "Install to dev" to stop (Ctrl+C again to quit now)`},
		{"cut to the row", "two", ansi.Truncate(`Waiting for "`+long+`" to stop (Ctrl+C again to quit now)`, 79, "…")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := newStepTracker(ui.NopReporter{}, run.plan)
			if tt.running != "" {
				steps.Start(tt.running, "")
			}
			got := run.stoppingNotice(steps)
			if got != tt.want {
				t.Errorf("stoppingNotice() = %q, want %q", got, tt.want)
			}
			if ansi.StringWidth(got) > 79 {
				t.Errorf("the notice is %d cells wide; it must fit an 80-column row", ansi.StringWidth(got))
			}
		})
	}
}

func TestAwaitWork_Slow(t *testing.T) {
	tests := []struct {
		name      string
		takes     time.Duration
		withSlow  bool
		wantCalls int
	}{
		{"prompt work", slowStopAfter / 2, true, 0},
		{"slow work", 3 * slowStopAfter, true, 1},
		{"abandoned work", 2 * interruptGrace, true, 1},
		{"no callback", 3 * slowStopAfter, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				results := make(chan error, 1)
				go func() {
					time.Sleep(tt.takes)
					results <- nil
				}()
				calls := 0
				var slow func()
				if tt.withSlow {
					slow = func() { calls++ }
				}
				finished, _ := awaitWork(ctx, results, func() {}, slow)
				if calls != tt.wantCalls {
					t.Errorf("slow called %d times, want %d", calls, tt.wantCalls)
				}
				if want := tt.takes < interruptGrace; finished != want {
					t.Errorf("finished = %v, want %v", finished, want)
				}
				// Let an abandoned work end inside the bubble.
				time.Sleep(tt.takes)
			})
		})
	}
}

func TestStepRun_TTY_ReportsAfterTheProgramStopped(t *testing.T) {
	// The program stops at the interrupt, and its Send drops everything
	// after that. A step the work still ends during the grace wait must
	// show how it ended, not "interrupted".
	signals := &fakeSignals{}
	var out bytes.Buffer
	result := runnerFor(&out, progressTTY, signals).execute(context.Background(), func(ctx context.Context, steps ui.StepReporter) error {
		steps.Start("one", "")
		signals.interrupt()
		<-ctx.Done()
		// Long enough for the program to have shut down.
		time.Sleep(200 * time.Millisecond)
		steps.Done("one", "finished in the grace period")
		return runStep(ctx, steps, "two", "", func() (string, error) { return "", nil })
	})
	checkRunResult(t, result, `deploy interrupted before "Step two"`, true)
	frame := finalFrame(t, out.String())
	assertRows(t, frame,
		"🚀 Testing the runner",
		"",
		"  ✅ Step one",
		"  ○  Step two",
		"  ○  Step three",
		"",
	)
	if !strings.HasSuffix(frame[2], "finished in the grace period") {
		t.Errorf("row = %q, want the late detail", frame[2])
	}
}

func TestStepRun_TTY_InterruptBeforeTheWorkStarts(t *testing.T) {
	signals := &fakeSignals{}
	var out bytes.Buffer
	var ran atomic.Int32
	run := runnerFor(&out, progressTTY, signals)
	run.runner.runProgram = func(p *tea.Program) (tea.Model, error) {
		signals.interrupt()
		return p.Run()
	}
	result := run.execute(context.Background(), func(ctx context.Context, steps ui.StepReporter) error {
		ran.Add(1)
		return timedStep(ctx, steps, "one", 0, "")
	})
	// Whether the program got as far as starting the work is a race; the
	// outcome is the same either way.
	checkRunResult(t, result, `deploy interrupted before "Step one"`, true)
	assertRows(t, finalFrame(t, out.String()),
		"🚀 Testing the runner",
		"",
		"  ○  Step one",
		"  ○  Step two",
		"  ○  Step three",
		"",
	)
	if ran.Load() > 1 {
		t.Errorf("the work ran %d times", ran.Load())
	}
}

func TestStepRun_TTY_ProgramEndsBeforeTheWorkStarts(t *testing.T) {
	// The program can end before it runs its start command, as when an
	// interrupt reaches it first. The runner then owns the start: the work
	// must never run, and the run ends interrupted before its first step.
	signals := &fakeSignals{}
	var out bytes.Buffer
	run := runnerFor(&out, progressTTY, signals)
	run.runner.runProgram = func(p *tea.Program) (tea.Model, error) {
		signals.interrupt()
		// Ends the program that never ran, so the forwarded interrupt is
		// not left waiting on it.
		p.Kill()
		return nil, nil
	}
	var ran atomic.Bool
	result := run.execute(context.Background(), func(context.Context, ui.StepReporter) error {
		ran.Store(true)
		return nil
	})
	checkRunResult(t, result, `deploy interrupted before "Step one"`, true)
	if ran.Load() {
		t.Error("the work ran after the program had ended")
	}
	if out.Len() != 0 {
		t.Errorf("printed %q without a final model to draw", out.String())
	}
}

func TestStepRun_TTY_StartupFailureFallsBackToPlain(t *testing.T) {
	signals := &fakeSignals{}
	var out bytes.Buffer
	var ran atomic.Int32
	run := runnerFor(&out, progressTTY, signals)
	run.runner.runProgram = func(p *tea.Program) (tea.Model, error) {
		return nil, errors.New("could not open a new TTY")
	}
	result := run.execute(context.Background(), func(ctx context.Context, steps ui.StepReporter) error {
		ran.Add(1)
		return timedStep(ctx, steps, "one", 0, "ok")
	})
	checkRunResult(t, result, "", false)
	want := "🚀 Testing the runner\n[1/3] Step one\n[1/3] ✓ Step one: ok (0s)\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
	if ran.Load() != 1 {
		t.Errorf("the work ran %d times, want once", ran.Load())
	}
}

func TestStepRun_TTY_DisplayFailureKeepsWaiting(t *testing.T) {
	signals := &fakeSignals{}
	var out bytes.Buffer
	started := make(chan struct{})
	release := make(chan struct{})
	run := runnerFor(&out, progressTTY, signals)
	run.runner.runProgram = func(p *tea.Program) (tea.Model, error) {
		go func() {
			<-started
			p.Kill()
		}()
		model, err := p.Run()
		close(release)
		return model, err
	}
	work, returned := returning(func(ctx context.Context, steps ui.StepReporter) error {
		close(started)
		// The work outlives the display, and must still be waited for.
		<-release
		return timedStep(ctx, steps, "one", 0, "ok")
	})
	result := run.execute(context.Background(), work)
	checkRunResult(t, result, "", false)
	if !returned.Load() {
		t.Error("the runner did not wait for the work")
	}
	tail := ansi.Strip(out.String())
	if !strings.HasSuffix(tail, "progress display stopped (program was killed); waiting for the deploy to finish\n") {
		t.Errorf("output does not end with the notice:\n%q", tail)
	}
}

func TestPickProgressMode(t *testing.T) {
	tests := []struct {
		name     string
		json     bool
		terminal bool
		term, ci string
		want     progressMode
	}{
		{"json wins", true, true, "xterm", "", progressNone},
		{"terminal", false, true, "xterm-256color", "", progressTTY},
		{"pipe", false, false, "xterm", "", progressPlain},
		{"dumb terminal", false, true, "dumb", "", progressPlain},
		{"CI=true", false, true, "xterm", "true", progressPlain},
		{"CI=1", false, true, "xterm", "1", progressPlain},
		{"CI=false", false, true, "xterm", "false", progressTTY},
		{"CI=FALSE", false, true, "xterm", "FALSE", progressTTY},
		{"CI=0", false, true, "xterm", "0", progressTTY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickProgressMode(tt.json, tt.terminal, tt.term, tt.ci); got != tt.want {
				t.Errorf("pickProgressMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProgressModeFor(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("CI", "")
	tests := []struct {
		name string
		out  io.Writer
		json bool
		want progressMode
	}{
		{"json", &bytes.Buffer{}, true, progressNone},
		{"buffer", &bytes.Buffer{}, false, progressPlain},
		{"pipe", writer, false, progressPlain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progressModeFor(tt.out, tt.json); got != tt.want {
				t.Errorf("progressModeFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTerminalSize(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	for name, out := range map[string]io.Writer{"buffer": &bytes.Buffer{}, "pipe": writer} {
		if w, h := terminalSize(out); w != 0 || h != 0 {
			t.Errorf("%s: terminalSize() = %dx%d, want 0x0 (the model's default)", name, w, h)
		}
	}
}

func TestRunStep(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		cancel    string // "before", "during" or ""
		fnErr     error
		wantErr   error
		wantCalls []string
	}{
		{"success", "", nil, nil, []string{"start one starting", "done one finished"}},
		{"failure", "", boom, boom, []string{"start one starting", "fail one boom"}},
		{"failure after a cancel is left running", "during", boom, boom, []string{"start one starting"}},
		{"cancelled before the start", "before", nil, context.Canceled, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel == "before" {
				cancel()
			}
			steps := &recordingReporter{}
			err := runStep(ctx, steps, "one", "starting", func() (string, error) {
				if tt.cancel == "during" {
					cancel()
				}
				return "finished", tt.fnErr
			})
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if got := steps.got(); fmt.Sprint(got) != fmt.Sprint(tt.wantCalls) {
				t.Errorf("reports = %q, want %q", got, tt.wantCalls)
			}
		})
	}
}

func TestSkipStep(t *testing.T) {
	steps := &recordingReporter{}
	if err := skipStep(context.Background(), steps, "one", "not needed"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := skipStep(ctx, steps, "two", "not needed"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if got := steps.got(); fmt.Sprint(got) != "[skip one not needed]" {
		t.Errorf("reports = %q", got)
	}
}

func TestStepTracker_Position(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracker := newStepTracker(ui.NopReporter{}, runnerPlan)
		began := time.Now()

		if step, running, _ := tracker.position(began); step.ID != "one" || running {
			t.Errorf("before any step: %v, %v; want one, not running", step.ID, running)
		}

		tracker.Start("one", "")
		time.Sleep(4200 * time.Millisecond)
		if step, running, elapsed := tracker.position(time.Now()); step.ID != "one" || !running || elapsed != 4200*time.Millisecond {
			t.Errorf("while one runs: %v, %v, %v", step.ID, running, elapsed)
		}
		if _, _, elapsed := tracker.position(began); elapsed != 0 {
			t.Errorf("elapsed before the start = %v, want 0", elapsed)
		}

		tracker.Done("one", "")
		tracker.Skip("two", "")
		if step, running, _ := tracker.position(time.Now()); step.ID != "three" || running {
			t.Errorf("after one and two: %v, %v; want three, not running", step.ID, running)
		}

		tracker.Start("three", "")
		tracker.Fail("three", errors.New("boom"))
		if step, running, _ := tracker.position(time.Now()); step.ID != "" || running {
			t.Errorf("after every step: %v, %v; want none", step.ID, running)
		}
		if !tracker.hasFailed() {
			t.Error("a failed step was not recorded")
		}
	})
}

func TestStepTracker_UnknownRunningStep(t *testing.T) {
	tracker := newStepTracker(ui.NopReporter{}, runnerPlan)
	tracker.Start("elsewhere", "")
	if step, running, _ := tracker.position(time.Now()); step.ID != "one" || running {
		t.Errorf("position = %v, %v; want the first step, not running", step.ID, running)
	}
}

func TestStepTracker_ForwardsUntilFrozen(t *testing.T) {
	inner := &recordingReporter{}
	tracker := newStepTracker(inner, runnerPlan)
	tracker.Start("one", "go")
	tracker.Detail("one", "halfway")
	tracker.Note("one", "noted")
	tracker.Progress("one", ui.Progress{Items: 1, ItemsTotal: 2})
	tracker.Done("one", "done")
	tracker.freeze()
	tracker.Start("two", "")
	tracker.Fail("two", errors.New("late"))
	tracker.failRunning(errors.New("later"))

	want := "[start one go detail one halfway note one noted progress one 1/2 0/0 done one done]"
	if got := fmt.Sprint(inner.got()); got != want {
		t.Errorf("forwarded = %s, want %s", got, want)
	}
	if !tracker.hasFailed() {
		t.Error("failRunning did not mark the run failed")
	}
	if step, running, _ := tracker.position(time.Now()); step.ID != "two" || running {
		t.Errorf("a frozen tracker recorded reports: %v, %v", step.ID, running)
	}
}

func TestInterruptClock(t *testing.T) {
	var clock interruptClock
	first := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	if got := clock.mark(first); !got.Equal(first) {
		t.Errorf("mark = %v, want %v", got, first)
	}
	if got := clock.mark(first.Add(time.Second)); !got.Equal(first) {
		t.Errorf("second mark = %v, want the first %v", got, first)
	}
}

func TestSentMessages(t *testing.T) {
	var sent sentMessages
	sent.add(ui.FinishedMsg{})
	all := sent.all()
	sent.add(ui.FinishedMsg{})
	if len(all) != 1 || len(sent.all()) != 2 {
		t.Errorf("all() does not return a copy: %d, %d", len(all), len(sent.all()))
	}
}

func TestPrintFinalViewIgnoresOtherModels(t *testing.T) {
	var out bytes.Buffer
	stepRun{out: &out}.printFinalView(nil, &sentMessages{})
	if out.Len() != 0 {
		t.Errorf("printed %q for a nil model", out.String())
	}
}

package ui

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestNewStepsModel(t *testing.T) {
	tests := []struct {
		name                    string
		cfg                     StepsModelConfig
		wantWidth, wantHeight   int
		wantTitleWidth, wantRow int
	}{
		{
			name:      "zero size defaults to 80x24",
			cfg:       StepsModelConfig{Plan: deployPlan},
			wantWidth: 80, wantHeight: 24, wantTitleWidth: len("Compare with server"), wantRow: 9,
		},
		{
			name:      "seeded size",
			cfg:       StepsModelConfig{Plan: deployPlan[:2], Width: 120, Height: 40},
			wantWidth: 120, wantHeight: 40, wantTitleWidth: len("Load project config"), wantRow: 2,
		},
		{
			name:      "titles are sanitized before measuring",
			cfg:       StepsModelConfig{Plan: []Step{{ID: "a", Title: "\x1b[1mBold\x1b[0m\tTitle"}}},
			wantWidth: 80, wantHeight: 24, wantTitleWidth: len("Bold Title"), wantRow: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewStepsModel(tt.cfg)
			if m.width != tt.wantWidth || m.height != tt.wantHeight {
				t.Errorf("size = %dx%d, want %dx%d", m.width, m.height, tt.wantWidth, tt.wantHeight)
			}
			if m.titleWidth != tt.wantTitleWidth {
				t.Errorf("titleWidth = %d, want %d", m.titleWidth, tt.wantTitleWidth)
			}
			if len(m.rows) != tt.wantRow {
				t.Errorf("rows = %d, want %d", len(m.rows), tt.wantRow)
			}
			for i, r := range m.rows {
				if r.state != statePending {
					t.Errorf("row %d state = %v, want pending", i, r.state)
				}
			}
		})
	}
}

func TestNewStepsModel_DuplicateIDKeepsFirst(t *testing.T) {
	m := NewStepsModel(StepsModelConfig{Plan: []Step{{ID: "a", Title: "First"}, {ID: "a", Title: "Second"}}})
	m = send(t, m, started("a", 1, ""))
	if m.rows[0].state != stateRunning || m.rows[1].state != statePending {
		t.Errorf("states = %v, %v; want the first row running", m.rows[0].state, m.rows[1].state)
	}
}

func TestStepsModel_Update(t *testing.T) {
	boom := errors.New("upload a.wasm: connection lost\ngoroutine 7 [running]")
	tests := []struct {
		name  string
		setup []tea.Msg
		msg   tea.Msg
		check func(t *testing.T, m StepsModel)
	}{
		{
			name: "start",
			msg:  started("auth", 1, "signing in"),
			check: func(t *testing.T, m StepsModel) {
				r := m.rows[1]
				if r.state != stateRunning || r.detail != "signing in" || !r.started.Equal(at(1)) {
					t.Errorf("row = %+v, want running since 1s with detail", r)
				}
			},
		},
		{
			name:  "detail replaces a running step's detail",
			setup: []tea.Msg{started("auth", 1, "a")},
			msg:   stepDetailMsg{stepEvent{"auth", at(2)}, "b\tc"},
			check: func(t *testing.T, m StepsModel) {
				if got := m.rows[1].detail; got != "b c" {
					t.Errorf("detail = %q, want %q", got, "b c")
				}
			},
		},
		{
			name:  "detail for a done step is ignored",
			setup: []tea.Msg{started("auth", 1, ""), done("auth", 2, "outcome")},
			msg:   stepDetailMsg{stepEvent{"auth", at(3)}, "late"},
			check: func(t *testing.T, m StepsModel) {
				if got := m.rows[1].detail; got != "outcome" {
					t.Errorf("detail = %q, want %q", got, "outcome")
				}
			},
		},
		{
			name:  "note keeps the latest",
			setup: []tea.Msg{started("connect", 1, ""), stepNoteMsg{stepEvent{"connect", at(2)}, "first"}},
			msg:   stepNoteMsg{stepEvent{"connect", at(3)}, "second\nignored"},
			check: func(t *testing.T, m StepsModel) {
				if got := m.rows[4].note; got != "second" {
					t.Errorf("note = %q, want %q", got, "second")
				}
			},
		},
		{
			name:  "progress on a running step",
			setup: []tea.Msg{started("upload", 1, "")},
			msg:   progressed("upload", 2, Progress{Items: 1, ItemsTotal: 3, Noun: "files"}),
			check: func(t *testing.T, m StepsModel) {
				r := m.rows[6]
				if !r.hasProgress || r.progress.Items != 1 || r.progress.ItemsTotal != 3 {
					t.Errorf("row = %+v, want progress 1/3", r)
				}
			},
		},
		{
			name:  "done records outcome and duration",
			setup: []tea.Msg{started("upload", 1, ""), progressed("upload", 2, Progress{Items: 1, ItemsTotal: 3})},
			msg:   done("upload", 4.5, "3 files · 1.2 MB"),
			check: func(t *testing.T, m StepsModel) {
				r := m.rows[6]
				if r.state != stateDone || r.detail != "3 files · 1.2 MB" || r.ended.Sub(r.started) != 3500*time.Millisecond {
					t.Errorf("row = %+v, want done after 3.5s", r)
				}
				if r.hasProgress {
					t.Error("a done step still carries progress")
				}
			},
		},
		{
			name: "done without start has zero duration",
			msg:  done("auth", 2, ""),
			check: func(t *testing.T, m StepsModel) {
				r := m.rows[1]
				if r.state != stateDone || !r.started.Equal(at(2)) {
					t.Errorf("row = %+v, want done at 2s", r)
				}
			},
		},
		{
			name: "skip has no duration",
			msg:  stepSkippedMsg{stepEvent{"upload", at(2)}, "all files already on server"},
			check: func(t *testing.T, m StepsModel) {
				r := m.rows[6]
				if r.state != stateSkipped || r.detail != "all files already on server" || !r.started.IsZero() {
					t.Errorf("row = %+v, want skipped with reason and no times", r)
				}
			},
		},
		{
			name:  "fail keeps the first line",
			setup: []tea.Msg{started("upload", 1, "")},
			msg:   stepFailedMsg{stepEvent{"upload", at(5.2)}, boom.Error()},
			check: func(t *testing.T, m StepsModel) {
				r := m.rows[6]
				if r.state != stateFailed || r.detail != "upload a.wasm: connection lost" {
					t.Errorf("row = %+v, want failed with the first line", r)
				}
			},
		},
		{
			name: "unknown ID changes nothing",
			msg:  started("nope", 9, "x"),
			check: func(t *testing.T, m StepsModel) {
				for i, r := range m.rows {
					if r.state != statePending {
						t.Errorf("row %d = %+v, want pending", i, r)
					}
				}
				if !m.now.Equal(epoch) {
					t.Errorf("now = %v, want unchanged", m.now)
				}
			},
		},
		{
			name: "window size",
			msg:  tea.WindowSizeMsg{Width: 100, Height: 30},
			check: func(t *testing.T, m StepsModel) {
				if m.width != 100 || m.height != 30 {
					t.Errorf("size = %dx%d, want 100x30", m.width, m.height)
				}
			},
		},
		{
			name: "zero window size keeps the last known size",
			msg:  tea.WindowSizeMsg{},
			check: func(t *testing.T, m StepsModel) {
				if m.width != 80 || m.height != 24 {
					t.Errorf("size = %dx%d, want 80x24", m.width, m.height)
				}
			},
		},
		{
			name: "tick sets now",
			msg:  spinner.TickMsg{Time: at(7)},
			check: func(t *testing.T, m StepsModel) {
				if !m.now.Equal(at(7)) {
					t.Errorf("now = %v, want %v", m.now, at(7))
				}
			},
		},
		{
			name:  "now never moves backwards",
			setup: []tea.Msg{spinner.TickMsg{Time: at(7)}},
			msg:   started("auth", 3, ""),
			check: func(t *testing.T, m StepsModel) {
				if !m.now.Equal(at(7)) {
					t.Errorf("now = %v, want %v", m.now, at(7))
				}
			},
		},
		{
			name: "other keys are ignored",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")},
			check: func(t *testing.T, m StepsModel) {
				if m.quitting {
					t.Error("q quit the display")
				}
			},
		},
		{
			name: "unknown messages are ignored",
			msg:  struct{}{},
			check: func(t *testing.T, m StepsModel) {
				if m.quitting {
					t.Error("an unknown message quit the display")
				}
			},
		},
		{
			name:  "step messages are ignored once quitting",
			setup: []tea.Msg{started("auth", 1, ""), FinishedMsg{}},
			msg:   done("auth", 2, "late"),
			check: func(t *testing.T, m StepsModel) {
				if m.rows[1].state != stateRunning {
					t.Errorf("state = %v, want running", m.rows[1].state)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := send(t, newFixtureModel(80, 24), tt.setup...)
			tt.check(t, send(t, m, tt.msg))
		})
	}
}

func TestStepsModel_UpdateLeavesEarlierModelIntact(t *testing.T) {
	before := newFixtureModel(80, 24)
	_ = send(t, before, started("config", 1, "x"), done("config", 2, "y"))
	if before.rows[0].state != statePending {
		t.Errorf("earlier model changed: row 0 = %+v", before.rows[0])
	}
}

func TestStepsModel_ProgressIsMonotonic(t *testing.T) {
	m := send(t, newFixtureModel(80, 24),
		started("upload", 1, ""),
		progressed("upload", 2, Progress{Items: 5, ItemsTotal: 10, Bytes: 500, BytesTotal: 1000, Noun: "files"}),
		// A worker that snapshotted earlier delivers late.
		progressed("upload", 3, Progress{Items: 4, ItemsTotal: 10, Bytes: 600, BytesTotal: 1000}),
	)
	want := Progress{Items: 5, ItemsTotal: 10, Bytes: 600, BytesTotal: 1000, Noun: "files"}
	if got := m.rows[6].progress; got != want {
		t.Errorf("progress = %+v, want %+v", got, want)
	}
}

func TestStepsModel_IgnoresProgressForIdleSteps(t *testing.T) {
	p := Progress{Items: 1, ItemsTotal: 2}
	tests := []struct {
		name  string
		setup []tea.Msg
	}{
		{"pending", nil},
		{"done", []tea.Msg{started("upload", 1, ""), done("upload", 2, "")}},
		{"failed", []tea.Msg{started("upload", 1, ""), stepFailedMsg{stepEvent{"upload", at(2)}, "x"}}},
		{"skipped", []tea.Msg{stepSkippedMsg{stepEvent{"upload", at(2)}, "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := send(t, newFixtureModel(80, 24), tt.setup...)
			m = send(t, m, progressed("upload", 3, p))
			if m.rows[6].hasProgress {
				t.Errorf("row %+v took progress", m.rows[6])
			}
		})
	}
}

func TestStepsModel_CtrlCInterrupts(t *testing.T) {
	var calls atomic.Int32
	var interruptedAt time.Time
	m := NewStepsModel(StepsModelConfig{Header: deployHeader, Plan: deployPlan, OnInterrupt: func(at time.Time) {
		calls.Add(1)
		interruptedAt = at
	}})
	m = send(t, m, stepStartedMsg{stepEvent{"config", time.Now()}, ""})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(StepsModel)
	if calls.Load() != 1 {
		t.Fatalf("OnInterrupt called %d times, want 1", calls.Load())
	}
	// The runner times its error from this moment, so it must be the one
	// the row ended with, not a second reading of the clock.
	if !interruptedAt.Equal(m.rows[0].ended) {
		t.Errorf("OnInterrupt got %v, the row ended at %v", interruptedAt, m.rows[0].ended)
	}
	if !isQuit(cmd) {
		t.Error("ctrl+c did not return tea.Quit")
	}
	if !m.Interrupted() || m.Finished() {
		t.Errorf("Interrupted() = %v, Finished() = %v; want true, false", m.Interrupted(), m.Finished())
	}
	if m.rows[0].state != stateInterrupted || m.rows[0].detail != "interrupted" {
		t.Errorf("running row = %+v, want interrupted", m.rows[0])
	}
	if m.rows[1].state != statePending {
		t.Errorf("pending row = %+v, want unchanged", m.rows[1])
	}

	// A second ctrl+c while shutting down must not cancel again.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if calls.Load() != 1 || cmd != nil {
		t.Errorf("second ctrl+c: OnInterrupt calls = %d, cmd = %v; want 1, nil", calls.Load(), cmd)
	}
}

func TestStepsModel_CtrlZSuspends(t *testing.T) {
	// Raw mode delivers ctrl+z as a key instead of stopping the process, so
	// the model must ask the program to suspend, and carry on afterwards.
	called := false
	m := NewStepsModel(StepsModelConfig{Header: deployHeader, Plan: deployPlan, OnInterrupt: func(time.Time) { called = true }})
	m = send(t, m, started("install", 1, ""))

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	m = next.(StepsModel)
	if cmd == nil {
		t.Fatal("ctrl+z returned no command")
	}
	if _, ok := cmd().(tea.SuspendMsg); !ok {
		t.Errorf("ctrl+z returned %T, want tea.SuspendMsg", cmd())
	}
	if called || m.quitting || m.Interrupted() || m.rows[8].state != stateRunning {
		t.Errorf("ctrl+z interrupted the run: OnInterrupt called %v, row %+v", called, m.rows[8])
	}

	// After fg the program sends ResumeMsg; the frame just goes on.
	next, cmd = m.Update(tea.ResumeMsg{})
	if cmd != nil || next.(StepsModel).quitting {
		t.Error("resuming changed the model")
	}

	// Once the display is quitting, there is nothing left to suspend.
	m = send(t, m, FinishedMsg{})
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ}); cmd != nil {
		t.Error("ctrl+z while quitting returned a command")
	}
}

func TestStepsModel_CtrlCWithoutCallback(t *testing.T) {
	next, cmd := newFixtureModel(80, 24).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) || !next.(StepsModel).Interrupted() {
		t.Error("ctrl+c without OnInterrupt did not interrupt")
	}
}

func TestStepsModel_InterruptMsg(t *testing.T) {
	tests := []struct {
		name    string
		at      time.Time
		wantEnd time.Time
	}{
		{"uses the signal's time", at(48.2), at(48.2)},
		{"zero time falls back to the model's clock", time.Time{}, at(10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			m := newFixtureModel(80, 24)
			m.onInterrupt = func(time.Time) { called = true }
			m = send(t, m, started("install", 0, ""), spinner.TickMsg{Time: at(10)})

			next, cmd := m.Update(InterruptMsg{At: tt.at})
			m = next.(StepsModel)
			if !isQuit(cmd) || !m.Interrupted() {
				t.Fatal("InterruptMsg did not quit as interrupted")
			}
			if called {
				t.Error("InterruptMsg called OnInterrupt; the runner has already cancelled")
			}
			if r := m.rows[8]; r.state != stateInterrupted || !r.ended.Equal(tt.wantEnd) {
				t.Errorf("row = %+v, want interrupted at %v", r, tt.wantEnd)
			}
			if _, cmd := m.Update(InterruptMsg{At: at(50)}); cmd != nil {
				t.Error("a second InterruptMsg returned a command")
			}
		})
	}
}

// interruptedBetweenSteps is the §3 timeline interrupted after the manifest
// step, with nothing running.
func interruptedBetweenSteps(t *testing.T, width, height int) StepsModel {
	t.Helper()
	return send(t, newFixtureModel(width, height), append(throughManifest(), InterruptMsg{At: at(43.7)})...)
}

func TestStepsModel_ReportsAfterInterrupt(t *testing.T) {
	// The work can end a step after the interrupt: a reply that raced it,
	// or a write the grace period lets finish. The runner decides the
	// outcome from the work's result, so the frame must show how each step
	// really ended, not "interrupted" over a step that succeeded.
	tests := []struct {
		name string
		base func(t *testing.T, width, height int) StepsModel
		late []tea.Msg
		row  int
		want string
	}{
		{
			name: "a done that raced the interrupt is drawn",
			base: installInterruptedModel,
			late: []tea.Msg{done("install", 140.2, "1.4.3-dev.5"), FinishedMsg{}},
			row:  8,
			want: "  ✅ Install to dev       48.3s  1.4.3-dev.5",
		},
		{
			name: "a failure after the interrupt is drawn",
			base: installInterruptedModel,
			late: []tea.Msg{stepFailedMsg{stepEvent{"install", at(140.3)}, "record sync failed\nstack"}},
			row:  8,
			want: "  ❌ Install to dev       48.4s  record sync failed",
		},
		{
			name: "a skip after the interrupt is drawn",
			base: interruptedBetweenSteps,
			late: []tea.Msg{stepSkippedMsg{stepEvent{"upload", at(43.8)}, "all files already on server"}},
			row:  6,
			want: "  –  Upload files                all files already on server",
		},
		{
			name: "a step started after the interrupt was cut short at once",
			base: interruptedBetweenSteps,
			late: []tea.Msg{started("upload", 43.8, "")},
			row:  6,
			want: "  ■  Upload files            0s  interrupted",
		},
		{
			name: "a step started after the interrupt can still finish",
			base: interruptedBetweenSteps,
			late: []tea.Msg{started("upload", 43.8, ""), done("upload", 44.3, "312 files · 12.4 MB")},
			row:  6,
			want: "  ✅ Upload files          0.5s  312 files · 12.4 MB",
		},
		{
			name: "transient reports after the interrupt are ignored",
			base: installInterruptedModel,
			late: []tea.Msg{
				stepDetailMsg{stepEvent{"install", at(140.2)}, "late detail"},
				progressed("install", 140.2, Progress{Items: 1, ItemsTotal: 2}),
				stepNoteMsg{stepEvent{"install", at(140.2)}, "late note"},
			},
			row:  8,
			want: "  ■  Install to dev       48.2s  interrupted",
		},
		{
			name: "a start does not reopen an interrupted step",
			base: installInterruptedModel,
			late: []tea.Msg{started("install", 141, "")},
			row:  8,
			want: "  ■  Install to dev       48.2s  interrupted",
		},
		{
			name: "a settled step is not reopened",
			base: installInterruptedModel,
			late: []tea.Msg{done("publish", 150, "late"), stepFailedMsg{stepEvent{"publish", at(150)}, "late"}},
			row:  7,
			want: "  ✅ Publish version      18.3s  com.acme.crm@1.4.3-dev.5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := send(t, tt.base(t, 80, 24), tt.late...)
			if !m.Interrupted() {
				t.Fatal("the model forgot the interrupt")
			}
			if got := ansi.Strip(m.rowLine(m.rows[tt.row], true)); got != tt.want {
				t.Errorf("row = %q, want %q", got, tt.want)
			}
			if got := m.rows[8].note; tt.row == 8 && !strings.HasPrefix(got, "↻ server resolved") {
				t.Errorf("note = %q, want the retry note kept", got)
			}
		})
	}
}

func TestStepsModel_ReplayIntoFinalModel(t *testing.T) {
	// Once the program has stopped its Send drops messages, so a report
	// made during the grace wait never reaches the model. A runner that
	// keeps every report can replay them all into the model Run returned:
	// the ones it already applied change nothing, and the late ones land.
	reports := append(throughManifest(),
		started("upload", 44.9, ""), done("upload", 73.6, "312 files · 12.4 MB"),
		started("publish", 73.6, ""), done("publish", 91.9, "com.acme.crm@1.4.3-dev.5"),
		started("install", 91.9, "running on the server (can take minutes)"),
		progressed("install", 95, Progress{Items: 1, ItemsTotal: 2}),
		stepNoteMsg{stepEvent{"install", at(95)}, "↻ retrying"},
	)
	late := done("install", 141.5, "1.4.3-dev.5")

	// What Run returns: every report up to the interrupt, none after it.
	final := send(t, newFixtureModel(80, 24), append(reports, InterruptMsg{At: at(140.1)})...)
	replayed := send(t, final, append(reports, late)...)
	// What the model would show had the late report arrived in time.
	direct := send(t, final, late)

	if got, want := replayed.FinalView(), direct.FinalView(); got != want {
		t.Errorf("replayed final view:\n%s\nwant:\n%s", got, want)
	}
	if got, want := plainLines(replayed.FinalView())[10], "  ✅ Install to dev       49.6s  1.4.3-dev.5"; got != want {
		t.Errorf("install row = %q, want %q", got, want)
	}
}

func TestStepsModel_FinishedMsgQuits(t *testing.T) {
	m := send(t, newFixtureModel(80, 24), started("config", 0, ""), done("config", 1, ""))
	next, cmd := m.Update(FinishedMsg{})
	m = next.(StepsModel)
	if !isQuit(cmd) {
		t.Error("FinishedMsg did not return tea.Quit")
	}
	if !m.Finished() || m.Interrupted() {
		t.Errorf("Finished() = %v, Interrupted() = %v; want true, false", m.Finished(), m.Interrupted())
	}
	if _, cmd := m.Update(spinner.TickMsg{Time: at(2)}); cmd != nil {
		t.Error("the spinner kept ticking after quitting")
	}
}

func TestStepsModel_InitStartsWork(t *testing.T) {
	tests := []struct {
		name     string
		withWork bool
	}{
		{"with work", true},
		{"without work", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ran atomic.Bool
			cfg := StepsModelConfig{Plan: deployPlan}
			if tt.withWork {
				cfg.Start = func() tea.Msg {
					ran.Store(true)
					return FinishedMsg{}
				}
			}
			msgs := runCmd(NewStepsModel(cfg).Init())
			var ticked, finished bool
			for _, msg := range msgs {
				switch msg.(type) {
				case spinner.TickMsg:
					ticked = true
				case FinishedMsg:
					finished = true
				}
			}
			if !ticked {
				t.Error("Init did not start the spinner")
			}
			if ran.Load() != tt.withWork || finished != tt.withWork {
				t.Errorf("work ran = %v, finished = %v; want %v", ran.Load(), finished, tt.withWork)
			}
		})
	}
}

func TestStepsModel_LiveFrame(t *testing.T) {
	// The §3 live frame at 80 columns. The bar is sized for the widest the
	// counters get ("12.4/12.4 MB"), so it is one cell shorter than when
	// measured on today's values, and holds still as they grow.
	want := strings.Join([]string{
		"🚀 Deploying apps/com.acme.crm to dev",
		"",
		"  ✅ Load project config   0.4s  com.acme.crm · tenant acme",
		"  ✅ Authenticate          0.9s",
		"  ✅ Bump version          0.2s  1.4.3-dev.5 · app.scl updated",
		"  ✅ Collect files         0.6s  496 files · 18.2 MB",
		"  ✅ Connect               0.3s  devops.acme.simple.dev",
		"  ✅ Compare with server  41.2s  312 to upload · 184 already on server",
		"  ⣾  Upload files         12.4s  ███████░░░░  65%  142/312 files · 8.1/12.4 MB",
		"  ○  Publish version",
		"  ○  Install to dev",
		"",
		"  elapsed 57.3s",
		"",
	}, "\n")
	if got := ansi.Strip(uploadingModel(t, 80, 24).View()); got != want {
		t.Errorf("live frame mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestStepsModel_ViewFitsWidth(t *testing.T) {
	longNote := stepNoteMsg{stepEvent{"upload", at(50)}, strings.Repeat("a very long note ", 20)}
	for _, colour := range []bool{false, true} {
		for _, width := range []int{30, 40, 80, 120, 200} {
			t.Run(fmt.Sprintf("width %d colour %v", width, colour), func(t *testing.T) {
				if colour {
					forceColour(t)
				}
				m := send(t, uploadingModel(t, width, 24), longNote)
				frames := map[string]string{
					"live":  m.View(),
					"final": uploadFailedModel(t, width, 24).FinalView(),
				}
				for name, frame := range frames {
					for i, line := range strings.Split(frame, "\n") {
						if w := ansi.StringWidth(line); w > width-1 {
							t.Errorf("%s line %d is %d cells, want <= %d: %q", name, i, w, width-1, ansi.Strip(line))
						}
						if stripped := ansi.Strip(line); stripped != strings.TrimRight(stripped, " ") {
							t.Errorf("%s line %d is padded to the edge: %q", name, i, stripped)
						}
					}
				}
			})
		}
	}
}

func TestStepsModel_ViewHeightBudget(t *testing.T) {
	note := stepNoteMsg{stepEvent{"upload", at(50)}, "server rejected the saved session; signing in again"}
	models := map[string]func(width, height int) StepsModel{
		"uploading": func(w, h int) StepsModel { return uploadingModel(t, w, h) },
		"uploading with a note": func(w, h int) StepsModel {
			return send(t, uploadingModel(t, w, h), note)
		},
		"first step": func(w, h int) StepsModel {
			return send(t, newFixtureModel(w, h), started("config", 0, "scl-parser: downloading 43%"),
				stepNoteMsg{stepEvent{"config", at(0.1)}, "downloading scl-parser"})
		},
		"last step": func(w, h int) StepsModel {
			return send(t, newFixtureModel(w, h), append(throughManifest(),
				started("upload", 44.9, ""), done("upload", 73.6, ""),
				started("publish", 73.6, ""), done("publish", 91.9, ""),
				started("install", 91.9, "running on the server (can take minutes)"))...)
		},
	}
	for name, build := range models {
		for _, height := range []int{24, 12, 8, 5, 4, 3} {
			t.Run(fmt.Sprintf("%s height %d", name, height), func(t *testing.T) {
				m := build(80, height)
				lines := plainLines(m.View())
				if len(lines) > height-1 {
					t.Errorf("%d lines, want <= %d:\n%s", len(lines), height-1, strings.Join(lines, "\n"))
				}
				if lines[0] != deployHeader {
					t.Errorf("first line = %q, want the header", lines[0])
				}
				running := 0
				for _, l := range lines {
					if strings.HasPrefix(l, "  ⣾") {
						running++
					}
				}
				if running != 1 {
					t.Errorf("running rows = %d, want 1:\n%s", running, strings.Join(lines, "\n"))
				}
			})
		}
	}
}

func TestStepsModel_ViewHeightFolding(t *testing.T) {
	tests := []struct {
		height int
		want   []string
	}{
		{
			// 13 lines fit in 23: nothing folds.
			height: 24,
			want:   nil,
		},
		{
			// 11 lines available: only the three oldest steps fold.
			height: 12,
			want: []string{
				deployHeader,
				"",
				"  ✅ 3 steps done          1.5s",
				"  ✅ Collect files         0.6s  496 files · 18.2 MB",
				"  ✅ Connect               0.3s  devops.acme.simple.dev",
				"  ✅ Compare with server  41.2s  312 to upload · 184 already on server",
				"  ⣾  Upload files         12.4s  ███████░░░░  65%  142/312 files · 8.1/12.4 MB",
				"  ○  Publish version",
				"  ○  Install to dev",
				"",
				"  elapsed 57.3s",
			},
		},
		{
			height: 8,
			want: []string{
				deployHeader,
				"",
				"  ✅ 6 steps done         43.6s",
				"  ⣾  Upload files         12.4s  ███████░░░░  65%  142/312 files · 8.1/12.4 MB",
				"  ○  2 more steps",
				"",
				"  elapsed 57.3s",
			},
		},
		{
			height: 5,
			want: []string{
				deployHeader,
				"  ✅ 6 steps done         43.6s",
				"  ⣾  Upload files         12.4s  ███████░░░░  65%  142/312 files · 8.1/12.4 MB",
				"  ○  2 more steps",
			},
		},
		{
			height: 3,
			want: []string{
				deployHeader,
				"  ⣾  Upload files         12.4s  ███████░░░░  65%  142/312 files · 8.1/12.4 MB",
			},
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("height %d", tt.height), func(t *testing.T) {
			got := plainLines(uploadingModel(t, 80, tt.height).View())
			if tt.want == nil {
				if len(got) != 13 {
					t.Errorf("got %d lines, want the full 13", len(got))
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("frame mismatch\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestStepsModel_FoldingSingularLabels(t *testing.T) {
	plan := []Step{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"}, {ID: "c", Title: "Gamma"}}
	m := NewStepsModel(StepsModelConfig{Header: "h", Plan: plan, Height: 3})
	m.began, m.now = epoch, epoch
	m = send(t, m, started("a", 0, ""), done("a", 1, ""), started("b", 1, ""))
	// Folding needs two steps, so single runs stay as rows until the
	// shrink steps drop them.
	lines := plainLines(m.View())
	want := []string{"h", "  ⣾  Beta      0s"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("frame = %q, want %q", lines, want)
	}
	if got := countLabel(1, "step done", "steps done"); got != "1 step done" {
		t.Errorf("countLabel(1) = %q", got)
	}
}

func TestStepsModel_ProgressRowLayout(t *testing.T) {
	upload := Progress{Items: 142, ItemsTotal: 312, Bytes: 8_100_000, BytesTotal: 12_400_000, Noun: "files"}
	tests := []struct {
		name     string
		width    int
		progress Progress
		want     string // the row after the duration column
	}{
		{"bar, items and bytes at 80", 80, upload, "███████░░░░  65%  142/312 files · 8.1/12.4 MB"},
		{"bar caps at 24 cells", 120, upload, strings.Repeat("█", 15) + strings.Repeat("░", 9) + "  65%  142/312 files · 8.1/12.4 MB"},
		{"bar and items at 70", 70, upload, "██████████░░░░░░  65%  142/312 files"},
		{"a shorter bar before dropping it at 64", 64, upload, "██████░░░░  65%  142/312 files"},
		{"items without the bar at 63", 63, upload, "65%  142/312 files"},
		// Short counters: dropping the bar makes room for bytes.
		{"items and bytes without the bar at 52", 52, Progress{Items: 1, ItemsTotal: 2, Bytes: 5, BytesTotal: 9}, "55%  1/2 · 5/9 B"},
		{"items without the bar at 60", 60, upload, "65%  142/312 files"},
		{"percentage alone at 40", 40, upload, "65%"},
		{"items only", 80, Progress{Items: 142, ItemsTotal: 496, Noun: "files"}, "██████░░░░░░░░░░░░░░░░░░  28%  142/496 files"},
		{"no noun", 80, Progress{Items: 1, ItemsTotal: 4}, "██████░░░░░░░░░░░░░░░░░░  25%  1/4"},
		{"bytes only", 80, Progress{Bytes: 500, BytesTotal: 1000}, "████████████░░░░░░░░░░░░  50%  0.5/1.0 kB"},
		{"bytes give way before the bar at 60", 60, Progress{Bytes: 500, BytesTotal: 1000}, strings.Repeat("█", 10) + strings.Repeat("░", 11) + "  50%"},
		{"no totals shows counters only", 80, Progress{Items: 7, Noun: "files"}, "7 files"},
		{"nothing to show", 80, Progress{Noun: "files"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := send(t, newFixtureModel(tt.width, 24),
				started("upload", 0, ""), progressed("upload", 12.4, tt.progress))
			row := ansi.Strip(m.rowLine(m.rows[6], false))
			prefix := "  ⣾  Upload files         12.4s"
			if !strings.HasPrefix(row, prefix) {
				t.Fatalf("row = %q, want prefix %q", row, prefix)
			}
			if got := strings.TrimPrefix(strings.TrimPrefix(row, prefix), "  "); got != tt.want {
				t.Errorf("progress = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProgressDetailHoldsStillAsCountsGrow(t *testing.T) {
	// The bar's width is decided by the widest counters, so it does not
	// jump as 9 files becomes 10 and 99 becomes 100.
	var widths []int
	for _, items := range []int64{9, 10, 99, 100, 312} {
		p := Progress{Items: items, ItemsTotal: 312, Bytes: items * 40_000, BytesTotal: 12_480_000, Noun: "files"}
		detail := ansi.Strip(progressDetail(p, 46))
		widths = append(widths, strings.Count(detail, "█")+strings.Count(detail, "░"))
	}
	for _, w := range widths[1:] {
		if w != widths[0] {
			t.Fatalf("bar widths = %v, want all equal", widths)
		}
	}
}

func TestStepsModel_RowStates(t *testing.T) {
	tests := []struct {
		name  string
		msgs  []tea.Msg
		final bool
		want  string
	}{
		{"pending", nil, false, "  ○  Upload files"},
		{"running with detail", []tea.Msg{started("upload", 0, ""), stepDetailMsg{stepEvent{"upload", at(1)}, "sending"}}, false, "  ⣾  Upload files            1s  sending"},
		// Progress with nothing to draw must not blank the detail.
		{"progress without counts keeps the detail", []tea.Msg{started("upload", 0, "preparing"), progressed("upload", 1, Progress{Noun: "files"})}, false, "  ⣾  Upload files            1s  preparing"},
		{"bytes without a total keep the detail", []tea.Msg{started("upload", 0, "preparing"), progressed("upload", 1, Progress{Bytes: 500})}, false, "  ⣾  Upload files            1s  preparing"},
		{"done without detail", []tea.Msg{started("upload", 0, ""), done("upload", 0.9, "")}, false, "  ✅ Upload files          0.9s"},
		{"skipped", []tea.Msg{stepSkippedMsg{stepEvent{"upload", at(1)}, "all files already on server"}}, false, "  –  Upload files                all files already on server"},
		{"skipped without reason", []tea.Msg{stepSkippedMsg{stepEvent{"upload", at(1)}, ""}}, false, "  –  Upload files"},
		{"failed", []tea.Msg{started("upload", 0, ""), stepFailedMsg{stepEvent{"upload", at(4.2)}, "boom"}}, false, "  ❌ Upload files          4.2s  boom"},
		{"running in the final view was cut short", []tea.Msg{started("upload", 0, ""), spinner.TickMsg{Time: at(3)}}, true, "  ■  Upload files            3s  interrupted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := send(t, newFixtureModel(80, 24), tt.msgs...)
			if got := ansi.Strip(m.rowLine(m.rows[6], tt.final)); got != tt.want {
				t.Errorf("row = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStepsModel_ViewEmptyWhenQuitting(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"finished", FinishedMsg{}},
		{"interrupt message", InterruptMsg{At: at(1)}},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := send(t, uploadingModel(t, 80, 24), tt.msg)
			if got := m.View(); got != "" {
				t.Errorf("View() = %q, want empty once quitting", got)
			}
		})
	}
}

func TestStepsModel_ViewIsPure(t *testing.T) {
	m := uploadingModel(t, 80, 24)
	before := m.now
	first, second := m.View(), m.View()
	if first != second {
		t.Error("View() differs between calls on the same model")
	}
	final1, final2 := m.FinalView(), m.FinalView()
	if final1 != final2 {
		t.Error("FinalView() differs between calls on the same model")
	}
	if !m.now.Equal(before) || m.rows[6].state != stateRunning {
		t.Error("rendering changed the model")
	}
}

func TestStepsModel_FinalView(t *testing.T) {
	tests := []struct {
		name string
		m    StepsModel
		want []string
	}{
		{
			name: "success",
			m:    deployedModel(t, 80, 5),
			want: []string{
				deployHeader,
				"",
				"  ✅ Load project config   0.4s  com.acme.crm · tenant acme",
				"  ✅ Authenticate          0.9s",
				"  ✅ Bump version          0.2s  1.4.3-dev.5 · app.scl updated",
				"  ✅ Collect files         0.6s  496 files · 18.2 MB",
				"  ✅ Connect               0.3s  devops.acme.simple.dev",
				"  ✅ Compare with server  41.2s  312 to upload · 184 already on server",
				"  ✅ Upload files         28.7s  312 files · 12.4 MB",
				"  ✅ Publish version      18.3s  com.acme.crm@1.4.3-dev.5",
				"  ✅ Install to dev       1m52s  1.4.3-dev.5",
				"",
			},
		},
		{
			name: "failed part-way",
			m:    uploadFailedModel(t, 80, 5),
			want: []string{
				deployHeader,
				"",
				"  ✅ Load project config   0.4s  com.acme.crm · tenant acme",
				"  ✅ Authenticate          0.9s",
				"  ✅ Bump version          0.2s  1.4.3-dev.5 · app.scl updated",
				"  ✅ Collect files         0.6s  496 files · 18.2 MB",
				"  ✅ Connect               0.3s  devops.acme.simple.dev",
				"  ✅ Compare with server  41.2s  312 to upload · 184 already on server",
				"  ❌ Upload files          4.2s  upload actions/crm/build/release.wasm: connec…",
				"  ○  Publish version",
				"  ○  Install to dev",
				"",
			},
		},
		{
			name: "interrupted, with a note",
			m:    installInterruptedModel(t, 80, 5),
			want: []string{
				deployHeader,
				"",
				"  ✅ Load project config   0.4s  com.acme.crm · tenant acme",
				"  ✅ Authenticate          0.9s",
				"  ✅ Bump version          0.2s  1.4.3-dev.5 · app.scl updated",
				"  ✅ Collect files         0.6s  496 files · 18.2 MB",
				"  ✅ Connect               0.3s  devops.acme.simple.dev",
				"  ✅ Compare with server  41.2s  312 to upload · 184 already on server",
				"  ✅ Upload files         28.7s  312 files · 12.4 MB",
				"  ✅ Publish version      18.3s  com.acme.crm@1.4.3-dev.5",
				"  ■  Install to dev       48.2s  interrupted",
				"     ↻ server resolved 1.4.3-dev.4; retrying install of 1.4.3-dev.5 in 5s (2/4)",
				"",
			},
		},
		{
			name: "skipped install",
			m: send(t, newFixtureModel(80, 5), append(throughManifest(),
				stepSkippedMsg{stepEvent{"upload", at(44)}, "all files already on server"},
				started("publish", 44, ""), done("publish", 60, "com.acme.crm@1.4.3-dev.5"),
				stepSkippedMsg{stepEvent{"install", at(60)}, "--no-install"},
				FinishedMsg{})...),
			want: []string{
				deployHeader,
				"",
				"  ✅ Load project config   0.4s  com.acme.crm · tenant acme",
				"  ✅ Authenticate          0.9s",
				"  ✅ Bump version          0.2s  1.4.3-dev.5 · app.scl updated",
				"  ✅ Collect files         0.6s  496 files · 18.2 MB",
				"  ✅ Connect               0.3s  devops.acme.simple.dev",
				"  ✅ Compare with server  41.2s  312 to upload · 184 already on server",
				"  –  Upload files                all files already on server",
				"  ✅ Publish version        16s  com.acme.crm@1.4.3-dev.5",
				"  –  Install to dev              --no-install",
				"",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			final := tt.m.FinalView()
			if !strings.HasSuffix(final, "\n") {
				t.Error("FinalView does not end in a newline")
			}
			if strings.ContainsAny(ansi.Strip(final), "⣾⣽⣻⢿⡿⣟⣯⣷") || strings.Contains(final, "elapsed") {
				t.Error("FinalView shows a spinner or the footer")
			}
			if got := plainLines(final); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("final view mismatch\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestStepsProgram_EndToEnd(t *testing.T) {
	var p *tea.Program
	plan := deployPlan[:3]
	m := NewStepsModel(StepsModelConfig{
		Header: deployHeader,
		Plan:   plan,
		Start: func() tea.Msg {
			r := NewTTYReporter(p.Send)
			r.Start("config", "")
			r.Note("config", "downloading scl-parser")
			r.Done("config", "com.acme.crm · tenant acme")
			r.Start("auth", "")
			r.Detail("auth", "signing in")
			r.Done("auth", "")
			r.Start("version", "")
			r.Progress("version", Progress{Items: 1, ItemsTotal: 2})
			r.Fail("version", errors.New("app.scl: syntax error\nat line 3"))
			return FinishedMsg{}
		},
	})
	var out bytes.Buffer
	p = tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(&out), tea.WithoutSignalHandler())

	result := make(chan tea.Model, 1)
	errs := make(chan error, 1)
	go func() {
		final, err := p.Run()
		result <- final
		errs <- err
	}()
	var final tea.Model
	select {
	case final = <-result:
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("the program did not quit after the work finished")
	}
	if err := <-errs; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	fm, ok := final.(StepsModel)
	if !ok {
		t.Fatalf("final model is %T", final)
	}
	if !fm.Finished() || fm.Interrupted() {
		t.Errorf("Finished() = %v, Interrupted() = %v; want true, false", fm.Finished(), fm.Interrupted())
	}
	got := plainLines(fm.FinalView())
	want := []string{
		deployHeader,
		"",
		"  ✅ Load project config",
		"     downloading scl-parser",
		"  ✅ Authenticate",
		"  ❌ Bump version",
		"",
	}
	if len(got) != len(want) {
		t.Fatalf("final view:\n%s", strings.Join(got, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("line %d = %q, want prefix %q", i, got[i], want[i])
		}
	}
	if !strings.HasSuffix(got[5], "app.scl: syntax error") {
		t.Errorf("failed row = %q, want the error's first line", got[5])
	}
	if out.Len() == 0 {
		t.Error("the program drew nothing")
	}
}

// isQuit reports whether cmd is tea.Quit.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// runCmd executes cmd, expanding batches, and returns the messages produced.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, runCmd(c)...)
	}
	return msgs
}

// forceColour makes lipgloss emit 256-colour escapes for the rest of the
// test, so width checks cover styled output. 1 is termenv.ANSI256.
func forceColour(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(1)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

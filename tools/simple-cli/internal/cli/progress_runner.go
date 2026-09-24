package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"simple-cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// progressMode is how a command shows its steps.
type progressMode int

const (
	// progressNone shows nothing: under --json, stdout carries only the JSON
	// document.
	progressNone progressMode = iota
	// progressPlain writes one line per event, for logs, pipes and CI.
	progressPlain
	// progressTTY draws a frame that repaints in place.
	progressTTY
)

// progressModeFor picks how to show progress on out.
func progressModeFor(out io.Writer, jsonMode bool) progressMode {
	return pickProgressMode(jsonMode, outputIsTerminal(out), os.Getenv("TERM"), os.Getenv("CI"))
}

// outputIsTerminal reports whether out writes to a terminal, by the same
// test build applies to stdin.
func outputIsTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && progressInputIsTerminal(f)
}

// pickProgressMode chooses the mode from what progressModeFor observed.
// Bubble Tea checks neither TERM nor CI, so a CI job whose output is a PTY
// would otherwise get a log full of repaint escapes.
func pickProgressMode(jsonMode, outIsTerminal bool, termEnv, ciEnv string) progressMode {
	switch {
	case jsonMode:
		return progressNone
	case outIsTerminal && termEnv != "dumb" && !ciEnabled(ciEnv):
		return progressTTY
	default:
		return progressPlain
	}
}

// ciEnabled reports whether the CI variable says this is a CI run. CI
// systems set it to "true" or "1"; unset, "false" and "0" mean it is not.
func ciEnabled(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "0" && !strings.EqualFold(value, "false")
}

const (
	// interruptGrace is how long an interrupted run waits for the work to
	// return. It lets the deferred client.Close run and a BumpVersion write
	// that is under way finish; a worker that takes longer is abandoned.
	interruptGrace = 2 * time.Second
	// slowStopAfter is how long into the grace wait the live view says it is
	// waiting. The frame is gone by then, and a step that does not watch
	// the context (hashing files, running the scl-parser) would otherwise
	// leave the screen blank for up to interruptGrace. Work that stops
	// promptly returns well within it, so the notice does not flicker.
	slowStopAfter = 250 * time.Millisecond
)

// errInterrupted is wrapped by the error of a run that SIGINT, SIGTERM or
// ctrl+c stopped.
var errInterrupted = errors.New("interrupted")

// stepWork is the work a stepRun shows: it reports its steps to steps and
// must stop soon after ctx is cancelled.
type stepWork func(ctx context.Context, steps ui.StepReporter) error

// runnerDeps are the terminal and the signals a stepRun works with. Tests
// replace them; defaultRunnerDeps wires the real ones.
type runnerDeps struct {
	// stdin decides whether the live view reads keys: only a terminal gets
	// raw mode.
	stdin *os.File
	// notifySignals derives a context that SIGINT and SIGTERM cancel; nil
	// means signal.NotifyContext. Tests pass context.WithCancel: a
	// signal.Notify inside a synctest bubble never lets the bubble end.
	notifySignals func(context.Context) (context.Context, context.CancelFunc)
	// runProgram runs the TTY program; nil means p.Run. Tests stand in for a
	// terminal that fails, or send it keys.
	runProgram func(p *tea.Program) (tea.Model, error)
}

func defaultRunnerDeps() runnerDeps {
	return runnerDeps{stdin: os.Stdin}
}

// stepRun shows a plan's progress while its work runs, and turns SIGINT,
// SIGTERM and ctrl+c into a graceful stop in every mode.
type stepRun struct {
	runner runnerDeps
	out    io.Writer
	mode   progressMode
	verb   string // "deploy" or "install", for the interrupt error
	header string
	plan   []ui.Step
}

// stepRunResult is how a run ended. A nil err means the work returned nil,
// so whatever it wrote may be read. After an interrupt, a work that outlives
// the grace period is abandoned and may still be writing.
type stepRunResult struct {
	// interrupted reports that an interrupt decided the outcome; err then
	// says where the run stopped.
	interrupted bool
	err         error
}

// execute runs work while showing its progress on r.out, and returns how it
// ended. The outcome comes from the work's result, never from the display:
// work that returned nil succeeded even if an interrupt arrived at the same
// moment.
func (r stepRun) execute(ctx context.Context, work stepWork) stepRunResult {
	base, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	notify := r.runner.notifySignals
	if notify == nil {
		notify = notifyInterrupts
	}
	workCtx, stopSignals := notify(base)
	defer stopSignals()

	if r.mode == progressTTY {
		if result, shown := r.executeTTY(workCtx, cancel, stopSignals, work); shown {
			return result
		}
		// The terminal could not be set up, and the work never started:
		// run it with plain lines instead.
		return r.executeLines(workCtx, stopSignals, work, progressPlain)
	}
	return r.executeLines(workCtx, stopSignals, work, r.mode)
}

func notifyInterrupts(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

// executeLines runs work with plain lines (progressPlain) or with no output
// at all (progressNone).
func (r stepRun) executeLines(ctx context.Context, stopSignals func(), work stepWork, mode progressMode) stepRunResult {
	var lines *ui.LineReporter
	var inner ui.StepReporter = ui.NopReporter{}
	if mode == progressPlain {
		_, _ = fmt.Fprintln(r.out, r.header)
		lines = ui.NewLineReporter(r.out, r.plan)
		inner = lines
	}
	// The interrupt is timed when it arrives, not when the work has wound
	// down: the error and the ■ lines then name the same moment the live
	// view would, however long the grace wait took.
	var clock interruptClock
	stopTiming := context.AfterFunc(ctx, func() { clock.mark(time.Now()) })
	defer stopTiming()

	steps := newStepTracker(inner, r.plan)
	results := make(chan error, 1)
	go func() { results <- runWork(ctx, steps, work) }()

	finished, err := awaitWork(ctx, results, stopSignals, nil)
	steps.freeze()
	at := clock.mark(time.Now())
	result := r.outcome(ctx, steps, finished, err, at)
	if lines != nil {
		if result.interrupted {
			lines.Interrupt(at)
		} else {
			lines.Close()
		}
	}
	return result
}

// executeTTY runs work under a Bubble Tea program. shown is false when the
// program failed before the work started; the work then never runs here,
// and the caller runs it without the display.
func (r stepRun) executeTTY(ctx context.Context, cancel context.CancelCauseFunc, stopSignals func(), work stepWork) (result stepRunResult, shown bool) {
	var (
		p     *tea.Program
		sent  sentMessages
		clock interruptClock
		// claimed goes to whichever comes first: the work starting, or the
		// runner giving up on a program that never started it. Deciding it
		// once means a work cannot start after the runner has moved on.
		claimed atomic.Bool
	)
	steps := newStepTracker(ui.NewTTYReporter(func(msg tea.Msg) {
		sent.add(msg)
		p.Send(msg)
	}), r.plan)
	results := make(chan error, 1)
	start := func() tea.Msg {
		if claimed.CompareAndSwap(false, true) {
			results <- runWork(ctx, steps, work)
		}
		if ctx.Err() != nil {
			// Work cut short by an interrupt returns at once, and its
			// FinishedMsg can reach the model before the forwarded
			// InterruptMsg. The model would then draw the step it left
			// running with the time of its last spinner tick, not the
			// interrupt's. Both messages carry the one recorded moment, so
			// the frame and the error agree whichever arrives first.
			return ui.InterruptMsg{At: clock.mark(time.Now())}
		}
		return ui.FinishedMsg{}
	}

	options := []tea.ProgramOption{tea.WithOutput(r.out), tea.WithoutSignalHandler()}
	if !progressInputIsTerminal(r.runner.stdin) {
		options = append(options, tea.WithInput(nil))
	}
	width, height := terminalSize(r.out)
	// The footer's elapsed time counts from NewStepsModel, so the model is
	// built just before the program runs.
	p = tea.NewProgram(ui.NewStepsModel(ui.StepsModelConfig{
		Header: r.header,
		Plan:   r.plan,
		Width:  width,
		Height: height,
		Start:  start,
		OnInterrupt: func(at time.Time) {
			// The model ended the running step at at; the error must say the
			// same.
			clock.mark(at)
			cancel(errInterrupted)
		},
	}), options...)
	// Signals end the program the same way ctrl+c does in raw mode.
	stopForwarding := context.AfterFunc(ctx, func() {
		stopSignals()
		p.Send(ui.InterruptMsg{At: clock.mark(time.Now())})
	})
	runProgram := r.runner.runProgram
	if runProgram == nil {
		runProgram = (*tea.Program).Run
	}
	final, runErr := runProgram(p)
	stopForwarding()

	if claimed.CompareAndSwap(false, true) {
		if runErr != nil {
			return stepRunResult{}, false
		}
		// An interrupt stopped the program before the work began.
		steps.freeze()
		r.printFinalView(final, &sent)
		return r.outcome(ctx, steps, false, nil, clock.mark(time.Now())), true
	}

	var slow func()
	noticed := false
	if runErr != nil {
		// The display broke mid-run (a panic in View or Update). The work is
		// in flight, and abandoning it could leave a deploy half done.
		_, _ = fmt.Fprintf(r.out, "progress display stopped (%v); waiting for the %s to finish\n", runErr, r.verb)
	} else {
		slow = func() {
			noticed = true
			_, _ = fmt.Fprint(r.out, r.stoppingNotice(steps))
		}
	}
	finished, err := awaitWork(ctx, results, stopSignals, slow)
	steps.freeze()
	result = r.outcome(ctx, steps, finished, err, clock.mark(time.Now()))
	if runErr == nil {
		if noticed {
			// The notice has no newline, so the final frame replaces it.
			_, _ = fmt.Fprint(r.out, "\r"+ansi.EraseEntireLine)
		}
		r.printFinalView(final, &sent)
	}
	return result, true
}

// stoppingNotice is the line shown while an interrupted run waits for its
// work to stop, once the live frame has gone. It ends without a newline and
// fits on one row, so a carriage return and an erase remove it.
func (r stepRun) stoppingNotice(steps *stepTracker) string {
	what := "the " + r.verb
	if step, running, _ := steps.position(time.Now()); running {
		what = strconv.Quote(step.Title)
	}
	notice := fmt.Sprintf("Waiting for %s to stop (Ctrl+C again to quit now)", what)
	width, _ := terminalSize(r.out)
	if width <= 0 {
		width = 80
	}
	return ansi.Truncate(notice, width-1, "…")
}

// printFinalView prints the program's last frame after it has torn down,
// where the renderer can neither crop nor erase it. Once the program stops,
// Send drops messages, so reports made during the grace wait never reached
// the model. Every report is replayed into the model Run returned: the
// model ignores the ones it already applied, and records how a step ended
// after the interrupt.
func (r stepRun) printFinalView(final tea.Model, sent *sentMessages) {
	m, ok := final.(ui.StepsModel)
	if !ok {
		return
	}
	for _, msg := range sent.all() {
		if next, ok := updateModel(m, msg); ok {
			m = next
		}
	}
	_, _ = fmt.Fprint(r.out, m.FinalView())
}

func updateModel(m ui.StepsModel, msg tea.Msg) (ui.StepsModel, bool) {
	next, _ := m.Update(msg)
	sm, ok := next.(ui.StepsModel)
	return sm, ok
}

// outcome decides how the run ended. at is when the interrupt is said to
// have stopped the running step, matching what the display shows.
func (r stepRun) outcome(ctx context.Context, steps *stepTracker, finished bool, err error, at time.Time) stepRunResult {
	switch {
	case finished && err == nil:
		return stepRunResult{}
	case finished && (ctx.Err() == nil || steps.hasFailed()):
		// A step failed on its own before any interrupt: runStep reports a
		// failure only while the context is live. Its error is the story.
		return stepRunResult{err: err}
	default:
		return stepRunResult{interrupted: true, err: r.interruptError(steps, at)}
	}
}

// interruptError says where the interrupt stopped the run:
// `deploy interrupted during "Upload files" after 4.2s`, or
// `deploy interrupted before "Connect"` when no step was running.
func (r stepRun) interruptError(steps *stepTracker, at time.Time) error {
	step, running, elapsed := steps.position(at)
	switch {
	case running:
		return fmt.Errorf("%s %w during %q after %s", r.verb, errInterrupted, step.Title, ui.FormatDuration(elapsed))
	case step.ID != "":
		return fmt.Errorf("%s %w before %q", r.verb, errInterrupted, step.Title)
	default:
		return fmt.Errorf("%s %w", r.verb, errInterrupted)
	}
}

// awaitWork waits for the work's result. Once ctx is cancelled it stops
// listening for signals, so a second ctrl+c kills the process, and gives
// the work interruptGrace to return before abandoning it. slow, when set,
// is called once if the work is still running slowStopAfter into that wait.
func awaitWork(ctx context.Context, results <-chan error, stopSignals func(), slow func()) (finished bool, err error) {
	select {
	case err := <-results:
		return true, err
	case <-ctx.Done():
	}
	stopSignals()

	grace := time.NewTimer(interruptGrace)
	defer grace.Stop()
	var slowTimer <-chan time.Time
	if slow != nil {
		t := time.NewTimer(slowStopAfter)
		defer t.Stop()
		slowTimer = t.C
	}
	for {
		select {
		case err := <-results:
			return true, err
		case <-grace.C:
			return false, nil
		case <-slowTimer:
			slowTimer = nil
			slow()
		}
	}
}

// runWork runs work, turning a panic into a *workPanic error so a bug in
// one step fails the run instead of killing the process mid-deploy.
func runWork(ctx context.Context, steps *stepTracker, work stepWork) (err error) {
	defer func() {
		if value := recover(); value != nil {
			failure := &workPanic{value: value, stack: debug.Stack()}
			steps.failRunning(failure)
			err = failure
		}
	}()
	return work(ctx, steps)
}

// workPanic is a panic recovered from the work.
type workPanic struct {
	value any
	stack []byte
}

func (p *workPanic) Error() string {
	return fmt.Sprintf("internal error: %v\n\n%s", p.value, p.stack)
}

// runStep reports one step around fn: Start with detail, then Done with the
// detail fn returns, or Fail with its error. A context that is already
// cancelled returns before Start. A step whose fn fails after the context
// was cancelled is left running, so the display marks it interrupted
// rather than failed: the failure is the interrupt's doing.
func runStep(ctx context.Context, steps ui.StepReporter, id ui.StepID, detail string, fn func() (string, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	steps.Start(id, detail)
	done, err := fn()
	if err != nil {
		if ctx.Err() == nil {
			steps.Fail(id, err)
		}
		return err
	}
	steps.Done(id, done)
	return nil
}

// skipStep reports a step that does not need to run.
func skipStep(ctx context.Context, steps ui.StepReporter, id ui.StepID, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	steps.Skip(id, reason)
	return nil
}

// stepTracker forwards reports to the display and remembers which step is
// running and since when, so an interrupt can say where it stopped the run.
// Once frozen it forwards nothing more: an abandoned worker cannot change
// the display after the runner has judged the outcome from it.
type stepTracker struct {
	inner ui.StepReporter
	plan  []ui.Step

	// gate is held for reading while a report is recorded and forwarded,
	// and for writing by freeze, so no report is half applied when freeze
	// returns. Reports do not wait on each other.
	gate   sync.RWMutex
	frozen bool

	mu      sync.Mutex // guards the fields below
	running ui.StepID
	since   time.Time
	ended   map[ui.StepID]bool
	failed  bool
}

var _ ui.StepReporter = (*stepTracker)(nil)

func newStepTracker(inner ui.StepReporter, plan []ui.Step) *stepTracker {
	return &stepTracker{inner: inner, plan: plan, ended: make(map[ui.StepID]bool)}
}

// forward runs report unless the tracker is frozen.
func (t *stepTracker) forward(report func()) {
	t.gate.RLock()
	defer t.gate.RUnlock()
	if !t.frozen {
		report()
	}
}

// freeze stops every later report, and waits for the ones in progress.
func (t *stepTracker) freeze() {
	t.gate.Lock()
	t.frozen = true
	t.gate.Unlock()
}

// Start implements ui.StepReporter.
func (t *stepTracker) Start(id ui.StepID, detail string) {
	t.forward(func() {
		t.mu.Lock()
		t.running, t.since = id, time.Now()
		delete(t.ended, id)
		t.mu.Unlock()
		t.inner.Start(id, detail)
	})
}

// Detail implements ui.StepReporter.
func (t *stepTracker) Detail(id ui.StepID, detail string) {
	t.forward(func() { t.inner.Detail(id, detail) })
}

// Note implements ui.StepReporter.
func (t *stepTracker) Note(id ui.StepID, text string) {
	t.forward(func() { t.inner.Note(id, text) })
}

// Progress implements ui.StepReporter.
func (t *stepTracker) Progress(id ui.StepID, p ui.Progress) {
	t.forward(func() { t.inner.Progress(id, p) })
}

// Done implements ui.StepReporter.
func (t *stepTracker) Done(id ui.StepID, detail string) {
	t.forward(func() {
		t.end(id, false)
		t.inner.Done(id, detail)
	})
}

// Skip implements ui.StepReporter.
func (t *stepTracker) Skip(id ui.StepID, reason string) {
	t.forward(func() {
		t.end(id, false)
		t.inner.Skip(id, reason)
	})
}

// Fail implements ui.StepReporter.
func (t *stepTracker) Fail(id ui.StepID, err error) {
	t.forward(func() {
		t.end(id, true)
		t.inner.Fail(id, err)
	})
}

func (t *stepTracker) end(id ui.StepID, failed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ended[id] = true
	if t.running == id {
		t.running = ""
	}
	t.failed = t.failed || failed
}

// failRunning fails the running step with err, and marks the run failed
// even when no step is running.
func (t *stepTracker) failRunning(err error) {
	t.mu.Lock()
	id := t.running
	t.failed = true
	t.mu.Unlock()
	if id != "" {
		t.Fail(id, err)
	}
}

// hasFailed reports whether a step failed.
func (t *stepTracker) hasFailed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failed
}

// position returns the running step and how long it had run at at, or,
// when none is running, the first step that has not ended.
func (t *stepTracker) position(at time.Time) (step ui.Step, running bool, elapsed time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running != "" {
		i := slices.IndexFunc(t.plan, func(s ui.Step) bool { return s.ID == t.running })
		if i >= 0 {
			return t.plan[i], true, max(at.Sub(t.since), 0)
		}
	}
	for _, s := range t.plan {
		if !t.ended[s.ID] {
			return s, false, 0
		}
	}
	return ui.Step{}, false, 0
}

// sentMessages records every message a TTYReporter sends, for the final
// replay.
type sentMessages struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (s *sentMessages) add(msg tea.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
}

func (s *sentMessages) all() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.msgs)
}

// interruptClock keeps the moment the interrupt was first seen, so the frame
// and the error agree on how long the interrupted step ran.
type interruptClock struct {
	mu sync.Mutex
	at time.Time
}

// mark records now unless a moment is already recorded, and returns the
// recorded one.
func (c *interruptClock) mark(now time.Time) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() {
		c.at = now
	}
	return c.at
}

// terminalSize seeds the frame's size: the program draws its first frame
// before the first WindowSizeMsg arrives. Zero means the model's 80x24.
func terminalSize(out io.Writer) (width, height int) {
	f, ok := out.(*os.File)
	if !ok {
		return 0, 0
	}
	width, height, err := term.GetSize(f.Fd())
	if err != nil {
		return 0, 0
	}
	return width, height
}

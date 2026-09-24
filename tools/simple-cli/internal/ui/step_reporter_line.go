package ui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	// lineTick is how often a LineReporter checks running steps.
	lineTick = time.Second
	// lineProgressEvery spaces progress lines out. A step's counters are
	// printed only when they changed and at least this long has passed since
	// that step's last line, so short steps print none and long uploads
	// print a handful.
	lineProgressEvery = 10 * time.Second
	// lineHeartbeatAfter is how long a step may print nothing before a
	// "still running" line: CI systems kill jobs that stay silent too long.
	lineHeartbeatAfter = 30 * time.Second

	// lineIndent lines sub-lines up under the step title after "[n/N] ".
	lineIndent = "      "
)

// lineStep is the state of one running step.
type lineStep struct {
	started  time.Time
	lastLine time.Time
	progress Progress
	printed  Progress
}

// LineReporter writes a plain transcript, one line per event, for output
// that is not a terminal: CI logs, pipes and files. It writes no escape
// sequences and never rewrites a line, so the log reads the same in any
// viewer. Steps are marked ✓ done, ✗ failed, - skipped and ■ interrupted.
//
// Detail is ignored: it is transient and would flood a log (a download
// reports its percentage about a hundred times). Progress is only stored;
// a ticker prints it at most every lineProgressEvery, and prints a heartbeat
// for a step that has been silent for lineHeartbeatAfter.
//
// A LineReporter is safe for concurrent use. After Close every call is a
// no-op, so a worker abandoned after an interrupt cannot print into the
// command's own closing lines.
type LineReporter struct {
	out     io.Writer
	plan    []Step
	index   map[StepID]int
	mu      sync.Mutex
	closed  bool
	running map[StepID]*lineStep
	stop    chan struct{}
	wg      sync.WaitGroup
}

var _ StepReporter = (*LineReporter)(nil)

// NewLineReporter returns a reporter that writes to out and starts its
// ticker. The caller must call Close (or Interrupt) to stop the ticker.
func NewLineReporter(out io.Writer, plan []Step) *LineReporter {
	r := &LineReporter{
		out:     out,
		plan:    make([]Step, len(plan)),
		index:   make(map[StepID]int, len(plan)),
		running: make(map[StepID]*lineStep),
		stop:    make(chan struct{}),
	}
	for i, step := range plan {
		r.plan[i] = Step{ID: step.ID, Title: sanitizeLine(step.Title)}
		if _, dup := r.index[step.ID]; !dup {
			r.index[step.ID] = i
		}
	}
	r.wg.Go(r.tickLoop)
	return r
}

// Start implements StepReporter. The detail is transient and not printed.
func (r *LineReporter) Start(id StepID, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.lookup(id)
	if !ok {
		return
	}
	now := time.Now()
	r.running[id] = &lineStep{started: now, lastLine: now}
	r.printf("%s %s\n", r.prefix(i), r.plan[i].Title)
}

// Detail implements StepReporter. Plain output ignores transient status.
func (r *LineReporter) Detail(StepID, string) {}

// Note implements StepReporter. Notes are events worth keeping, so each is
// printed on its own indented line.
func (r *LineReporter) Note(id StepID, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.lookup(id); !ok {
		return
	}
	text = sanitizeLine(text)
	if text == "" {
		return
	}
	r.printf("%s%s\n", lineIndent, text)
	if s, running := r.running[id]; running {
		s.lastLine = time.Now()
	}
}

// Progress implements StepReporter. The value is stored, keeping the
// per-field maximum, and the ticker decides when to print it.
func (r *LineReporter) Progress(id StepID, p Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	s, running := r.running[id]
	if !running {
		return
	}
	s.progress = mergeProgress(s.progress, p)
}

// Done implements StepReporter.
func (r *LineReporter) Done(id StepID, detail string) {
	r.finish(id, "✓", sanitizeLine(detail), true)
}

// Skip implements StepReporter. A skipped step has no duration.
func (r *LineReporter) Skip(id StepID, reason string) {
	r.finish(id, "-", sanitizeLine(reason), false)
}

// Fail implements StepReporter. Only the error's first line is printed; the
// full error reaches stderr through the command's return value.
func (r *LineReporter) Fail(id StepID, err error) {
	text := ""
	if err != nil {
		text = sanitizeLine(err.Error())
	}
	r.finish(id, "✗", text, true)
}

// Interrupt closes the reporter and marks every running step interrupted.
// Both happen under one hold of the lock: if it were released between
// them, a report from an abandoned worker already waiting on it could
// print ✓ under the ■ line.
func (r *LineReporter) Interrupt() {
	r.mu.Lock()
	if !r.closed {
		r.closeLocked()
		now := time.Now()
		for i, step := range r.plan {
			s, running := r.running[step.ID]
			if !running || r.index[step.ID] != i {
				continue
			}
			r.printf("%s ■ %s: %s (%s)\n", r.prefix(i), step.Title, interruptedText, FormatDuration(now.Sub(s.started)))
		}
		clear(r.running)
	}
	r.mu.Unlock()
	// Wait outside the lock: the ticker may be blocked on it.
	r.wg.Wait()
}

// Close stops the ticker and waits for it to exit. Later calls, and every
// report after the first Close, are no-ops.
func (r *LineReporter) Close() {
	r.mu.Lock()
	r.closeLocked()
	r.mu.Unlock()
	// Wait outside the lock: the ticker may be blocked on it.
	r.wg.Wait()
}

// closeLocked refuses every later report and tells the ticker to stop. The
// caller holds mu.
func (r *LineReporter) closeLocked() {
	if r.closed {
		return
	}
	r.closed = true
	close(r.stop)
}

// finish prints a step's closing line: "[n/N] ✓ Title: detail (0.4s)".
func (r *LineReporter) finish(id StepID, mark, text string, timed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.lookup(id)
	if !ok {
		return
	}
	line := fmt.Sprintf("%s %s %s", r.prefix(i), mark, r.plan[i].Title)
	if text != "" {
		line += ": " + text
	}
	// A step that was never started has no duration to report.
	if s, running := r.running[id]; running && timed {
		line += " (" + FormatDuration(time.Since(s.started)) + ")"
	}
	delete(r.running, id)
	r.printf("%s\n", line)
}

func (r *LineReporter) tickLoop() {
	ticker := time.NewTicker(lineTick)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.tick()
		}
	}
}

// tick prints what each running step owes the log, in plan order.
func (r *LineReporter) tick() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	now := time.Now()
	for i, step := range r.plan {
		s, running := r.running[step.ID]
		if !running || r.index[step.ID] != i {
			continue
		}
		silent := now.Sub(s.lastLine)
		// A value with nothing to draw (no counts yet, or bytes without a
		// total) would print a blank line and hold off the heartbeat.
		progress := progressLine(s.progress)
		switch {
		case progress != "" && s.progress != s.printed && silent >= lineProgressEvery:
			r.printf("%s%s\n", lineIndent, progress)
			s.printed = s.progress
			s.lastLine = now
		case silent >= lineHeartbeatAfter:
			// Whole seconds: the tick lands a fraction after the mark, and
			// "still running (30.4s)" is noise in a heartbeat.
			elapsed := now.Sub(s.started).Truncate(time.Second)
			r.printf("%sstill running (%s)\n", lineIndent, FormatDuration(elapsed))
			s.lastLine = now
		}
	}
}

// lookup returns the plan index of id, or false when the reporter is closed
// or id is not in the plan. The caller holds mu.
func (r *LineReporter) lookup(id StepID) (int, bool) {
	if r.closed {
		return 0, false
	}
	i, ok := r.index[id]
	return i, ok
}

func (r *LineReporter) prefix(i int) string {
	return fmt.Sprintf("[%d/%d]", i+1, len(r.plan))
}

// printf writes one line. A failed write to the log cannot be reported
// anywhere more useful, and must not stop the work being logged.
func (r *LineReporter) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.out, format, args...)
}

// progressLine renders "118/312 files · 4.6/12.4 MB (37%)".
func progressLine(p Progress) string {
	done, total := p.Items, p.ItemsTotal
	if p.BytesTotal > 0 {
		done, total = p.Bytes, p.BytesTotal
	}
	var bytes string
	if p.BytesTotal > 0 {
		bytes = formatBytePair(p.Bytes, p.BytesTotal)
	}
	line := joinCounters("", itemsText(p.Items, p.ItemsTotal, p.Noun), bytes)
	if total > 0 {
		line += fmt.Sprintf(" (%d%%)", percent(done, total))
	}
	return line
}

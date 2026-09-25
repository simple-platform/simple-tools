package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultWidth  = 80
	defaultHeight = 24

	// Each row is "  " + glyph (2 cells) + " " + title + " " + duration (6)
	// + "  " + detail; rowChrome is every cell of that except the title and
	// the detail.
	glyphCells    = 2
	durationCells = 6
	rowChrome     = 2 + glyphCells + 1 + 1 + durationCells + 2

	// The bar shares the row with its counters: it gets whatever they leave,
	// up to maxBarCells, and is dropped rather than drawn uselessly short.
	maxBarCells = 24
	minBarCells = 10

	// noteIndent lines a note up under its step's title.
	noteIndent = "     "
)

// Fixed 256-colour codes only: an AdaptiveColor makes lipgloss query the
// terminal's background, and that OSC reply races stdin. The glyphs and the
// bar's two block characters stay readable when colour is off (NO_COLOR,
// TERM=dumb). VS16 emoji such as ⚠️ never appear here, because terminals
// disagree on their width.
var (
	headerStyle      = lipgloss.NewStyle().Bold(true)
	spinnerStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	pendingStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	runningStyle     = lipgloss.NewStyle().Bold(true)
	detailStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	failedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	interruptedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	barFilledStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	barEmptyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	footerStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

const (
	glyphDone        = "✅"
	glyphFailed      = "❌"
	glyphPending     = "○"
	glyphSkipped     = "–"
	glyphInterrupted = "■"
	interruptedText  = "interrupted"
)

type stepState int

const (
	statePending stepState = iota
	stateRunning
	stateDone
	stateSkipped
	stateFailed
	stateInterrupted
)

type stepRow struct {
	title       string
	state       stepState
	detail      string
	note        string
	progress    Progress
	hasProgress bool
	started     time.Time
	ended       time.Time
}

// InterruptMsg tells the model a signal arrived. The runner forwards SIGINT
// and SIGTERM as this message, so every interrupt takes the same graceful
// path as ctrl+c in raw mode.
type InterruptMsg struct{ At time.Time }

// FinishedMsg tells the model the work has returned. The runner decides the
// outcome from the work's result; the model only stops drawing.
type FinishedMsg struct{}

// StepsModelConfig configures a StepsModel.
type StepsModelConfig struct {
	Header string
	Plan   []Step
	// Width and Height seed the layout until the first WindowSizeMsg, which
	// arrives only after the first frame is drawn. Zero means 80x24.
	Width, Height int
	// Start runs the work. Init returns it alongside the spinner tick, so the
	// work begins only once the terminal is set up: a start-up failure never
	// leaves work running with no display.
	Start tea.Cmd
	// OnInterrupt is called from Update when ctrl+c arrives as a key in raw
	// mode, before the program quits, so the work is cancelled even though
	// no signal was raised. at is the moment the model ended the running
	// step with, so the runner can report the same duration.
	OnInterrupt func(at time.Time)
}

// StepsModel draws a plan as one row per step, pending steps included. It is
// a value type without locks: all state changes arrive as messages through
// Update, and View is a pure function of the model.
type StepsModel struct {
	header      string
	ids         map[StepID]int // read-only after NewStepsModel, so copies may share it
	rows        []stepRow      // copied before every change, so earlier models stay intact
	titleWidth  int
	width       int
	height      int
	spinner     spinner.Model
	start       tea.Cmd
	onInterrupt func(at time.Time)
	began       time.Time
	// now advances from spinner ticks and message timestamps; View never
	// reads the wall clock, so a frame is reproducible from the model alone.
	now         time.Time
	quitting    bool
	interrupted bool
	finished    bool
}

// NewStepsModel builds a model with every step of cfg.Plan pending.
func NewStepsModel(cfg StepsModelConfig) StepsModel {
	began := time.Now()
	m := StepsModel{
		header:      sanitizeLine(cfg.Header),
		ids:         make(map[StepID]int, len(cfg.Plan)),
		rows:        make([]stepRow, len(cfg.Plan)),
		width:       defaultWidth,
		height:      defaultHeight,
		spinner:     spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(spinnerStyle)),
		start:       cfg.Start,
		onInterrupt: cfg.OnInterrupt,
		began:       began,
		now:         began,
	}
	if cfg.Width > 0 {
		m.width = cfg.Width
	}
	if cfg.Height > 0 {
		m.height = cfg.Height
	}
	for i, step := range cfg.Plan {
		title := sanitizeLine(step.Title)
		m.rows[i] = stepRow{title: title}
		m.titleWidth = max(m.titleWidth, ansi.StringWidth(title))
		if _, dup := m.ids[step.ID]; !dup {
			m.ids[step.ID] = i
		}
	}
	return m
}

// Init starts the spinner and the work together.
func (m StepsModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.start)
}

// Update applies one message. Once the display is quitting, step reports
// are ignored, with one exception: after an interrupt, how a step ended is
// still recorded (see settle). A runner can therefore replay every report
// into the model Run returned, so the final frame also shows the ones
// made after the program stopped.
func (m StepsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		return m, nil

	case spinner.TickMsg:
		m.advance(msg.Time)
		if m.quitting {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if msg.Type != tea.KeyCtrlC || m.quitting {
			return m, nil
		}
		at := time.Now()
		m = m.interrupt(at)
		if m.onInterrupt != nil {
			m.onInterrupt(at)
		}
		return m, tea.Quit

	case InterruptMsg:
		if m.quitting {
			return m, nil
		}
		at := msg.At
		if at.IsZero() {
			at = m.now
		}
		return m.interrupt(at), tea.Quit

	case FinishedMsg:
		m.finished = true
		m.quitting = true
		return m, tea.Quit

	case stepMessage:
		ev := msg.event()
		i, ok := m.ids[ev.id]
		if !ok {
			return m, nil
		}
		row := m.rows[i]
		switch {
		case !m.quitting:
			row = row.apply(msg, ev.at)
		case m.interrupted:
			if row, ok = row.settle(msg, ev.at); !ok {
				return m, nil
			}
		default:
			// The work has returned, so nothing reported now is news.
			return m, nil
		}
		m.advance(ev.at)
		m.rows = slices.Clone(m.rows)
		m.rows[i] = row
		return m, nil
	}
	return m, nil
}

// Interrupted reports whether an interrupt stopped the display.
func (m StepsModel) Interrupted() bool { return m.interrupted }

// Finished reports whether the work returned before the display stopped.
func (m StepsModel) Finished() bool { return m.finished }

func (m *StepsModel) advance(t time.Time) {
	if t.After(m.now) {
		m.now = t
	}
}

func (m StepsModel) interrupt(at time.Time) StepsModel {
	m.advance(at)
	m.rows = slices.Clone(m.rows)
	for i := range m.rows {
		if m.rows[i].state == stateRunning {
			m.rows[i] = m.rows[i].finish(stateInterrupted, interruptedText, at)
		}
	}
	m.interrupted = true
	m.quitting = true
	return m
}

func (r stepRow) apply(msg stepMessage, at time.Time) stepRow {
	switch msg := msg.(type) {
	case stepStartedMsg:
		return stepRow{title: r.title, state: stateRunning, detail: sanitizeLine(msg.detail), note: r.note, started: at}
	case stepDetailMsg:
		if r.state == stateRunning {
			r.detail = sanitizeLine(msg.detail)
		}
	case stepNoteMsg:
		r.note = sanitizeLine(msg.text)
	case stepProgressMsg:
		// Progress for a step that is not running is a late send from a
		// worker the step has already finished with; drawing it would
		// resurrect a bar on a done row.
		if r.state == stateRunning {
			r.progress = mergeProgress(r.progress, msg.progress)
			// A value with nothing to draw would blank the step's detail.
			r.hasProgress = hasCounts(r.progress)
		}
	case stepDoneMsg:
		return r.finish(stateDone, msg.detail, at)
	case stepSkippedMsg:
		r = r.finish(stateSkipped, msg.reason, at)
		r.started, r.ended = time.Time{}, time.Time{}
	case stepFailedMsg:
		return r.finish(stateFailed, msg.text, at)
	}
	return r
}

// settle applies a report that arrives after an interrupt. The work can
// still end a step then: a reply that raced the interrupt, or a write the
// grace period let finish. The runner judges the outcome from the work's
// result, so the frame must not say "interrupted" over a step that
// succeeded. Only how a step ended is recorded, and only for a step that
// had not already ended, so replaying a report applied before changes
// nothing. A step that starts after the interrupt was cut short at once.
// Transient status (detail, progress, notes) is dropped.
func (r stepRow) settle(msg stepMessage, at time.Time) (stepRow, bool) {
	switch msg.(type) {
	case stepStartedMsg:
		if r.state != statePending {
			return r, false
		}
		r.started = at
		return r.finish(stateInterrupted, interruptedText, at), true
	case stepDoneMsg, stepSkippedMsg, stepFailedMsg:
		if r.state != statePending && r.state != stateInterrupted {
			return r, false
		}
		return r.apply(msg, at), true
	}
	return r, false
}

func (r stepRow) finish(state stepState, detail string, at time.Time) stepRow {
	if r.started.IsZero() {
		r.started = at
	}
	r.state = state
	r.detail = sanitizeLine(detail)
	r.ended = at
	r.progress, r.hasProgress = Progress{}, false
	return r
}

// mergeProgress keeps the per-field maximum: progress from concurrent
// workers can arrive out of order, and a bar that steps backwards reads as a
// bug. The noun is sanitized here because every consumer merges before it
// draws.
func mergeProgress(prev, next Progress) Progress {
	noun := sanitizeLine(next.Noun)
	if noun == "" {
		noun = prev.Noun
	}
	return Progress{
		Items:      max(prev.Items, next.Items),
		ItemsTotal: max(prev.ItemsTotal, next.ItemsTotal),
		Bytes:      max(prev.Bytes, next.Bytes),
		BytesTotal: max(prev.BytesTotal, next.BytesTotal),
		Noun:       noun,
	}
}

// hasCounts reports whether p has anything to draw: an item count, or a
// total to measure against. Bytes without a total are not drawn.
func hasCounts(p Progress) bool {
	return p.Items > 0 || p.ItemsTotal > 0 || p.BytesTotal > 0
}

// View draws the live frame: header, blank, rows, blank, elapsed footer. It
// returns "" once quitting, which clears the frame; the runner prints
// FinalView after the program has torn down.
func (m StepsModel) View() string {
	if m.quitting {
		return ""
	}
	return strings.Join(m.liveLines(), "\n") + "\n"
}

// FinalView draws every step, notes included, with no height cap, footer or
// spinner, followed by a blank line. It is printed outside the renderer,
// which would otherwise drop the top of a tall frame and erase its last line.
func (m StepsModel) FinalView() string {
	return strings.Join(m.frameLines(frameLayout{blanks: true, notes: true, final: true}), "\n") + "\n"
}

// frameLayout says how much of the frame to draw.
type frameLayout struct {
	foldDone    int  // this many leading done/skipped steps become one summary row
	foldPending int  // this many trailing pending steps become one summary row
	dropDone    bool // leading done/skipped steps are not drawn at all
	dropPending bool // trailing pending steps are not drawn at all
	blanks      bool // blank lines around the rows
	footer      bool // the elapsed line
	notes       bool // note sub-lines
	final       bool // FinalView: no spinner; a step still running was cut short
}

// liveLines fits the live frame into height-1 lines (the frame ends in "\n",
// so the cursor takes one more). The renderer would drop lines from the top
// of a taller frame, losing the header first, so the model gives up detail
// in order of least value instead:
//
//  1. fold the oldest done or skipped steps into "✅ N steps done";
//  2. fold the last pending steps into "○  N more steps";
//  3. drop the blank lines and the footer;
//  4. drop note sub-lines;
//  5. drop the pending steps, then the done ones.
//
// Each fold takes only as many steps as the frame must lose, so the most
// recent outcomes and the next step stay visible. A fold always covers at
// least two steps: one step's summary would take the same line and say
// less. The header and the running or failed row are never folded.
func (m StepsModel) liveLines() []string {
	budget := m.height - 1
	doneEnd, pendingStart := m.runs()
	pendingRun := len(m.rows) - pendingStart

	layout := frameLayout{blanks: true, footer: true, notes: true}
	lines := m.frameLines(layout)
	for n := 2; len(lines) > budget && n <= doneEnd; n++ {
		layout.foldDone = n
		lines = m.frameLines(layout)
	}
	for n := 2; len(lines) > budget && n <= pendingRun; n++ {
		layout.foldPending = n
		lines = m.frameLines(layout)
	}
	shrink := []func(){
		func() { layout.blanks, layout.footer = false, false },
		func() { layout.notes = false },
		func() { layout.dropPending = true },
		func() { layout.dropDone = true },
	}
	for _, step := range shrink {
		if len(lines) <= budget {
			break
		}
		step()
		lines = m.frameLines(layout)
	}
	return lines
}

// runs returns the end of the leading run of done or skipped steps and the
// start of the trailing run of pending steps.
func (m StepsModel) runs() (doneEnd, pendingStart int) {
	for doneEnd < len(m.rows) && (m.rows[doneEnd].state == stateDone || m.rows[doneEnd].state == stateSkipped) {
		doneEnd++
	}
	pendingStart = len(m.rows)
	for pendingStart > doneEnd && m.rows[pendingStart-1].state == statePending {
		pendingStart--
	}
	return doneEnd, pendingStart
}

func (m StepsModel) frameLines(layout frameLayout) []string {
	lines := []string{m.fit(paint(headerStyle, m.header))}
	if layout.blanks {
		lines = append(lines, "")
	}

	doneEnd, pendingStart := m.runs()
	addRows := func(from, to int) {
		for i := from; i < to; i++ {
			lines = append(lines, m.fit(m.rowLine(m.rows[i], layout.final)))
			if layout.notes && m.rows[i].note != "" {
				lines = append(lines, m.fit(noteIndent+paint(detailStyle, m.rows[i].note)))
			}
		}
	}

	if !layout.dropDone {
		folded := min(layout.foldDone, doneEnd)
		if folded > 0 {
			lines = append(lines, m.fit(m.foldedDoneLine(folded)))
		}
		addRows(folded, doneEnd)
	}
	addRows(doneEnd, pendingStart)
	if !layout.dropPending {
		folded := min(layout.foldPending, len(m.rows)-pendingStart)
		addRows(pendingStart, len(m.rows)-folded)
		if folded > 0 {
			label := paint(pendingStyle, countLabel(folded, "more step", "more steps"))
			lines = append(lines, m.fit(m.layoutRow(paint(pendingStyle, glyphPending), label, "", "")))
		}
	}

	if layout.blanks {
		lines = append(lines, "")
	}
	if layout.footer {
		lines = append(lines, m.fit(paint(footerStyle, "  elapsed "+FormatDuration(m.now.Sub(m.began)))))
	}
	return lines
}

func countLabel(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (m StepsModel) foldedDoneLine(n int) string {
	var total time.Duration
	for _, r := range m.rows[:n] {
		total += r.ended.Sub(r.started)
	}
	return m.layoutRow(glyphDone, countLabel(n, "step done", "steps done"), FormatDuration(total), "")
}

// fit cuts a line one cell short of the terminal width. A line that fills
// the last column makes some terminals wrap early, and a wrapped line takes
// two rows while the renderer counts one, which smears the frame on redraw.
func (m StepsModel) fit(line string) string {
	return ansi.Truncate(line, max(m.width-1, 1), "…")
}

func (m StepsModel) rowLine(r stepRow, final bool) string {
	switch {
	case r.state == stateRunning && final:
		// FinalView is printed after the program stops. A step still running
		// then was cut short: the work returned early on a cancelled context,
		// or it was abandoned after the grace period.
		return m.layoutRow(paint(interruptedStyle, glyphInterrupted), r.title,
			FormatDuration(m.now.Sub(r.started)), paint(detailStyle, interruptedText))
	case r.state == stateRunning:
		detail := paint(detailStyle, r.detail)
		if r.hasProgress {
			detail = progressDetail(r.progress, m.width-1-rowChrome-m.titleWidth)
		}
		return m.layoutRow(m.spinner.View(), paint(runningStyle, r.title), FormatDuration(m.now.Sub(r.started)), detail)
	case r.state == stateDone:
		return m.layoutRow(glyphDone, r.title, FormatDuration(r.ended.Sub(r.started)), paint(detailStyle, r.detail))
	case r.state == stateSkipped:
		return m.layoutRow(paint(pendingStyle, glyphSkipped), r.title, "", paint(detailStyle, r.detail))
	case r.state == stateFailed:
		return m.layoutRow(glyphFailed, r.title, FormatDuration(r.ended.Sub(r.started)), paint(failedStyle, r.detail))
	case r.state == stateInterrupted:
		return m.layoutRow(paint(interruptedStyle, glyphInterrupted), r.title,
			FormatDuration(r.ended.Sub(r.started)), paint(detailStyle, r.detail))
	default:
		return m.layoutRow(paint(pendingStyle, glyphPending), paint(pendingStyle, r.title), "", "")
	}
}

// layoutRow places one row's columns. The duration column follows the
// longest title rather than the terminal's right edge, and nothing pads the
// row out to the edge: a short row then never reaches the last column, which
// absorbs ambiguous-width glyphs and narrowing resizes.
func (m StepsModel) layoutRow(glyph, title, duration, detail string) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(padRight(glyph, glyphCells))
	b.WriteString(" ")
	b.WriteString(title)
	if duration == "" && ansi.StringWidth(detail) == 0 {
		return b.String()
	}
	b.WriteString(strings.Repeat(" ", max(m.titleWidth-ansi.StringWidth(title), 0)))
	b.WriteString(" ")
	b.WriteString(strings.Repeat(" ", max(durationCells-ansi.StringWidth(duration), 0)))
	b.WriteString(duration)
	if ansi.StringWidth(detail) > 0 {
		b.WriteString("  ")
		b.WriteString(detail)
	}
	return b.String()
}

func padRight(s string, cells int) string {
	return s + strings.Repeat(" ", max(cells-ansi.StringWidth(s), 0))
}

// progressDetail draws a running step's progress in at most avail cells,
// choosing the first layout that fits: bar + percentage + items + bytes,
// then bar + percentage + items, then percentage + items + bytes, then
// percentage + items, then the percentage alone. The bar needs at least
// minBarCells, so once it goes the counters usually cannot fit bytes
// either; percentage + items keeps the file count on a 60-column terminal.
// Fit is judged on the widest the counters will get (done == total), so
// the layout and the bar's width hold still while the numbers grow.
func progressDetail(p Progress, avail int) string {
	done, total := p.Items, p.ItemsTotal
	if p.BytesTotal > 0 {
		done, total = p.Bytes, p.BytesTotal
	}
	items := itemsText(p.Items, p.ItemsTotal, p.Noun)
	widestItems := itemsText(max(p.Items, p.ItemsTotal), p.ItemsTotal, p.Noun)
	var bytes, widestBytes string
	if p.BytesTotal > 0 {
		bytes = formatBytePair(p.Bytes, p.BytesTotal)
		widestBytes = formatBytePair(max(p.Bytes, p.BytesTotal), p.BytesTotal)
	}
	if total <= 0 {
		// Nothing to measure against: counters only.
		return paint(detailStyle, joinCounters("", items, bytes))
	}
	pct := percent(done, total)

	// With the bar, the percentage is padded to 4 cells so the bar's end
	// does not move as it grows from 9% to 10% to 100%.
	for _, withBytes := range []bool{true, false} {
		widest := joinCounters("100%", widestItems, pick(withBytes, widestBytes))
		cells := min(maxBarCells, avail-1-ansi.StringWidth(widest))
		if cells >= minBarCells {
			counters := joinCounters(fmt.Sprintf("%3d%%", pct), items, pick(withBytes, bytes))
			return renderBar(done, total, cells) + " " + paint(detailStyle, counters)
		}
	}
	for _, withBytes := range []bool{true, false} {
		widest := joinCounters("100%", widestItems, pick(withBytes, widestBytes))
		if ansi.StringWidth(widest) <= avail {
			return paint(detailStyle, joinCounters(fmt.Sprintf("%d%%", pct), items, pick(withBytes, bytes)))
		}
	}
	return paint(detailStyle, fmt.Sprintf("%d%%", pct))
}

// pick returns s when want is set, else "".
func pick(want bool, s string) string {
	if want {
		return s
	}
	return ""
}

// paint styles s, leaving an empty s empty: lipgloss would wrap it in a
// pair of escape codes that draw nothing.
func paint(style lipgloss.Style, s string) string {
	if s == "" {
		return ""
	}
	return style.Render(s)
}

func itemsText(done, total int64, noun string) string {
	var s string
	switch {
	case total > 0:
		s = fmt.Sprintf("%d/%d", max(done, 0), total)
	case done > 0:
		s = fmt.Sprintf("%d", done)
	default:
		return ""
	}
	if noun != "" {
		s += " " + noun
	}
	return s
}

// joinCounters renders "65%  142/312 files · 8.1/12.4 MB", leaving out the
// parts that are empty.
func joinCounters(pct, items, bytes string) string {
	counts := items
	if bytes != "" {
		if counts != "" {
			counts += " · "
		}
		counts += bytes
	}
	switch {
	case pct == "":
		return counts
	case counts == "":
		return pct
	default:
		return pct + "  " + counts
	}
}

func renderBar(done, total int64, cells int) string {
	// Rounded down, like the percentage: a full bar means the work is done.
	filled := int(int64(cells) * min(max(done, 0), total) / total)
	return paint(barFilledStyle, strings.Repeat("█", filled)) +
		paint(barEmptyStyle, strings.Repeat("░", cells-filled))
}

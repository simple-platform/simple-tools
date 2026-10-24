package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// StepID names one step of a plan. Reporters and views look steps up by ID,
// so the same ID must not appear twice in one plan.
type StepID string

// Step is one row of a plan. Every step is known before the work starts, so
// the user sees what is still to come, not just what is running.
type Step struct {
	ID    StepID
	Title string
}

// Progress is a snapshot of a running step's counters. Producers may report
// from several goroutines, so consumers keep the per-field maximum rather than
// trusting arrival order.
type Progress struct {
	Items, ItemsTotal int64
	// The bar and percentage follow bytes when BytesTotal > 0, else items:
	// a few large files dominate an upload, so bytes are the honest measure.
	Bytes, BytesTotal int64
	Noun              string // what Items counts, e.g. "files"
}

// StepReporter receives the life cycle of each step in a plan. Implementations
// must be safe for concurrent use: collect and upload report from worker
// goroutines.
type StepReporter interface {
	// Start marks the step running. detail is transient status.
	Start(id StepID, detail string)
	// Detail replaces the running step's transient status. Plain output
	// ignores it, so nothing that must survive the run belongs here.
	Detail(id StepID, detail string)
	// Note records an event worth keeping, such as a retry or a warning.
	Note(id StepID, text string)
	// Progress reports the running step's counters.
	Progress(id StepID, p Progress)
	// Done marks the step finished; detail summarises its outcome.
	Done(id StepID, detail string)
	// Skip marks a step that did not need to run.
	Skip(id StepID, reason string)
	// Fail marks the step failed with err.
	Fail(id StepID, err error)
}

// NopReporter discards every report. `--json` uses it so stdout carries only
// the JSON document.
type NopReporter struct{}

var _ StepReporter = NopReporter{}

// Start implements StepReporter.
func (NopReporter) Start(StepID, string) {}

// Detail implements StepReporter.
func (NopReporter) Detail(StepID, string) {}

// Note implements StepReporter.
func (NopReporter) Note(StepID, string) {}

// Progress implements StepReporter.
func (NopReporter) Progress(StepID, Progress) {}

// Done implements StepReporter.
func (NopReporter) Done(StepID, string) {}

// Skip implements StepReporter.
func (NopReporter) Skip(StepID, string) {}

// Fail implements StepReporter.
func (NopReporter) Fail(StepID, error) {}

// FormatDuration renders a step duration compactly: "0.4s", "41.2s", "30s"
// under a minute, "3m12s" under an hour, "1h02m" beyond. Each branch rounds
// before choosing its unit, so 59.96s reads "1m00s" rather than "60s".
func FormatDuration(d time.Duration) string {
	d = max(d, 0)
	if r := d.Round(100 * time.Millisecond); r < time.Minute {
		s := strconv.FormatFloat(r.Seconds(), 'f', 1, 64)
		return strings.TrimSuffix(s, ".0") + "s"
	}
	if r := d.Round(time.Second); r < time.Hour {
		return fmt.Sprintf("%dm%02ds", int64(r/time.Minute), int64(r%time.Minute/time.Second))
	}
	r := d.Round(time.Minute)
	return fmt.Sprintf("%dh%02dm", int64(r/time.Hour), int64(r%time.Hour/time.Minute))
}

// byteUnits are SI units: file sizes on the server and in browsers use base
// 1000, so a binary unit here would disagree with what users compare it to.
var byteUnits = []string{"kB", "MB", "GB"}

// byteScale picks the unit for n: the largest one that keeps the rounded,
// one-decimal value under 1000, so 999,960 bytes reads "1.0 MB", not
// "1000.0 kB". A divisor of 1 means plain bytes.
func byteScale(n int64) (divisor float64, unit string) {
	if n < 1000 {
		return 1, "B"
	}
	divisor = 1000
	i := 0
	for i < len(byteUnits)-1 && math.Round(float64(n)/divisor*10)/10 >= 1000 {
		divisor *= 1000
		i++
	}
	return divisor, byteUnits[i]
}

func scaleBytes(n int64, divisor float64) string {
	if divisor == 1 {
		return strconv.FormatInt(n, 10)
	}
	return strconv.FormatFloat(float64(n)/divisor, 'f', 1, 64)
}

// FormatBytes renders a byte count in SI units with one decimal: "512 B",
// "37.2 kB", "18.2 MB".
func FormatBytes(n int64) string {
	n = max(n, 0)
	divisor, unit := byteScale(n)
	return scaleBytes(n, divisor) + " " + unit
}

// formatBytePair renders done/total in the total's unit ("8.1/12.4 MB"), so
// the two numbers are directly comparable and the unit never flips mid-run.
func formatBytePair(done, total int64) string {
	done, total = max(done, 0), max(total, 0)
	divisor, unit := byteScale(total)
	return scaleBytes(done, divisor) + "/" + scaleBytes(total, divisor) + " " + unit
}

// percent is done/total as a whole percentage, rounded down so 100% only
// appears once the work is complete.
func percent(done, total int64) int {
	if total <= 0 || done <= 0 {
		return 0
	}
	if done >= total {
		return 100
	}
	return int(done * 100 / total)
}

// sanitizeLine makes text safe to lay out in a single terminal row. Server
// errors and file paths can carry newlines, tabs, carriage returns and escape
// sequences; x/ansi measures \t and \r as zero width, so any of them would
// break the column layout or repaint over the frame. Only the first non-blank
// line is kept: the full text still reaches stderr through the command's
// error.
func sanitizeLine(s string) string {
	s = ansi.Strip(s)
	first := ""
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			first = line
			break
		}
	}
	first = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, first)
	return strings.TrimSpace(first)
}

package ui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// progressInterval caps progress messages per step. It matches the spinner's
// 10 frames per second: sending faster only queues frames nobody sees, and
// every send waits for the event loop.
const progressInterval = 100 * time.Millisecond

// stepEvent is what every step message carries. at is taken on the
// reporting goroutine, so durations do not include time spent waiting for
// the event loop.
type stepEvent struct {
	id StepID
	at time.Time
}

func (e stepEvent) event() stepEvent { return e }

// stepMessage is implemented by every message a TTYReporter sends.
type stepMessage interface{ event() stepEvent }

type (
	stepStartedMsg struct {
		stepEvent
		detail string
	}
	stepDetailMsg struct {
		stepEvent
		detail string
	}
	stepNoteMsg struct {
		stepEvent
		text string
	}
	stepProgressMsg struct {
		stepEvent
		progress Progress
	}
	stepDoneMsg struct {
		stepEvent
		detail string
	}
	stepSkippedMsg struct {
		stepEvent
		reason string
	}
	stepFailedMsg struct {
		stepEvent
		text string
	}
)

// TTYReporter turns reports into messages for a StepsModel. It is safe for
// concurrent use. Every send happens outside the reporter's lock, so a
// worker blocked on the event loop never holds up another worker's
// bookkeeping. Sends happen on the caller's goroutine, except a held
// progress value, which a timer sends when its interval ends.
type TTYReporter struct {
	send         func(tea.Msg)
	mu           sync.Mutex
	lastProgress map[StepID]time.Time
	lastDetail   map[StepID]string
	// held is the progress the throttle kept back for a step, and flush the
	// timer that sends it. A step has both or neither.
	held  map[StepID]Progress
	flush map[StepID]*time.Timer
}

var _ StepReporter = (*TTYReporter)(nil)

// NewTTYReporter returns a reporter that delivers messages through send,
// normally the running program's Send.
func NewTTYReporter(send func(tea.Msg)) *TTYReporter {
	return &TTYReporter{
		send:         send,
		lastProgress: make(map[StepID]time.Time),
		lastDetail:   make(map[StepID]string),
		held:         make(map[StepID]Progress),
		flush:        make(map[StepID]*time.Timer),
	}
}

// Start implements StepReporter.
func (r *TTYReporter) Start(id StepID, detail string) {
	at := time.Now()
	r.mu.Lock()
	r.dropHeld(id)
	delete(r.lastProgress, id)
	r.lastDetail[id] = detail
	r.mu.Unlock()
	r.send(stepStartedMsg{stepEvent{id, at}, detail})
}

// Detail implements StepReporter. A value identical to the step's previous
// one is dropped: download status repeats its percentage many times over.
func (r *TTYReporter) Detail(id StepID, detail string) {
	at := time.Now()
	r.mu.Lock()
	if last, seen := r.lastDetail[id]; seen && last == detail {
		r.mu.Unlock()
		return
	}
	r.lastDetail[id] = detail
	r.mu.Unlock()
	r.send(stepDetailMsg{stepEvent{id, at}, detail})
}

// Note implements StepReporter.
func (r *TTYReporter) Note(id StepID, text string) {
	r.send(stepNoteMsg{stepEvent{id, time.Now()}, text})
}

// Progress implements StepReporter. At most one message per step is sent per
// progressInterval. A value that comes too soon is held, merged with any
// value already held, and sent when the interval ends: upload acks arrive in
// bursts and then pause while large files go up, and without that last send
// the bar would show the start of the burst for as long as the pause lasts.
func (r *TTYReporter) Progress(id StepID, p Progress) {
	at := time.Now()
	r.mu.Lock()
	if _, holding := r.flush[id]; holding {
		r.held[id] = mergeProgress(r.held[id], p)
		r.mu.Unlock()
		return
	}
	if last, seen := r.lastProgress[id]; seen && at.Sub(last) < progressInterval {
		r.held[id] = p
		r.armFlush(id, last.Add(progressInterval).Sub(at))
		r.mu.Unlock()
		return
	}
	r.lastProgress[id] = at
	r.mu.Unlock()
	r.send(stepProgressMsg{stepEvent{id, at}, p})
}

// armFlush starts the timer that sends id's held value after d. The caller
// holds mu.
func (r *TTYReporter) armFlush(id StepID, d time.Duration) {
	var timer *time.Timer
	timer = time.AfterFunc(d, func() {
		r.mu.Lock()
		// armFlush's caller holds mu while timer is assigned, so it is set
		// by the time this gets the lock. A timer that fired just before
		// Start, Done, Skip or Fail stopped it finds its entry gone or
		// replaced, and sends nothing.
		if r.flush[id] != timer {
			r.mu.Unlock()
			return
		}
		p := r.held[id]
		delete(r.held, id)
		delete(r.flush, id)
		at := time.Now()
		r.lastProgress[id] = at
		r.mu.Unlock()
		// Sent from the timer's goroutine, so it can land just after the
		// step's Done; the model ignores progress for a step that is not
		// running.
		r.send(stepProgressMsg{stepEvent{id, at}, p})
	})
	r.flush[id] = timer
}

// dropHeld discards id's held value: the step has ended or restarted, so it
// is stale. The caller holds mu.
func (r *TTYReporter) dropHeld(id StepID) {
	if timer, holding := r.flush[id]; holding {
		timer.Stop()
		delete(r.flush, id)
	}
	delete(r.held, id)
}

// Done implements StepReporter.
func (r *TTYReporter) Done(id StepID, detail string) {
	at := r.end(id)
	r.send(stepDoneMsg{stepEvent{id, at}, detail})
}

// Skip implements StepReporter.
func (r *TTYReporter) Skip(id StepID, reason string) {
	at := r.end(id)
	r.send(stepSkippedMsg{stepEvent{id, at}, reason})
}

// Fail implements StepReporter. Only the error's first line is kept for the
// row; the full error reaches stderr through the command's return value.
func (r *TTYReporter) Fail(id StepID, err error) {
	text := ""
	if err != nil {
		text = sanitizeLine(err.Error())
	}
	at := r.end(id)
	r.send(stepFailedMsg{stepEvent{id, at}, text})
}

// end drops a finished step's held progress and returns the time it ended.
func (r *TTYReporter) end(id StepID) time.Time {
	at := time.Now()
	r.mu.Lock()
	r.dropHeld(id)
	r.mu.Unlock()
	return at
}

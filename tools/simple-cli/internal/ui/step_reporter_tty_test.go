package ui

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// recorder collects what a TTYReporter sends.
type recorder struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (r *recorder) send(msg tea.Msg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
}

func (r *recorder) all() []tea.Msg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tea.Msg(nil), r.msgs...)
}

// withoutTimes zeroes each message's timestamp so messages compare by
// content.
func withoutTimes(msgs []tea.Msg) []tea.Msg {
	out := make([]tea.Msg, 0, len(msgs))
	for _, msg := range msgs {
		switch m := msg.(type) {
		case stepStartedMsg:
			m.at = time.Time{}
			msg = m
		case stepDetailMsg:
			m.at = time.Time{}
			msg = m
		case stepNoteMsg:
			m.at = time.Time{}
			msg = m
		case stepProgressMsg:
			m.at = time.Time{}
			msg = m
		case stepDoneMsg:
			m.at = time.Time{}
			msg = m
		case stepSkippedMsg:
			m.at = time.Time{}
			msg = m
		case stepFailedMsg:
			m.at = time.Time{}
			msg = m
		}
		out = append(out, msg)
	}
	return out
}

func ev(id StepID) stepEvent { return stepEvent{id: id} }

func TestTTYReporter_MapsCallsToMessages(t *testing.T) {
	p := Progress{Items: 1, ItemsTotal: 3, Noun: "files"}
	tests := []struct {
		name string
		call func(r *TTYReporter)
		want tea.Msg
	}{
		{"start", func(r *TTYReporter) { r.Start("a", "detail") }, stepStartedMsg{ev("a"), "detail"}},
		{"detail", func(r *TTYReporter) { r.Detail("a", "detail") }, stepDetailMsg{ev("a"), "detail"}},
		{"note", func(r *TTYReporter) { r.Note("a", "note") }, stepNoteMsg{ev("a"), "note"}},
		{"progress", func(r *TTYReporter) { r.Progress("a", p) }, stepProgressMsg{ev("a"), p}},
		{"done", func(r *TTYReporter) { r.Done("a", "outcome") }, stepDoneMsg{ev("a"), "outcome"}},
		{"skip", func(r *TTYReporter) { r.Skip("a", "reason") }, stepSkippedMsg{ev("a"), "reason"}},
		{"fail keeps the first line", func(r *TTYReporter) { r.Fail("a", errors.New("boom\nstack")) }, stepFailedMsg{ev("a"), "boom"}},
		{"fail with nil", func(r *TTYReporter) { r.Fail("a", nil) }, stepFailedMsg{ev("a"), ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec recorder
			before := time.Now()
			tt.call(NewTTYReporter(rec.send))
			msgs := rec.all()
			if len(msgs) != 1 {
				t.Fatalf("sent %d messages, want 1", len(msgs))
			}
			if sm, ok := msgs[0].(stepMessage); !ok || sm.event().at.Before(before) {
				t.Errorf("message %#v lacks a send time", msgs[0])
			}
			if got := withoutTimes(msgs)[0]; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sent %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTTYReporter_ThrottlesProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		r := NewTTYReporter(rec.send)
		for i := range 1000 {
			r.Progress("upload", Progress{Items: int64(i)})
		}
		if n := len(rec.all()); n != 1 {
			t.Fatalf("a burst of 1000 sent %d messages at once, want 1", n)
		}

		// Another step has its own budget.
		r.Progress("collect", Progress{Items: 1})
		if n := len(rec.all()); n != 2 {
			t.Fatalf("another step's first progress was dropped; %d messages, want 2", n)
		}

		// The burst's latest value follows when the interval ends, and
		// nothing else does.
		time.Sleep(10 * progressInterval)
		msgs := rec.all()
		if len(msgs) != 3 {
			t.Fatalf("after the interval: %d messages, want 3", len(msgs))
		}
		if got := msgs[2].(stepProgressMsg).progress.Items; got != 999 {
			t.Errorf("held Items = %d, want the burst's latest value 999", got)
		}

		// After a quiet interval the next call sends at once.
		r.Progress("upload", Progress{Items: 1000})
		msgs = rec.all()
		if len(msgs) != 4 || msgs[3].(stepProgressMsg).progress.Items != 1000 {
			t.Fatalf("a call after a quiet interval was not sent at once: %d messages", len(msgs))
		}

		// A restarted step starts a fresh budget.
		r.Start("upload", "")
		r.Progress("upload", Progress{Items: 1})
		if n := len(rec.all()); n != 6 {
			t.Errorf("progress right after Start was dropped; %d messages, want 6", n)
		}
	})
}

// sent is a message and when it was sent, relative to the bubble's start.
type sent struct {
	msg   tea.Msg
	after time.Duration
}

func TestTTYReporter_SendsHeldProgress(t *testing.T) {
	// Upload acks come in bursts and then pause while large files go up.
	// The throttle sends the first value of a burst; the latest one must
	// follow when the interval ends, or the bar shows a stale count for as
	// long as the pause lasts.
	const ms = time.Millisecond
	boom := errors.New("boom")
	items := func(n int64) Progress { return Progress{Items: n, ItemsTotal: 312, Noun: "files"} }
	prog := func(id StepID, p Progress) tea.Msg { return stepProgressMsg{ev(id), p} }
	tests := []struct {
		name string
		run  func(r *TTYReporter)
		want []sent
	}{
		{
			name: "a burst's latest value follows when the interval ends",
			run: func(r *TTYReporter) {
				r.Start("upload", "")
				for i := range int64(32) {
					r.Progress("upload", items(i+1))
				}
			},
			want: []sent{
				{stepStartedMsg{ev("upload"), ""}, 0},
				{prog("upload", items(1)), 0},
				{prog("upload", items(32)), 100 * ms},
			},
		},
		{
			name: "held values keep the per-field maximum",
			run: func(r *TTYReporter) {
				r.Progress("upload", Progress{Items: 5, ItemsTotal: 10})
				r.Progress("upload", Progress{Items: 9, Bytes: 10})
				r.Progress("upload", Progress{Items: 7, Bytes: 20})
			},
			want: []sent{
				{prog("upload", Progress{Items: 5, ItemsTotal: 10}), 0},
				{prog("upload", Progress{Items: 9, Bytes: 20}), 100 * ms},
			},
		},
		{
			name: "the held value is sent once per interval",
			run: func(r *TTYReporter) {
				r.Progress("upload", items(1))
				time.Sleep(50 * ms)
				r.Progress("upload", items(2))
				time.Sleep(30 * ms)
				r.Progress("upload", items(3)) // joins the held value
				time.Sleep(70 * ms)            // 150ms: inside the flush's interval
				r.Progress("upload", items(4))
			},
			want: []sent{
				{prog("upload", items(1)), 0},
				{prog("upload", items(3)), 100 * ms},
				{prog("upload", items(4)), 200 * ms},
			},
		},
		{
			name: "a call after a quiet interval is sent at once",
			run: func(r *TTYReporter) {
				r.Progress("upload", items(1))
				time.Sleep(250 * ms)
				r.Progress("upload", items(2))
			},
			want: []sent{
				{prog("upload", items(1)), 0},
				{prog("upload", items(2)), 250 * ms},
			},
		},
		{
			name: "each step holds its own value",
			run: func(r *TTYReporter) {
				r.Progress("upload", items(1))
				r.Progress("upload", items(2))
				time.Sleep(30 * ms)
				r.Progress("collect", items(1))
				r.Progress("collect", items(2))
			},
			want: []sent{
				{prog("upload", items(1)), 0},
				{prog("collect", items(1)), 30 * ms},
				{prog("upload", items(2)), 100 * ms},
				{prog("collect", items(2)), 130 * ms},
			},
		},
		{
			name: "done drops the held value",
			run: func(r *TTYReporter) {
				r.Progress("upload", items(1))
				r.Progress("upload", items(2))
				r.Done("upload", "312 files")
			},
			want: []sent{
				{prog("upload", items(1)), 0},
				{stepDoneMsg{ev("upload"), "312 files"}, 0},
			},
		},
		{
			name: "skip drops the held value",
			run: func(r *TTYReporter) {
				r.Progress("upload", items(1))
				r.Progress("upload", items(2))
				r.Skip("upload", "all files already on server")
			},
			want: []sent{
				{prog("upload", items(1)), 0},
				{stepSkippedMsg{ev("upload"), "all files already on server"}, 0},
			},
		},
		{
			name: "fail drops the held value",
			run: func(r *TTYReporter) {
				r.Progress("upload", items(1))
				r.Progress("upload", items(2))
				r.Fail("upload", boom)
			},
			want: []sent{
				{prog("upload", items(1)), 0},
				{stepFailedMsg{ev("upload"), "boom"}, 0},
			},
		},
		{
			name: "start drops the held value and resets the budget",
			run: func(r *TTYReporter) {
				r.Progress("upload", items(1))
				r.Progress("upload", items(2))
				r.Start("upload", "")
				r.Progress("upload", items(3))
			},
			want: []sent{
				{prog("upload", items(1)), 0},
				{stepStartedMsg{ev("upload"), ""}, 0},
				{prog("upload", items(3)), 0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var rec recorder
				begin := time.Now()
				tt.run(NewTTYReporter(rec.send))
				time.Sleep(time.Second) // any held value is sent by now
				msgs := rec.all()
				got := make([]sent, len(msgs))
				for i, msg := range withoutTimes(msgs) {
					got[i] = sent{msg, msgs[i].(stepMessage).event().at.Sub(begin)}
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("sent:\n%+v\nwant:\n%+v", got, tt.want)
				}
			})
		})
	}
}

func TestTTYReporter_FiredTimerAfterDropSendsNothing(t *testing.T) {
	// Real time: a timer that has fired and is waiting for the lock while
	// Done drops its value must not send. synctest cannot stage this, as a
	// goroutine waiting on a mutex never counts as durably blocked.
	var rec recorder
	r := NewTTYReporter(rec.send)
	r.mu.Lock()
	r.held["upload"] = Progress{Items: 2}
	r.armFlush("upload", 0)
	time.Sleep(20 * time.Millisecond) // the timer fires and waits on mu
	r.dropHeld("upload")
	r.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	if msgs := rec.all(); len(msgs) != 0 {
		t.Errorf("a dropped value was sent: %#v", msgs)
	}
}

func TestTTYReporter_DedupesDetail(t *testing.T) {
	var rec recorder
	r := NewTTYReporter(rec.send)
	r.Start("config", "loading")
	r.Detail("config", "loading")              // same as Start: dropped
	r.Detail("config", "scl-parser: 10%")      // sent
	r.Detail("config", "scl-parser: 10%")      // dropped
	r.Detail("config", "scl-parser: 11%")      // sent
	r.Detail("config", "scl-parser: 10%")      // not consecutive: sent
	r.Detail("connect", "scl-parser: 10%")     // another step: sent
	r.Note("config", "downloading scl-parser") // notes are never deduped
	r.Note("config", "downloading scl-parser")

	want := []tea.Msg{
		stepStartedMsg{ev("config"), "loading"},
		stepDetailMsg{ev("config"), "scl-parser: 10%"},
		stepDetailMsg{ev("config"), "scl-parser: 11%"},
		stepDetailMsg{ev("config"), "scl-parser: 10%"},
		stepDetailMsg{ev("connect"), "scl-parser: 10%"},
		stepNoteMsg{ev("config"), "downloading scl-parser"},
		stepNoteMsg{ev("config"), "downloading scl-parser"},
	}
	if got := withoutTimes(rec.all()); !reflect.DeepEqual(got, want) {
		t.Errorf("sent:\n%#v\nwant:\n%#v", got, want)
	}
}

func TestTTYReporter_ConcurrentCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		r := NewTTYReporter(rec.send)
		ids := []StepID{"collect", "upload"}
		var wg sync.WaitGroup
		for w := range 16 {
			wg.Go(func() {
				id := ids[w%len(ids)]
				for i := range 200 {
					r.Progress(id, Progress{Items: int64(i), ItemsTotal: 200})
					r.Detail(id, string(rune('a'+i%3)))
					if i%50 == 0 {
						r.Note(id, "note")
					}
					if i%20 == 0 {
						// Let held values flush while other workers report.
						time.Sleep(30 * time.Millisecond)
					}
				}
			})
		}
		wg.Wait()
		synctest.Wait() // a flush already under way finishes sending
		for _, id := range ids {
			r.Done(id, "")
		}
		time.Sleep(time.Second)

		msgs := rec.all()
		ended := map[StepID]bool{}
		notes := 0
		for _, msg := range msgs {
			switch msg := msg.(type) {
			case stepDoneMsg:
				ended[msg.id] = true
			case stepProgressMsg:
				if ended[msg.id] {
					t.Errorf("progress for %s after its Done", msg.id)
				}
			case stepNoteMsg:
				notes++
			}
		}
		if len(ended) != len(ids) {
			t.Errorf("Done sent for %v, want every step", ended)
		}
		if notes != 16*4 {
			t.Errorf("notes = %d, want every one of %d", notes, 16*4)
		}
	})
}

package ui

import (
	"errors"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		n    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"negative clamps to zero", -5, "0 B"},
		{"bytes", 999, "999 B"},
		{"one kilobyte", 1000, "1.0 kB"},
		{"kilobytes", 37_240, "37.2 kB"},
		{"rounds into the next unit", 999_960, "1.0 MB"},
		{"stays in kB below the rounding edge", 999_940, "999.9 kB"},
		{"megabytes", 18_200_000, "18.2 MB"},
		{"gigabytes", 2_500_000_000, "2.5 GB"},
		{"gigabytes are the largest unit", 1_500_000_000_000, "1500.0 GB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytes(tt.n); got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestFormatBytePair(t *testing.T) {
	tests := []struct {
		name        string
		done, total int64
		want        string
	}{
		{"uses the total's unit", 8_100_000, 12_400_000, "8.1/12.4 MB"},
		{"small done in a large unit", 40_000, 12_400_000, "0.0/12.4 MB"},
		{"bytes", 512, 900, "512/900 B"},
		{"kilobytes", 1_500, 37_200, "1.5/37.2 kB"},
		{"zero total", 0, 0, "0/0 B"},
		{"negative values clamp", -1, -1, "0/0 B"},
		{"complete", 12_400_000, 12_400_000, "12.4/12.4 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBytePair(tt.done, tt.total); got != tt.want {
				t.Errorf("formatBytePair(%d, %d) = %q, want %q", tt.done, tt.total, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"negative clamps to zero", -time.Second, "0s"},
		{"sub-second rounds to a tenth", 440 * time.Millisecond, "0.4s"},
		{"trailing .0 is dropped", 30 * time.Second, "30s"},
		{"tenths", 41_240 * time.Millisecond, "41.2s"},
		{"rounding up to a minute switches unit", 59_960 * time.Millisecond, "1m00s"},
		{"one minute", time.Minute, "1m00s"},
		{"minutes and seconds", 3*time.Minute + 12*time.Second, "3m12s"},
		{"seconds round", time.Minute + 51_600*time.Millisecond, "1m52s"},
		{"rounding up to an hour switches unit", 59*time.Minute + 59_600*time.Millisecond, "1h00m"},
		{"hours and minutes", time.Hour + 2*time.Minute, "1h02m"},
		{"many hours", 26*time.Hour + 30*time.Minute + 29*time.Second, "26h30m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatDuration(tt.d); got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestPercent(t *testing.T) {
	tests := []struct {
		name        string
		done, total int64
		want        int
	}{
		{"zero total", 5, 0, 0},
		{"negative total", 5, -1, 0},
		{"nothing done", 0, 10, 0},
		{"negative done", -3, 10, 0},
		{"rounds down", 8_100_000, 12_400_000, 65},
		{"never 100 before the end", 999, 1000, 99},
		{"complete", 10, 10, 100},
		{"over the total clamps", 11, 10, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percent(tt.done, tt.total); got != tt.want {
				t.Errorf("percent(%d, %d) = %d, want %d", tt.done, tt.total, got, tt.want)
			}
		})
	}
}

func TestSanitizeLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is unchanged", "312 files · 12.4 MB", "312 files · 12.4 MB"},
		{"tab becomes a space", "a\tb", "a b"},
		{"carriage return becomes a space", "50%\r60%", "50% 60%"},
		{"windows line ending", "first\r\nsecond", "first"},
		{"keeps the first line", "record sync failed\n\ngoroutine 1 [running]:", "record sync failed"},
		{"skips leading blank lines", "\n  \nreal message\nmore", "real message"},
		{"strips ANSI styling", "\x1b[31mred\x1b[0m text", "red text"},
		{"strips cursor movement", "a\x1b[2Kb\x1b[1A", "ab"},
		{"other C0 controls become spaces", "bell\x07null\x00end", "bell null end"},
		{"C1 controls become spaces", "a\u0085b", "a b"},
		{"trims surrounding space", "  padded \t", "padded"},
		{"empty", "", ""},
		{"only blank lines", "\n\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeLine(tt.in); got != tt.want {
				t.Errorf("sanitizeLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMergeProgress(t *testing.T) {
	tests := []struct {
		name       string
		prev, next Progress
		want       Progress
	}{
		{
			name: "newer values win",
			prev: Progress{Items: 1, ItemsTotal: 10, Bytes: 100, BytesTotal: 1000, Noun: "files"},
			next: Progress{Items: 2, ItemsTotal: 10, Bytes: 200, BytesTotal: 1000, Noun: "files"},
			want: Progress{Items: 2, ItemsTotal: 10, Bytes: 200, BytesTotal: 1000, Noun: "files"},
		},
		{
			name: "a late, older snapshot never moves backwards",
			prev: Progress{Items: 5, ItemsTotal: 10, Bytes: 500, BytesTotal: 1000},
			next: Progress{Items: 3, ItemsTotal: 10, Bytes: 700, BytesTotal: 1000},
			want: Progress{Items: 5, ItemsTotal: 10, Bytes: 700, BytesTotal: 1000},
		},
		{
			name: "the noun is sanitized",
			next: Progress{Items: 1, Noun: "\x1b[31mfiles\x1b[0m\n"},
			want: Progress{Items: 1, Noun: "files"},
		},
		{
			name: "an empty noun keeps the previous one",
			prev: Progress{Noun: "files"},
			next: Progress{Items: 1},
			want: Progress{Items: 1, Noun: "files"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeProgress(tt.prev, tt.next); got != tt.want {
				t.Errorf("mergeProgress() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNopReporter(t *testing.T) {
	// NopReporter must satisfy StepReporter and accept every call without
	// effect or panic, including a nil error.
	var r StepReporter = NopReporter{}
	calls := []struct {
		name string
		call func()
	}{
		{"Start", func() { r.Start("a", "detail") }},
		{"Detail", func() { r.Detail("a", "detail") }},
		{"Note", func() { r.Note("a", "note") }},
		{"Progress", func() { r.Progress("a", Progress{Items: 1, ItemsTotal: 2}) }},
		{"Done", func() { r.Done("a", "done") }},
		{"Skip", func() { r.Skip("a", "reason") }},
		{"Fail", func() { r.Fail("a", errors.New("boom")) }},
		{"Fail with nil", func() { r.Fail("a", nil) }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			c.call()
		})
	}
}

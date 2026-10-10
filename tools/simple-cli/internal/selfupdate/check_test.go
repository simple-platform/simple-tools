package selfupdate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"simple-cli/internal/fsx"
)

const statePath = "/home/someone/.simple/update-check.json"

var noon = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// kept writes what an earlier check left on disk.
func kept(t *testing.T, checkedAt time.Time, latest string) []byte {
	t.Helper()

	content, err := json.Marshal(remembered{CheckedAt: checkedAt, Latest: latest})
	if err != nil {
		t.Fatalf("failed to write the state: %v", err)
	}

	return content
}

func TestCheckerNewer(t *testing.T) {
	current := Version{1, 3, 2}

	tests := []struct {
		name        string
		onDisk      []byte
		github      string
		githubErr   error
		mkdirErr    error
		writeErr    error
		want        Version
		wantNewer   bool
		wantAsked   int
		wantKept    string
		wantNoWrite bool
	}{
		{
			name:   "never checked: GitHub is asked and the answer is kept",
			github: "1.4.0",
			want:   Version{1, 4, 0}, wantNewer: true, wantAsked: 1, wantKept: "1.4.0",
		},
		{
			name:   "checked an hour ago: the answer on disk is used",
			onDisk: kept(t, noon.Add(-time.Hour), "1.4.0"),
			github: "9.9.9",
			want:   Version{1, 4, 0}, wantNewer: true, wantAsked: 0, wantKept: "1.4.0",
		},
		{
			name:   "checked a day ago: GitHub is asked again",
			onDisk: kept(t, noon.Add(-24*time.Hour), "1.4.0"),
			github: "1.5.0",
			want:   Version{1, 5, 0}, wantNewer: true, wantAsked: 1, wantKept: "1.5.0",
		},
		{
			name:   "checked in the future, by a clock that has since been put right: GitHub is asked",
			onDisk: kept(t, noon.Add(time.Hour), "1.4.0"),
			github: "1.5.0",
			want:   Version{1, 5, 0}, wantNewer: true, wantAsked: 1, wantKept: "1.5.0",
		},
		{
			name:   "the running version is the newest",
			github: "1.3.2",
			want:   Version{}, wantNewer: false, wantAsked: 1, wantKept: "1.3.2",
		},
		{
			name:   "the running version is ahead of the newest release",
			github: "1.3.0",
			want:   Version{}, wantNewer: false, wantAsked: 1, wantKept: "1.3.0",
		},
		{
			name:      "GitHub does not answer and nothing was known",
			githubErr: errBroken,
			want:      Version{}, wantNewer: false, wantAsked: 1, wantKept: "",
		},
		{
			name:      "GitHub does not answer: what was known before is kept and still told",
			onDisk:    kept(t, noon.Add(-48*time.Hour), "1.4.0"),
			githubErr: errBroken,
			want:      Version{1, 4, 0}, wantNewer: true, wantAsked: 1, wantKept: "1.4.0",
		},
		{
			name:   "a file that is not the state is the same as never having checked",
			onDisk: []byte("not json"),
			github: "1.4.0",
			want:   Version{1, 4, 0}, wantNewer: true, wantAsked: 1, wantKept: "1.4.0",
		},
		{
			name:   "a kept answer that is not a version says nothing",
			onDisk: kept(t, noon.Add(-time.Hour), "soon"),
			want:   Version{}, wantNewer: false, wantAsked: 0, wantKept: "soon",
		},
		{
			name:     "the home directory cannot be made: the answer is still given",
			github:   "1.4.0",
			mkdirErr: errBroken,
			want:     Version{1, 4, 0}, wantNewer: true, wantAsked: 1, wantNoWrite: true,
		},
		{
			name:     "the state cannot be written: the answer is still given",
			github:   "1.4.0",
			writeErr: errBroken,
			want:     Version{1, 4, 0}, wantNewer: true, wantAsked: 1, wantNoWrite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disk := &fsx.MockFileSystem{Files: map[string][]byte{}}
			if tt.onDisk != nil {
				disk.Files[statePath] = tt.onDisk
			}
			disk.MkdirAllErr = tt.mkdirErr
			disk.WriteFileErr = tt.writeErr

			asked := 0
			checker := Checker{
				FS:   disk,
				Path: statePath,
				Now:  func() time.Time { return noon },
				Latest: func(context.Context) (Release, error) {
					asked++
					if tt.githubErr != nil {
						return Release{}, tt.githubErr
					}
					version, _ := ParseVersion(tt.github)
					return Release{Version: version}, nil
				},
			}

			got, newer := checker.Newer(context.Background(), current)

			if got != tt.want || newer != tt.wantNewer {
				t.Errorf("Newer() = %v, %v; want %v, %v", got, newer, tt.want, tt.wantNewer)
			}
			if asked != tt.wantAsked {
				t.Errorf("GitHub was asked %d times, want %d", asked, tt.wantAsked)
			}

			content, written := disk.Files[statePath]
			if tt.wantNoWrite {
				if written {
					t.Errorf("the state was written: %s", content)
				}
				return
			}

			var state remembered
			if err := json.Unmarshal(content, &state); err != nil {
				t.Fatalf("the state on disk is not readable: %v (%s)", err, content)
			}
			if state.Latest != tt.wantKept {
				t.Errorf("kept latest = %q, want %q", state.Latest, tt.wantKept)
			}
			if tt.wantAsked > 0 && !state.CheckedAt.Equal(noon) {
				t.Errorf("kept checked_at = %v, want %v", state.CheckedAt, noon)
			}
		})
	}
}

// A machine that is offline is asked about once a day, not by every command.
func TestCheckerNewer_AFailedCheckIsNotRepeatedUntilTomorrow(t *testing.T) {
	disk := &fsx.MockFileSystem{Files: map[string][]byte{}}
	asked := 0
	now := noon
	checker := Checker{
		FS:   disk,
		Path: statePath,
		Now:  func() time.Time { return now },
		Latest: func(context.Context) (Release, error) {
			asked++
			return Release{}, errBroken
		},
	}

	for range 3 {
		checker.Newer(context.Background(), Version{1, 3, 2})
		now = now.Add(time.Hour)
	}
	if asked != 1 {
		t.Errorf("GitHub was asked %d times in three hours, want 1", asked)
	}

	now = noon.Add(24 * time.Hour)
	checker.Newer(context.Background(), Version{1, 3, 2})
	if asked != 2 {
		t.Errorf("GitHub was asked %d times by the next day, want 2", asked)
	}
}

func TestCheckerRecord(t *testing.T) {
	disk := &fsx.MockFileSystem{Files: map[string][]byte{}}
	asked := 0
	checker := Checker{
		FS:   disk,
		Path: statePath,
		Now:  func() time.Time { return noon },
		Latest: func(context.Context) (Release, error) {
			asked++
			return Release{}, errBroken
		},
	}

	checker.Record(Version{1, 4, 0})

	got, newer := checker.Newer(context.Background(), Version{1, 3, 2})
	if got != (Version{1, 4, 0}) || !newer {
		t.Errorf("Newer() after Record = %v, %v; want 1.4.0, true", got, newer)
	}
	if asked != 0 {
		t.Errorf("GitHub was asked %d times after a version was recorded, want 0", asked)
	}
}

// A clock set past the year 9999 cannot be written as a time, and the state is
// then left as it was rather than written wrong.
func TestCheckerRecord_ATimeThatCannotBeWritten(t *testing.T) {
	disk := &fsx.MockFileSystem{Files: map[string][]byte{}}
	checker := Checker{
		FS:   disk,
		Path: statePath,
		Now:  func() time.Time { return time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
	}

	checker.Record(Version{1, 4, 0})

	if content, written := disk.Files[statePath]; written {
		t.Errorf("the state was written: %s", content)
	}
}

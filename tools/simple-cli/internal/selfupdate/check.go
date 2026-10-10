package selfupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"simple-cli/internal/fsx"
)

// checkEvery is how long an answer from GitHub is kept before it is asked
// again. A day is often enough to hear of a release the day it is made, and
// seldom enough that a command almost never waits on the network for it.
const checkEvery = 24 * time.Hour

// FileSystem is what an update needs of a disk: the file that remembers the
// last check, and the program being replaced. fsx.OSFileSystem provides it,
// and so does fsx.MockFileSystem.
type FileSystem interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Rename(oldpath, newpath string) error
	Remove(name string) error
}

// remembered is what the last check found, as it is kept on disk.
type remembered struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// Checker knows whether a newer release than the running program is out,
// asking GitHub at most once a day.
type Checker struct {
	FS     FileSystem
	Path   string
	Now    func() time.Time
	Latest func(ctx context.Context) (Release, error)
}

// Newer answers with the newest release known, when it is newer than current.
//
// It never fails. A machine that is offline, a GitHub that will not answer and
// a home directory that cannot be written all end the same way, with nothing
// to say, because the command the user ran has nothing to do with any of them.
func (c Checker) Newer(ctx context.Context, current Version) (Version, bool) {
	known := c.read()

	now := c.Now()
	if now.Sub(known.CheckedAt) >= checkEvery || now.Before(known.CheckedAt) {
		known = c.ask(ctx, known, now)
	}

	latest, ok := ParseVersion(known.Latest)
	if !ok || !latest.NewerThan(current) {
		return Version{}, false
	}

	return latest, true
}

// Record keeps a version as the newest known as of now, for a caller that has
// just asked GitHub itself or has just installed it.
func (c Checker) Record(latest Version) {
	c.write(remembered{CheckedAt: c.Now(), Latest: latest.String()})
}

// ask puts the question to GitHub and keeps the answer.
//
// THE TIME IS KEPT EVEN WHEN NOTHING ANSWERED. A check that failed and left no
// trace would be made again by the very next command, and by every command
// after it for as long as the machine is offline, each one waiting out the
// same timeout. What was known before the failure is kept beside it.
func (c Checker) ask(ctx context.Context, known remembered, now time.Time) remembered {
	if release, err := c.Latest(ctx); err == nil {
		known.Latest = release.Version.String()
	}
	known.CheckedAt = now

	c.write(known)

	return known
}

// read loads what the last check found. A file that is missing or cannot be
// read as one is the same as never having checked.
func (c Checker) read() remembered {
	var known remembered

	content, err := c.FS.ReadFile(c.Path)
	if err != nil {
		return remembered{}
	}
	if err := json.Unmarshal(content, &known); err != nil {
		return remembered{}
	}

	return known
}

// write keeps what a check found. It is not reported when it fails: the cost
// is one more question to GitHub next time, and the user asked for neither.
func (c Checker) write(known remembered) {
	content, err := json.Marshal(known)
	if err != nil {
		return
	}
	if err := c.FS.MkdirAll(filepath.Dir(c.Path), fsx.DirPerm); err != nil {
		return
	}

	_ = c.FS.WriteFile(c.Path, content, fsx.FilePerm)
}

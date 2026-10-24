package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"simple-cli/internal/fsx"
)

const (
	// maxProgram bounds the download of one program. The CLI is tens of
	// megabytes; this is room to grow without letting an answer that never
	// ends fill the machine's memory.
	maxProgram = 512 << 20

	// maxChecksums bounds the file of checksums, which is a line for each
	// program a release carries.
	maxChecksums = 1 << 20

	// freshSuffix names the downloaded program while it sits beside the one
	// it is about to replace, and asideSuffix names the replaced one where it
	// cannot be removed yet.
	freshSuffix = ".new"
	asideSuffix = ".old"
)

// Installer replaces a program on disk with the one a release carries.
type Installer struct {
	FS        FileSystem
	Client    Doer
	GOOS      string
	UserAgent string
}

// Install downloads the release's program for this machine and puts it where
// exe is.
//
// NOTHING IS WRITTEN UNTIL THE DOWNLOAD MATCHES ITS CHECKSUM, so a download
// that was cut short or altered on the way leaves the disk as it was.
//
// THE NEW PROGRAM IS WRITTEN BESIDE THE OLD ONE AND RENAMED OVER IT, never
// written into it. The file a running program was started from is still in
// use: macOS ties a program's signature to the file it was checked on, and
// stalls or kills a process whose file is rewritten underneath it, and Windows
// refuses the write outright. A rename gives the name to a different file and
// leaves the old one whole for the process that is using it.
func (i Installer) Install(ctx context.Context, release Release, exe string) error {
	program, err := i.download(ctx, release)
	if err != nil {
		return err
	}

	// A file left by an update that was interrupted is removed first, so the
	// program is always written to a file of its own and never into one that
	// already has a history.
	fresh := exe + freshSuffix
	if err := i.FS.Remove(fresh); err != nil {
		return fmt.Errorf("failed to clear %s: %w", fresh, err)
	}
	if err := i.FS.WriteFile(fresh, program, fsx.ExecPerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", fresh, err)
	}

	if err := i.swap(exe, fresh); err != nil {
		_ = i.FS.Remove(fresh)
		return err
	}

	return nil
}

// Tidy removes the program an earlier update on Windows had to leave behind.
// It is safe to call anywhere and at any time: there is nothing to remove on
// other systems, and a file still in use stays until it is not.
func (i Installer) Tidy(exe string) {
	if i.GOOS == "windows" {
		_ = i.FS.Remove(exe + asideSuffix)
	}
}

// swap gives the name exe to the file at fresh.
//
// WINDOWS WILL NOT REPLACE A PROGRAM THAT IS RUNNING, AND WILL RENAME ONE. So
// there the running program is moved aside first and the new one takes its
// name. The one moved aside cannot be deleted while it runs, which is what
// Tidy is for. If the new program cannot take the name, the old one is moved
// back, so a failed update leaves a CLI that still starts.
func (i Installer) swap(exe, fresh string) error {
	if i.GOOS != "windows" {
		if err := i.FS.Rename(fresh, exe); err != nil {
			return fmt.Errorf("failed to replace %s: %w", exe, err)
		}

		return nil
	}

	aside := exe + asideSuffix
	i.Tidy(exe)
	if err := i.FS.Rename(exe, aside); err != nil {
		return fmt.Errorf("failed to move %s aside: %w", exe, err)
	}

	if err := i.FS.Rename(fresh, exe); err != nil {
		if back := i.FS.Rename(aside, exe); back != nil {
			return fmt.Errorf("failed to replace %s, and it is now at %s: %w", exe, aside, errors.Join(err, back))
		}

		return fmt.Errorf("failed to replace %s: %w", exe, err)
	}

	return nil
}

// download fetches the release's program for this machine and answers with it
// only if it is the file the release's checksums describe.
func (i Installer) download(ctx context.Context, release Release) ([]byte, error) {
	sums, err := get(ctx, i.Client, release.ChecksumsURL(), i.UserAgent, maxChecksums)
	if err != nil {
		return nil, fmt.Errorf("failed to download the checksums of %s: %w", release.Tag, err)
	}

	// The checksums list every program the release was built with, so a
	// machine that is not on the list is one the release has nothing for.
	want, listed, err := checksumOf(string(sums), release.Asset)
	if err != nil {
		return nil, fmt.Errorf("failed to read the checksums of %s: %w", release.Tag, err)
	}
	if !listed {
		return nil, fmt.Errorf("%w: %s carries no %s", ErrNoBuild, release.Tag, release.Asset)
	}

	program, err := get(ctx, i.Client, release.AssetURL(), i.UserAgent, maxProgram)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", release.Asset, err)
	}

	sum := sha256.Sum256(program)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("%s does not match its checksum: the release says %s and the download is %s", release.Asset, want, got)
	}

	return program, nil
}

// checksumOf finds one file's SHA-256 in a file of checksums, and says whether
// the file is listed at all. The file of checksums is what `sha256sum` prints:
// on each line the checksum, then the file's name, which that tool marks with
// a star when it read the file as binary.
func checksumOf(sums, name string) (sum string, listed bool, err error) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}

		sum = strings.ToLower(fields[0])
		if decoded, err := hex.DecodeString(sum); err != nil || len(decoded) != sha256.Size {
			return "", true, fmt.Errorf("the line for %s does not carry a SHA-256", name)
		}

		return sum, true, nil
	}

	return "", false, nil
}

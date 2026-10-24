package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
)

const (
	oldProgram = "the program that is running"
	newProgram = "the program the release carries"
)

// scriptedDisk is a disk whose renames fail when a test says so, one call at
// a time, so a test can break the second step of a swap and not the first.
type scriptedDisk struct {
	*fsx.MockFileSystem
	renames []error
}

func (d *scriptedDisk) Rename(oldpath, newpath string) error {
	if len(d.renames) > 0 {
		err := d.renames[0]
		d.renames = d.renames[1:]
		if err != nil {
			return err
		}
	}

	return d.MockFileSystem.Rename(oldpath, newpath)
}

// sumOf is the SHA-256 of content, as a file of checksums writes it.
func sumOf(content string) string {
	sum := sha256.Sum256([]byte(content))

	return hex.EncodeToString(sum[:])
}

func TestInstallerInstall(t *testing.T) {
	const (
		macExe     = "/usr/local/bin/simple"
		windowsExe = `C:\tools\simple.exe`
	)

	mac := Release{Version: Version{1, 4, 0}, Tag: "v1.4.0-simple-cli", Asset: macFile}
	windows := Release{Version: Version{1, 4, 0}, Tag: "v1.4.0-simple-cli", Asset: windowsFile}
	goodSums := func(release Release) string {
		return sumOf(newProgram) + "  " + release.Asset + "\n"
	}

	tests := []struct {
		name        string
		goos        string
		release     Release
		exe         string
		onDisk      map[string]string
		sums        *answer
		program     *answer
		renames     []error
		writeErr    error
		removeErr   error
		wantErr     string
		wantNoBuild bool
		wantOnDisk  map[string]string
	}{
		{
			name: "on macOS the new program takes the old one's name",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram},
			wantOnDisk: map[string]string{macExe: newProgram},
		},
		{
			name: "what an interrupted update left is cleared first",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram, macExe + ".new": "half a download"},
			wantOnDisk: map[string]string{macExe: newProgram},
		},
		{
			name: "on Windows the running program is moved aside and the new one takes its name",
			goos: "windows", release: windows, exe: windowsExe,
			onDisk:     map[string]string{windowsExe: oldProgram},
			wantOnDisk: map[string]string{windowsExe: newProgram, windowsExe + ".old": oldProgram},
		},
		{
			name: "on Windows the program an earlier update moved aside is replaced",
			goos: "windows", release: windows, exe: windowsExe,
			onDisk:     map[string]string{windowsExe: oldProgram, windowsExe + ".old": "an older program still"},
			wantOnDisk: map[string]string{windowsExe: newProgram, windowsExe + ".old": oldProgram},
		},
		{
			name: "the checksums are read as sha256sum writes them, whatever else is listed",
			goos: "darwin", release: mac, exe: macExe,
			onDisk: map[string]string{macExe: oldProgram},
			sums: &answer{body: sumOf("another program") + "  " + windowsFile + "\r\n" +
				strings.ToUpper(sumOf(newProgram)) + " *" + macFile + "\r\n"},
			wantOnDisk: map[string]string{macExe: newProgram},
		},
		{
			name: "a download that is not the file the release describes changes nothing",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:  map[string]string{macExe: oldProgram},
			program: &answer{body: "something else"},
			wantErr: macFile + " does not match its checksum: the release says " + sumOf(newProgram) +
				" and the download is " + sumOf("something else"),
			wantOnDisk: map[string]string{macExe: oldProgram},
		},
		{
			name: "a release without its checksums installs nothing",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram},
			sums:       &answer{status: http.StatusNotFound},
			wantErr:    "failed to download the checksums of v1.4.0-simple-cli",
			wantOnDisk: map[string]string{macExe: oldProgram},
		},
		{
			name: "a release built for other machines only installs nothing, and says so",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:      map[string]string{macExe: oldProgram},
			sums:        &answer{body: sumOf(newProgram) + "  " + windowsFile + "\n"},
			wantErr:     "the release has no build for this machine: v1.4.0-simple-cli carries no " + macFile,
			wantNoBuild: true,
			wantOnDisk:  map[string]string{macExe: oldProgram},
		},
		{
			name: "a line that is not a SHA-256 installs nothing",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram},
			sums:       &answer{body: "d41d8cd98f00b204e9800998ecf8427e  " + macFile + "\n"},
			wantErr:    "the line for " + macFile + " does not carry a SHA-256",
			wantOnDisk: map[string]string{macExe: oldProgram},
		},
		{
			name: "a program that cannot be downloaded installs nothing",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram},
			program:    &answer{status: http.StatusBadGateway},
			wantErr:    "failed to download " + macFile,
			wantOnDisk: map[string]string{macExe: oldProgram},
		},
		{
			name: "a leftover that cannot be cleared stops the update",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram},
			removeErr:  errBroken,
			wantErr:    "failed to clear " + macExe + ".new: broken on purpose",
			wantOnDisk: map[string]string{macExe: oldProgram},
		},
		{
			name: "a directory that cannot be written to stops the update",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram},
			writeErr:   errBroken,
			wantErr:    "failed to write " + macExe + ".new: broken on purpose",
			wantOnDisk: map[string]string{macExe: oldProgram},
		},
		{
			name: "on macOS a swap that fails leaves the old program and nothing beside it",
			goos: "darwin", release: mac, exe: macExe,
			onDisk:     map[string]string{macExe: oldProgram},
			renames:    []error{errBroken},
			wantErr:    "failed to replace " + macExe + ": broken on purpose",
			wantOnDisk: map[string]string{macExe: oldProgram},
		},
		{
			name: "on Windows a program that cannot be moved aside stays where it is",
			goos: "windows", release: windows, exe: windowsExe,
			onDisk:     map[string]string{windowsExe: oldProgram},
			renames:    []error{errBroken},
			wantErr:    "failed to move " + windowsExe + " aside: broken on purpose",
			wantOnDisk: map[string]string{windowsExe: oldProgram},
		},
		{
			name: "on Windows the old program is put back when the new one cannot take its name",
			goos: "windows", release: windows, exe: windowsExe,
			onDisk:     map[string]string{windowsExe: oldProgram},
			renames:    []error{nil, errBroken},
			wantErr:    "failed to replace " + windowsExe + ": broken on purpose",
			wantOnDisk: map[string]string{windowsExe: oldProgram},
		},
		{
			name: "on Windows a program that cannot be put back is named in the error",
			goos: "windows", release: windows, exe: windowsExe,
			onDisk:     map[string]string{windowsExe: oldProgram},
			renames:    []error{nil, errBroken, errBroken},
			wantErr:    "failed to replace " + windowsExe + ", and it is now at " + windowsExe + ".old",
			wantOnDisk: map[string]string{windowsExe + ".old": oldProgram},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sums := answer{body: goodSums(tt.release)}
			if tt.sums != nil {
				sums = *tt.sums
			}
			program := answer{body: newProgram}
			if tt.program != nil {
				program = *tt.program
			}
			github := &fakeGitHub{answers: map[string]answer{
				tt.release.ChecksumsURL(): sums,
				tt.release.AssetURL():     program,
			}}

			files := map[string][]byte{}
			for name, content := range tt.onDisk {
				files[name] = []byte(content)
			}
			disk := &scriptedDisk{
				MockFileSystem: &fsx.MockFileSystem{Files: files, WriteFileErr: tt.writeErr, RemoveErr: tt.removeErr},
				renames:        tt.renames,
			}

			installer := Installer{FS: disk, Client: github, GOOS: tt.goos, UserAgent: "simple-cli/test"}
			err := installer.Install(context.Background(), tt.release, tt.exe)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Install() error = %v, want one containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			if errors.Is(err, ErrNoBuild) != tt.wantNoBuild {
				t.Errorf("errors.Is(err, ErrNoBuild) = %v, want %v", !tt.wantNoBuild, tt.wantNoBuild)
			}

			if len(files) != len(tt.wantOnDisk) {
				t.Errorf("the disk holds %d files, want %d: %v", len(files), len(tt.wantOnDisk), namesOf(files))
			}
			for name, want := range tt.wantOnDisk {
				if got := string(files[name]); got != want {
					t.Errorf("%s holds %q, want %q", name, got, want)
				}
			}
		})
	}
}

// namesOf lists the files a disk holds, for a failure message.
func namesOf(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	return names
}

func TestInstallerTidy(t *testing.T) {
	const exe = `C:\tools\simple.exe`

	tests := []struct {
		name     string
		goos     string
		wantGone bool
	}{
		{"on Windows the program moved aside is removed", "windows", true},
		{"elsewhere a file of that name is not the CLI's to remove", "darwin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disk := &fsx.MockFileSystem{Files: map[string][]byte{exe + ".old": []byte(oldProgram)}}

			Installer{FS: disk, GOOS: tt.goos}.Tidy(exe)

			if _, still := disk.Files[exe+".old"]; still == tt.wantGone {
				t.Errorf("the file moved aside is still there = %v, want %v", still, !tt.wantGone)
			}
		})
	}
}

func TestChecksumOf(t *testing.T) {
	sum := sumOf(newProgram)

	tests := []struct {
		name       string
		sums       string
		want       string
		wantListed bool
		wantErr    string
	}{
		{name: "one line", sums: sum + "  " + macFile + "\n", want: sum, wantListed: true},
		{name: "no newline at the end", sums: sum + "  " + macFile, want: sum, wantListed: true},
		{name: "marked as binary", sums: sum + " *" + macFile + "\n", want: sum, wantListed: true},
		{
			name: "blank lines and other files",
			sums: "\n" + sumOf("x") + "  other\n\n" + sum + "  " + macFile + "\n",
			want: sum, wantListed: true,
		},
		{name: "a name that only ends the same", sums: sum + "  not-" + macFile + "\n"},
		{name: "a line with too many parts", sums: sum + "  " + macFile + " extra\n"},
		{name: "empty", sums: ""},
		{
			name: "not hexadecimal", sums: strings.Repeat("z", 64) + "  " + macFile + "\n",
			wantListed: true, wantErr: "does not carry a SHA-256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, listed, err := checksumOf(tt.sums, macFile)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("checksumOf() error = %v, want one containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("checksumOf() error = %v", err)
			}
			if got != tt.want || listed != tt.wantListed {
				t.Errorf("checksumOf() = %q, %v; want %q, %v", got, listed, tt.want, tt.wantListed)
			}
		})
	}
}

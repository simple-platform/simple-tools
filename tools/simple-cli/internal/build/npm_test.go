package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"simple-cli/internal/fsx"
)

// Installs run no dependency's install scripts: that is where a compromised
// release runs its code. Asserted on the arguments, because running npm here
// would need the network.
func TestNpmInstallRunsNoInstallScripts(t *testing.T) {
	if !slices.Equal(npmInstallArgs, []string{"install", "--ignore-scripts"}) {
		t.Errorf("npm install arguments are %v, want install --ignore-scripts", npmInstallArgs)
	}
}

const (
	lockedOne = `{"lockfileVersion":3,"packages":{"node_modules/is-number":{"version":"7.0.0"}}}`
	lockedTwo = `{"lockfileVersion":3,"packages":{"node_modules/is-number":{"version":"6.0.0"}}}`
)

// AN INSTALL IS CURRENT ONLY WHILE WHAT IT WAS INSTALLED FROM IS UNCHANGED.
//
// Each case changes one thing after an install has been recorded, and asks
// whether the packages still count as installed from the files there now.
func TestInstalledFromCurrentFiles(t *testing.T) {
	cases := []struct {
		name    string
		change  func(t *testing.T, dir string)
		current bool
	}{
		{
			name:    "nothing changed",
			change:  func(*testing.T, string) {},
			current: true,
		},
		{
			name:    "the lockfile moved a package",
			change:  func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "package-lock.json"), lockedTwo) },
			current: false,
		},
		{
			name: "the lockfile moved and moved back",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "package-lock.json"), lockedTwo)
				writeFile(t, filepath.Join(dir, "package-lock.json"), lockedOne)
			},
			current: true,
		},
		{
			name: "package.json gained an override",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "package.json"), `{"name":"thing","dependencies":{"is-number":"^7.0.0"},"overrides":{"is-number":"7.0.1"}}`)
			},
			current: false,
		},
		{
			name: "an .npmrc appeared",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, ".npmrc"), "install-strategy=linked\n")
			},
			current: false,
		},
		{
			name: "npm installed something by hand",
			change: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "node_modules", ".package-lock.json"), lockedTwo)
			},
			current: false,
		},
		{
			name: "the note of the install is gone",
			change: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "node_modules", installNote)); err != nil {
					t.Fatalf("could not remove the note: %v", err)
				}
			},
			current: false,
		},
		{
			name: "a file the install read can no longer be read",
			change: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, ".npmrc"), 0o755); err != nil {
					t.Fatalf("could not put a folder where .npmrc goes: %v", err)
				}
			},
			current: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "package.json"), `{"name":"thing","dependencies":{"is-number":"^7.0.0"}}`)
			writeFile(t, filepath.Join(dir, "package-lock.json"), lockedOne)
			writeFile(t, filepath.Join(dir, "node_modules", ".package-lock.json"), lockedOne)

			if err := RecordInstall(fsx.OSFileSystem{}, dir); err != nil {
				t.Fatalf("could not note the install: %v", err)
			}

			tc.change(t, dir)

			if got := InstalledFromCurrentFiles(fsx.OSFileSystem{}, dir); got != tc.current {
				t.Errorf("installed from the current files = %v, want %v", got, tc.current)
			}
		})
	}
}

// A package with nothing to install gets no node_modules from npm, so the note
// makes one. Without it, every run would install the package again.
func TestRecordInstall_NothingInstalled(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"thing"}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion":3,"packages":{"":{"name":"thing"}}}`)

	if err := RecordInstall(fsx.OSFileSystem{}, dir); err != nil {
		t.Fatalf("could not note the install: %v", err)
	}

	if !InstalledFromCurrentFiles(fsx.OSFileSystem{}, dir) {
		t.Error("a package with nothing to install does not count as installed")
	}
}

// A note that cannot be written is reported, not passed off as written.
func TestRecordInstall_ReportsAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"thing"}`)

	if err := os.Mkdir(filepath.Join(dir, "package-lock.json"), 0o755); err != nil {
		t.Fatalf("could not put a folder where the lockfile goes: %v", err)
	}

	if err := RecordInstall(fsx.OSFileSystem{}, dir); err == nil {
		t.Error("noted an install whose lockfile could not be read")
	}
}

// THE INSTALL NOTES ONLY WHAT NPM REALLY INSTALLED FROM.
//
// npm is stood in for by a command that succeeds, one that fails, and one that
// changes package.json while it runs, as a branch switched in another terminal
// would. Only the first may leave a note: the second installed nothing, and
// the third may have installed from the old package.json.
func TestEnsureDependencies_NotesOnlyAnInstallFromUnchangedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("npm is stood in for by shell commands")
	}

	cases := []struct {
		name    string
		npm     []string
		wantErr bool
		noted   bool
	}{
		{name: "npm succeeds", npm: []string{"true"}, noted: true},
		{name: "npm fails", npm: []string{"false"}, wantErr: true},
		{name: "package.json changes while npm runs", npm: []string{"sh", "-c", "echo >> package.json"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "package.json"), `{"name":"thing"}`)
			writeFile(t, filepath.Join(dir, "package-lock.json"), lockedOne)

			original := ExecCommandFunc
			ExecCommandFunc = func(string, ...string) *exec.Cmd { return exec.Command(tc.npm[0], tc.npm[1:]...) }
			t.Cleanup(func() { ExecCommandFunc = original })

			if err := EnsureDependencies(dir); (err != nil) != tc.wantErr {
				t.Fatalf("install error = %v, want an error: %v", err, tc.wantErr)
			}

			if got := InstalledFromCurrentFiles(fsx.OSFileSystem{}, dir); got != tc.noted {
				t.Errorf("installed from the current files = %v, want %v", got, tc.noted)
			}
		})
	}
}

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"simple-cli/internal/fsx"
	"simple-cli/internal/selfupdate"
)

const (
	testExe      = "/usr/local/bin/simple"
	testPlatform = "darwin/arm64"
)

var errVersionBroken = errors.New("broken on purpose")

// versionFixture answers for GitHub and for the disk, and remembers what the
// command did with them.
type versionFixture struct {
	current    string
	latest     string
	latestErr  error
	exeErr     error
	installErr error

	installed []string
	recorded  []string
}

func (f *versionFixture) deps() versionDeps {
	return versionDeps{
		current:  f.current,
		platform: testPlatform,
		latest: func(context.Context) (selfupdate.Release, error) {
			if f.latestErr != nil {
				return selfupdate.Release{}, f.latestErr
			}
			version, _ := selfupdate.ParseVersion(f.latest)

			return selfupdate.Release{
				Version: version,
				Tag:     "v" + f.latest + "-simple-cli",
				Asset:   selfupdate.AssetName("darwin", "arm64"),
			}, nil
		},
		install: func(_ context.Context, release selfupdate.Release, exe string) error {
			if f.installErr != nil {
				return f.installErr
			}
			f.installed = append(f.installed, release.Tag+" over "+exe)

			return nil
		},
		executable: func() (string, error) {
			if f.exeErr != nil {
				return "", f.exeErr
			}

			return testExe, nil
		},
		record: func(latest selfupdate.Version) {
			f.recorded = append(f.recorded, latest.String())
		},
	}
}

// inJSONMode runs a test as `--json` would, and puts the flag back.
func inJSONMode(t *testing.T, on bool) {
	t.Helper()

	jsonOutput = on
	t.Cleanup(func() { jsonOutput = false })
}

func TestRunVersion(t *testing.T) {
	notice := "\nA new version of simple is available: 1.4.0 (you have 1.3.2).\nRun `simple version update` to install it.\n"

	tests := []struct {
		name         string
		fixture      versionFixture
		json         bool
		want         string
		wantRecorded []string
	}{
		{
			name:         "a newer release is out",
			fixture:      versionFixture{current: "1.3.2", latest: "1.4.0"},
			want:         "simple 1.3.2\n" + notice,
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:         "the running version is the newest",
			fixture:      versionFixture{current: "1.4.0", latest: "1.4.0"},
			want:         "simple 1.4.0\nThis is the newest version.\n",
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:         "a build ahead of the newest release",
			fixture:      versionFixture{current: "1.5.0", latest: "1.4.0"},
			want:         "simple 1.5.0\nThis is the newest version.\n",
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:         "a build from source is told what the newest release is",
			fixture:      versionFixture{current: "dev", latest: "1.4.0"},
			want:         "simple dev\nThe newest release is 1.4.0. Run `simple version update` to install it.\n",
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:    "GitHub does not answer: the version is still printed",
			fixture: versionFixture{current: "1.3.2", latestErr: errVersionBroken},
			want:    "simple 1.3.2\nCould not check for a newer version: broken on purpose\n",
		},
		{
			name:         "as JSON, with a newer release out",
			fixture:      versionFixture{current: "1.3.2", latest: "1.4.0"},
			json:         true,
			want:         "{\n  \"latest\": \"1.4.0\",\n  \"update_available\": true,\n  \"version\": \"1.3.2\"\n}\n",
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:         "as JSON, on the newest",
			fixture:      versionFixture{current: "1.4.0", latest: "1.4.0"},
			json:         true,
			want:         "{\n  \"latest\": \"1.4.0\",\n  \"update_available\": false,\n  \"version\": \"1.4.0\"\n}\n",
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:    "as JSON, when GitHub does not answer",
			fixture: versionFixture{current: "1.3.2", latestErr: errVersionBroken},
			json:    true,
			want:    "{\n  \"version\": \"1.3.2\"\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inJSONMode(t, tt.json)
			fixture := tt.fixture
			out := new(bytes.Buffer)

			if err := runVersion(context.Background(), out, fixture.deps()); err != nil {
				t.Fatalf("runVersion() error = %v", err)
			}

			if out.String() != tt.want {
				t.Errorf("output = %q, want %q", out.String(), tt.want)
			}
			if fmt.Sprint(fixture.recorded) != fmt.Sprint(tt.wantRecorded) {
				t.Errorf("recorded %v, want %v", fixture.recorded, tt.wantRecorded)
			}
		})
	}
}

func TestRunVersionUpdate(t *testing.T) {
	refused := fmt.Errorf("failed to write %s.new: %w", testExe, fs.ErrPermission)

	tests := []struct {
		name          string
		fixture       versionFixture
		json          bool
		want          string
		wantErr       string
		wantInstalled []string
		wantRecorded  []string
	}{
		{
			name:          "a newer release is installed over the running program",
			fixture:       versionFixture{current: "1.3.2", latest: "1.4.0"},
			want:          "Downloading simple 1.4.0 for darwin/arm64...\n✅ Updated simple 1.3.2 to 1.4.0\n",
			wantInstalled: []string{"v1.4.0-simple-cli over " + testExe},
			wantRecorded:  []string{"1.4.0"},
		},
		{
			name:         "the newest release is left alone",
			fixture:      versionFixture{current: "1.4.0", latest: "1.4.0"},
			want:         "simple 1.4.0 is up to date.\n",
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:         "a build ahead of the newest release is not taken back to it",
			fixture:      versionFixture{current: "1.5.0", latest: "1.4.0"},
			want:         "simple 1.5.0 is up to date.\n",
			wantRecorded: []string{"1.4.0"},
		},
		{
			name:          "a build from source is replaced when asked",
			fixture:       versionFixture{current: "dev", latest: "1.4.0"},
			want:          "Downloading simple 1.4.0 for darwin/arm64...\n✅ Updated simple dev to 1.4.0\n",
			wantInstalled: []string{"v1.4.0-simple-cli over " + testExe},
			wantRecorded:  []string{"1.4.0"},
		},
		{
			name:    "GitHub does not answer",
			fixture: versionFixture{current: "1.3.2", latestErr: errVersionBroken},
			wantErr: "failed to find the newest version: broken on purpose",
		},
		{
			name:    "the CLI has never been released",
			fixture: versionFixture{current: "1.3.2", latestErr: selfupdate.ErrNoRelease},
			wantErr: "failed to find the newest version: no release was found",
		},
		{
			name: "the newest release has nothing built for this machine",
			fixture: versionFixture{
				current:    "1.3.2",
				latest:     "1.4.0",
				installErr: fmt.Errorf("%w: v1.4.0-simple-cli carries no simple-cli-linux-amd64", selfupdate.ErrNoBuild),
			},
			want:    "Downloading simple 1.4.0 for darwin/arm64...\n",
			wantErr: "failed to update simple: the release has no build for this machine: v1.4.0-simple-cli carries no simple-cli-linux-amd64",
		},
		{
			name:    "the running program cannot be found",
			fixture: versionFixture{current: "1.3.2", latest: "1.4.0", exeErr: errVersionBroken},
			wantErr: "broken on purpose",
		},
		{
			name:    "the install fails",
			fixture: versionFixture{current: "1.3.2", latest: "1.4.0", installErr: errVersionBroken},
			want:    "Downloading simple 1.4.0 for darwin/arm64...\n",
			wantErr: "failed to update simple: broken on purpose",
		},
		{
			name:    "the program is installed where this account cannot write",
			fixture: versionFixture{current: "1.3.2", latest: "1.4.0", installErr: refused},
			want:    "Downloading simple 1.4.0 for darwin/arm64...\n",
			wantErr: "This account cannot write to " + filepath.Dir(testExe) + ". Run the command again from one that can, " +
				"or download the new version from https://github.com/simple-platform/simple-tools/releases/tag/v1.4.0-simple-cli",
		},
		{
			name:          "as JSON, an update",
			fixture:       versionFixture{current: "1.3.2", latest: "1.4.0"},
			json:          true,
			want:          "{\n  \"from\": \"1.3.2\",\n  \"status\": \"updated\",\n  \"to\": \"1.4.0\"\n}\n",
			wantInstalled: []string{"v1.4.0-simple-cli over " + testExe},
			wantRecorded:  []string{"1.4.0"},
		},
		{
			name:         "as JSON, nothing to do",
			fixture:      versionFixture{current: "1.4.0", latest: "1.4.0"},
			json:         true,
			want:         "{\n  \"status\": \"current\",\n  \"version\": \"1.4.0\"\n}\n",
			wantRecorded: []string{"1.4.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inJSONMode(t, tt.json)
			fixture := tt.fixture
			out := new(bytes.Buffer)

			err := runVersionUpdate(context.Background(), out, fixture.deps())

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("runVersionUpdate() error = %v, want one containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("runVersionUpdate() error = %v", err)
			}

			if out.String() != tt.want {
				t.Errorf("output = %q, want %q", out.String(), tt.want)
			}
			if fmt.Sprint(fixture.installed) != fmt.Sprint(tt.wantInstalled) {
				t.Errorf("installed %v, want %v", fixture.installed, tt.wantInstalled)
			}
			if fmt.Sprint(fixture.recorded) != fmt.Sprint(tt.wantRecorded) {
				t.Errorf("recorded %v, want %v", fixture.recorded, tt.wantRecorded)
			}
		})
	}
}

func TestUpdateNoticeWanted(t *testing.T) {
	tests := []struct {
		name        string
		jsonMode    bool
		terminal    bool
		ciEnv       string
		commandPath string
		want        bool
	}{
		{name: "a person at a terminal", terminal: true, commandPath: "simple deploy", want: true},
		{name: "the bare command", terminal: true, commandPath: "simple", want: true},
		{name: "output that is not a terminal", terminal: false, commandPath: "simple deploy", want: false},
		{name: "JSON output", jsonMode: true, terminal: true, commandPath: "simple deploy", want: false},
		{name: "a CI job with a terminal", terminal: true, ciEnv: "true", commandPath: "simple deploy", want: false},
		{name: "CI set to false", terminal: true, ciEnv: "false", commandPath: "simple deploy", want: true},
		{name: "the version command", terminal: true, commandPath: "simple version", want: false},
		{name: "the update itself", terminal: true, commandPath: "simple version update", want: false},
		{name: "help", terminal: true, commandPath: "simple help", want: false},
		{name: "a completion script", terminal: true, commandPath: "simple completion zsh", want: false},
		{name: "the shell asking for completions", terminal: true, commandPath: "simple __complete", want: false},
		{name: "the same, without descriptions", terminal: true, commandPath: "simple __completeNoDesc", want: false},
		{name: "a command that only starts like a quiet one", terminal: true, commandPath: "simple versions", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updateNoticeWanted(tt.jsonMode, tt.terminal, tt.ciEnv, tt.commandPath); got != tt.want {
				t.Errorf("updateNoticeWanted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLookForUpdate(t *testing.T) {
	const statePath = "/home/someone/.simple/update-check.json"
	current := selfupdate.Version{Major: 1, Minor: 3, Patch: 2}

	tests := []struct {
		name      string
		latest    string
		exeErr    error
		want      string
		wantTidy  bool
		wantAsked int
	}{
		{
			name:     "a newer release is announced",
			latest:   "1.4.0",
			want:     "\nA new version of simple is available: 1.4.0 (you have 1.3.2).\nRun `simple version update` to install it.\n",
			wantTidy: true, wantAsked: 1,
		},
		{name: "the newest release says nothing", latest: "1.3.2", want: "", wantTidy: true, wantAsked: 1},
		{
			name:     "a program that cannot be found is still checked",
			latest:   "1.4.0",
			exeErr:   errVersionBroken,
			want:     "\nA new version of simple is available: 1.4.0 (you have 1.3.2).\nRun `simple version update` to install it.\n",
			wantTidy: false, wantAsked: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disk := &fsx.MockFileSystem{Files: map[string][]byte{testExe + ".old": []byte("left behind")}}
			asked := 0
			parts := updater{
				checker: selfupdate.Checker{
					FS:   disk,
					Path: statePath,
					Now:  time.Now,
					Latest: func(context.Context) (selfupdate.Release, error) {
						asked++
						version, _ := selfupdate.ParseVersion(tt.latest)

						return selfupdate.Release{Version: version}, nil
					},
				},
				installer: selfupdate.Installer{FS: disk, GOOS: "windows"},
			}
			errOut := new(bytes.Buffer)

			lookForUpdate(errOut, parts, current, func() (string, error) { return testExe, tt.exeErr })

			if errOut.String() != tt.want {
				t.Errorf("notice = %q, want %q", errOut.String(), tt.want)
			}
			if _, left := disk.Files[testExe+".old"]; left == tt.wantTidy {
				t.Errorf("the file an update left behind is still there = %v, want %v", left, !tt.wantTidy)
			}
			if asked != tt.wantAsked {
				t.Errorf("GitHub was asked %d times, want %d", asked, tt.wantAsked)
			}
		})
	}
}

// Nothing here reaches the network: both runs are turned away before a
// request could be made, one for carrying no version and one for not being at
// a terminal.
func TestAnnounceUpdate_SaysNothingWhereNobodyIsReading(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"a build from source", "dev"},
		{"a release whose output is not a terminal", "1.3.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := Version
			Version = tt.version
			t.Cleanup(func() { Version = previous })

			errOut := new(bytes.Buffer)
			announceUpdate(errOut, "simple deploy")

			if errOut.Len() != 0 {
				t.Errorf("announceUpdate() wrote %q, want nothing", errOut.String())
			}
		})
	}
}

// homeless takes away every variable the home directory is read from.
func homeless(t *testing.T) {
	t.Helper()

	for _, name := range []string{"SIMPLE_CLI_HOME", "HOME", "USERPROFILE", "home"} {
		t.Setenv(name, "")
	}
}

func TestNewUpdater(t *testing.T) {
	t.Run("keeps its state under the CLI's home", func(t *testing.T) {
		t.Setenv("SIMPLE_CLI_HOME", filepath.Join("somewhere", "else"))

		parts, err := newUpdater()
		if err != nil {
			t.Fatalf("newUpdater() error = %v", err)
		}

		if want := filepath.Join("somewhere", "else", ".simple", "update-check.json"); parts.checker.Path != want {
			t.Errorf("state path = %q, want %q", parts.checker.Path, want)
		}
		if parts.finder.GOOS != runtime.GOOS || parts.finder.GOARCH != runtime.GOARCH {
			t.Errorf("finder looks for %s/%s, want this machine's", parts.finder.GOOS, parts.finder.GOARCH)
		}
		if want := "simple-cli/" + Version; parts.finder.UserAgent != want || parts.installer.UserAgent != want {
			t.Errorf("user agents = %q and %q, want %q", parts.finder.UserAgent, parts.installer.UserAgent, want)
		}
		if parts.installer.GOOS != runtime.GOOS {
			t.Errorf("installer swaps as %s does, want %s", parts.installer.GOOS, runtime.GOOS)
		}
	})

	t.Run("fails without a home directory", func(t *testing.T) {
		homeless(t)

		if _, err := newUpdater(); err == nil || !strings.Contains(err.Error(), "failed to find the home directory") {
			t.Errorf("newUpdater() error = %v, want one about the home directory", err)
		}
	})
}

func TestDefaultVersionDeps(t *testing.T) {
	t.Run("is built for this program and this machine", func(t *testing.T) {
		t.Setenv("SIMPLE_CLI_HOME", "somewhere")

		deps, err := defaultVersionDeps()
		if err != nil {
			t.Fatalf("defaultVersionDeps() error = %v", err)
		}

		if deps.current != Version {
			t.Errorf("current = %q, want %q", deps.current, Version)
		}
		if want := runtime.GOOS + "/" + runtime.GOARCH; deps.platform != want {
			t.Errorf("platform = %q, want %q", deps.platform, want)
		}
		if deps.latest == nil || deps.install == nil || deps.executable == nil || deps.record == nil {
			t.Error("a dependency is missing")
		}
	})

	t.Run("fails without a home directory", func(t *testing.T) {
		homeless(t)

		if _, err := defaultVersionDeps(); err == nil {
			t.Error("defaultVersionDeps() error = nil, want one about the home directory")
		}
	})
}

func TestRunningExecutable(t *testing.T) {
	exe, err := runningExecutable()
	if err != nil {
		t.Fatalf("runningExecutable() error = %v", err)
	}
	if !filepath.IsAbs(exe) {
		t.Errorf("runningExecutable() = %q, want a full path", exe)
	}
}

// The two commands are reached by the names a user types, take no arguments,
// and report a failure to build what they run against.
func TestVersionCommands(t *testing.T) {
	fixture := &versionFixture{current: "1.3.2", latest: "1.4.0"}
	var depsErr error
	versionDepsFor = func() (versionDeps, error) { return fixture.deps(), depsErr }
	t.Cleanup(func() { versionDepsFor = defaultVersionDeps })

	t.Run("simple version", func(t *testing.T) {
		stdout, _, err := invokeCmd("version")
		if err != nil {
			t.Fatalf("simple version error = %v", err)
		}
		if !strings.Contains(stdout, "simple 1.3.2\n") || !strings.Contains(stdout, "simple version update") {
			t.Errorf("output = %q, want the version and how to update", stdout)
		}
	})

	t.Run("simple version update", func(t *testing.T) {
		fixture.installed = nil

		stdout, _, err := invokeCmd("version", "update")
		if err != nil {
			t.Fatalf("simple version update error = %v", err)
		}
		if !strings.Contains(stdout, "Updated simple 1.3.2 to 1.4.0") {
			t.Errorf("output = %q, want the update reported", stdout)
		}
		if len(fixture.installed) != 1 {
			t.Errorf("installed %v, want one install", fixture.installed)
		}
	})

	t.Run("neither takes an argument", func(t *testing.T) {
		if _, _, err := invokeCmd("version", "update", "1.4.0"); err == nil {
			t.Error("simple version update 1.4.0 error = nil, want a refusal")
		}
		if _, _, err := invokeCmd("version", "1.4.0"); err == nil {
			t.Error("simple version 1.4.0 error = nil, want a refusal")
		}
	})

	t.Run("a failure to build what they run against is the command's failure", func(t *testing.T) {
		depsErr = errVersionBroken
		t.Cleanup(func() { depsErr = nil })

		if _, _, err := invokeCmd("version"); !errors.Is(err, errVersionBroken) {
			t.Errorf("simple version error = %v, want %v", err, errVersionBroken)
		}
		if _, _, err := invokeCmd("version", "update"); !errors.Is(err, errVersionBroken) {
			t.Errorf("simple version update error = %v, want %v", err, errVersionBroken)
		}
	})
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"simple-cli/internal/fsx"
	"simple-cli/internal/home"
	"simple-cli/internal/selfupdate"
)

const (
	// updateCheckTimeout is the longest a command waits to hear whether a
	// newer release is out. It is paid at most once a day, after the command
	// has done its work, and a machine that is offline pays it and no more.
	updateCheckTimeout = 2 * time.Second

	// versionLookupTimeout is how long `simple version` and `simple version
	// update` wait for the list of releases. The user asked the question, so
	// it is given longer than the check nobody asked for.
	versionLookupTimeout = 15 * time.Second

	// updateDownloadTimeout bounds the download of the new program, which is
	// tens of megabytes over whatever connection the user has.
	updateDownloadTimeout = 10 * time.Minute

	// updateStateFile remembers, under the CLI's home, when GitHub was last
	// asked and what it said.
	updateStateFile = "update-check.json"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the CLI's version and whether a newer one is out",
	Long: `Show the version of this CLI and whether a newer one has been released.

Releases are read from https://github.com/simple-platform/simple-tools/releases.
To install the newest one, run 'simple version update'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		deps, err := versionDepsFor()
		if err != nil {
			return err
		}

		return runVersion(cmd.Context(), cmd.OutOrStdout(), deps)
	},
}

var versionUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Install the newest release of the CLI",
	Long: `Download the newest release of the CLI for this machine and replace the
running program with it.

The download is checked against the checksums published with the release
before anything on disk is changed. The program is replaced where it is
installed, so the account running this command needs to be able to write
there.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		deps, err := versionDepsFor()
		if err != nil {
			return err
		}

		return runVersionUpdate(cmd.Context(), cmd.OutOrStdout(), deps)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
	versionCmd.AddCommand(versionUpdateCmd)
}

// versionDeps is everything the version commands reach outside the process
// for, so a test can answer for GitHub and for the disk.
type versionDeps struct {
	current    string
	platform   string
	latest     func(ctx context.Context) (selfupdate.Release, error)
	install    func(ctx context.Context, release selfupdate.Release, exe string) error
	executable func() (string, error)
	record     func(latest selfupdate.Version)
}

// updater is the three parts of the update, built over the real network and
// the real disk.
type updater struct {
	finder    selfupdate.Finder
	checker   selfupdate.Checker
	installer selfupdate.Installer
}

func newUpdater() (updater, error) {
	homeDir, err := home.Dir()
	if err != nil {
		return updater{}, fmt.Errorf("failed to find the home directory: %w", err)
	}

	// The client carries no timeout of its own: each request is bounded by
	// the context of the command that makes it, and they differ.
	client := &http.Client{}
	agent := "simple-cli/" + Version
	finder := selfupdate.Finder{Client: client, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, UserAgent: agent}

	return updater{
		finder: finder,
		checker: selfupdate.Checker{
			FS:     fsx.OSFileSystem{},
			Path:   filepath.Join(homeDir, ".simple", updateStateFile),
			Now:    time.Now,
			Latest: finder.Latest,
		},
		installer: selfupdate.Installer{FS: fsx.OSFileSystem{}, Client: client, GOOS: runtime.GOOS, UserAgent: agent},
	}, nil
}

// versionDepsFor is where the two commands get what they run against. It is a
// variable so a test can run `simple version` through the command tree, names
// and arguments included, without the command reaching GitHub.
var versionDepsFor = defaultVersionDeps

func defaultVersionDeps() (versionDeps, error) {
	parts, err := newUpdater()
	if err != nil {
		return versionDeps{}, err
	}

	return versionDeps{
		current:    Version,
		platform:   runtime.GOOS + "/" + runtime.GOARCH,
		latest:     parts.finder.Latest,
		install:    parts.installer.Install,
		executable: runningExecutable,
		record:     parts.checker.Record,
	}, nil
}

// runningExecutable is the file this process was started from. A link to it is
// followed, because the file is what gets replaced: replacing the link would
// leave the program it points at as it was.
func runningExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to find the running program: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("failed to find the running program: %w", err)
	}

	return resolved, nil
}

// runVersion prints the running version and what GitHub says the newest is.
//
// A lookup that fails does not fail the command. The version is the answer
// that was asked for, and it is known without the network.
func runVersion(ctx context.Context, out io.Writer, deps versionDeps) error {
	ctx, cancel := context.WithTimeout(ctx, versionLookupTimeout)
	defer cancel()

	release, err := deps.latest(ctx)
	if err == nil {
		deps.record(release.Version)
	}

	current, released := selfupdate.ParseVersion(deps.current)
	newer := err == nil && released && release.Version.NewerThan(current)

	if jsonOutput {
		document := map[string]interface{}{"version": deps.current}
		if err == nil {
			document["latest"] = release.Version.String()
			document["update_available"] = newer
		}

		return printJSONTo(out, document)
	}

	_, _ = fmt.Fprintf(out, "simple %s\n", deps.current)

	switch {
	case err != nil:
		_, _ = fmt.Fprintf(out, "Could not check for a newer version: %v\n", err)
	case newer:
		_, _ = fmt.Fprint(out, updateNotice(current, release.Version))
	case released:
		_, _ = fmt.Fprintln(out, "This is the newest version.")
	default:
		_, _ = fmt.Fprintf(out, "The newest release is %s. Run `simple version update` to install it.\n", release.Version)
	}

	return nil
}

// runVersionUpdate installs the newest release over the running program,
// unless the running program is already that release or a later build.
func runVersionUpdate(ctx context.Context, out io.Writer, deps versionDeps) error {
	lookup, cancelLookup := context.WithTimeout(ctx, versionLookupTimeout)
	defer cancelLookup()

	release, err := deps.latest(lookup)
	if err != nil {
		return fmt.Errorf("failed to find the newest version: %w", err)
	}

	if current, released := selfupdate.ParseVersion(deps.current); released && !release.Version.NewerThan(current) {
		deps.record(release.Version)

		if jsonOutput {
			return printJSONTo(out, map[string]interface{}{"status": "current", "version": deps.current})
		}

		_, _ = fmt.Fprintf(out, "simple %s is up to date.\n", deps.current)

		return nil
	}

	exe, err := deps.executable()
	if err != nil {
		return err
	}

	if !jsonOutput {
		_, _ = fmt.Fprintf(out, "Downloading simple %s for %s...\n", release.Version, deps.platform)
	}

	download, cancelDownload := context.WithTimeout(ctx, updateDownloadTimeout)
	defer cancelDownload()

	if err := deps.install(download, release, exe); err != nil {
		return fmt.Errorf("failed to update simple: %w%s", err, permissionAdvice(err, exe, release))
	}

	deps.record(release.Version)

	if jsonOutput {
		return printJSONTo(out, map[string]interface{}{
			"status": "updated",
			"from":   deps.current,
			"to":     release.Version.String(),
		})
	}

	_, _ = fmt.Fprintf(out, "✅ Updated simple %s to %s\n", deps.current, release.Version)

	return nil
}

// permissionAdvice says what to do about the one failure a user can fix
// themselves: the program is installed where their account cannot write.
func permissionAdvice(err error, exe string, release selfupdate.Release) string {
	if !errors.Is(err, fs.ErrPermission) {
		return ""
	}

	return fmt.Sprintf(
		". This account cannot write to %s. Run the command again from one that can, or download the new version from %s",
		filepath.Dir(exe), release.PageURL(),
	)
}

// updateNotice is what a user is told when a newer release is out.
func updateNotice(current, latest selfupdate.Version) string {
	return fmt.Sprintf(
		"\nA new version of simple is available: %s (you have %s).\nRun `simple version update` to install it.\n",
		latest, current,
	)
}

// quietCommands are the commands that never look for a newer release.
//
// The version commands answer the question themselves. `help` is asked for by
// someone reading, not doing. And the completion commands are run by the
// shell, not by a person: `completion` each time a terminal opens, in a
// profile that sources its output, and `__complete` on every press of the
// Tab key, where a wait on the network is a terminal that stops responding.
var quietCommands = map[string]bool{
	"version":          true,
	"help":             true,
	"completion":       true,
	"__complete":       true,
	"__completeNoDesc": true,
}

// updateNoticeWanted says whether this run should look for a newer release and
// mention one.
//
// Only a person at a terminal is told. A notice written into the output of a
// script, a CI job or `--json` is read by a program that expected something
// else, and nobody is there to act on it.
func updateNoticeWanted(jsonMode, errIsTerminal bool, ciEnv, commandPath string) bool {
	if jsonMode || !errIsTerminal || ciEnabled(ciEnv) {
		return false
	}

	// A command's path is the names from the root down, as in
	// "simple version update".
	names := strings.Fields(commandPath)

	return len(names) < 2 || !quietCommands[names[1]]
}

// announceUpdate tells the user, after their command has run, that a newer
// release is out.
//
// It is given only a program that was released: one built from source carries
// no version to compare, and whoever built it knows where the source is.
func announceUpdate(errOut io.Writer, commandPath string) {
	current, released := selfupdate.ParseVersion(Version)
	if !released || !updateNoticeWanted(jsonOutput, outputIsTerminal(errOut), os.Getenv("CI"), commandPath) {
		return
	}

	parts, err := newUpdater()
	if err != nil {
		return
	}

	lookForUpdate(errOut, parts, current, runningExecutable)
}

// lookForUpdate asks whether a newer release is out and writes the notice if
// one is, waiting no longer than updateCheckTimeout for the answer.
func lookForUpdate(errOut io.Writer, parts updater, current selfupdate.Version, executable func() (string, error)) {
	// What an earlier update on Windows had to leave behind can be removed
	// now that the program it belonged to is no longer running.
	if exe, err := executable(); err == nil {
		parts.installer.Tidy(exe)
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	if latest, ok := parts.checker.Newer(ctx, current); ok {
		_, _ = fmt.Fprint(errOut, updateNotice(current, latest))
	}
}

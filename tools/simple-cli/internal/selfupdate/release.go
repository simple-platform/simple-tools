package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// repository is where the CLI is released. Its releases page is shared
	// with the other tools built here, which is why a release is recognised by
	// its tag and never taken to be "the latest release of the repository".
	repository = "simple-platform/simple-tools"

	// tagPrefix and tagSuffix are what the release workflow puts around a
	// version to name the CLI's tag: v1.4.0-simple-cli.
	tagPrefix = "v"
	tagSuffix = "-simple-cli"

	// tagRef is how a tag is written in a repository's list of references.
	tagRef = "refs/tags/"

	// checksumsAsset is the file every release carries beside its programs,
	// one line for each: the SHA-256 of the file, then its name.
	checksumsAsset = "simple-cli-checksums.txt"

	hostURL = "https://github.com"

	// maxRefs bounds the list of references. It holds a line for every
	// branch, tag and pull request of the repository, at about sixty bytes
	// each, so this is room for a quarter of a million of them.
	maxRefs = 16 << 20
)

var (
	// ErrNoRelease says the CLI has never been released.
	ErrNoRelease = errors.New("no release of the CLI was found")

	// ErrNoBuild says a release carries no program for this machine.
	ErrNoBuild = errors.New("the release has no build for this machine")
)

// Doer sends one request. *http.Client is one.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Release is one released version of the CLI, and the name of the file in it
// that is built for this machine.
type Release struct {
	Version Version
	Tag     string
	Asset   string
}

// AssetName is the name the release workflow gives the program built for an
// operating system and a processor: simple-cli-darwin-arm64,
// simple-cli-windows-amd64.exe.
//
// It is Go's own names for the two, so a machine works out the name of its
// file without a table, and a platform added to the workflow needs nothing
// added here.
func AssetName(goos, goarch string) string {
	name := "simple-cli-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}

	return name
}

// AssetURL is where the program for this machine is downloaded from.
func (r Release) AssetURL() string {
	return r.fileURL(r.Asset)
}

// ChecksumsURL is where the release's file of checksums is downloaded from.
func (r Release) ChecksumsURL() string {
	return r.fileURL(checksumsAsset)
}

// PageURL is the release's page, for a person to read or download from.
func (r Release) PageURL() string {
	return hostURL + "/" + repository + "/releases/tag/" + r.Tag
}

// fileURL is the address of one file of the release.
func (r Release) fileURL(name string) string {
	return hostURL + "/" + repository + "/releases/download/" + r.Tag + "/" + name
}

// Finder asks GitHub which release of the CLI is the newest.
type Finder struct {
	Client    Doer
	GOOS      string
	GOARCH    string
	UserAgent string
}

// refsURL is the address git itself asks for a repository's branches and tags.
//
// THE TAGS ARE READ THE WAY GIT READS THEM, NOT THROUGH GITHUB'S API. The API
// answers a machine that is not signed in sixty times an hour, counted for the
// whole address it shares with its network, and other tools on a developer's
// machine spend that allowance before the CLI asks. A check that fails for
// most of every hour is one nobody is told about, and an update that answers
// "try again later" is worse. This address has no allowance to run out, needs
// no sign-in for a public repository, and is the one `git ls-remote` uses.
func refsURL() string {
	return hostURL + "/" + repository + ".git/info/refs?service=git-upload-pack"
}

// Latest answers with the newest release of the CLI.
//
// A tag is created when its release is published, by the workflow that built
// the programs, so a tag that reads as a version of the CLI is a release.
func (f Finder) Latest(ctx context.Context) (Release, error) {
	refs, err := get(ctx, f.Client, refsURL(), f.UserAgent, maxRefs)
	if err != nil {
		return Release{}, fmt.Errorf("failed to list the releases: %w", err)
	}

	var newest Release
	found := false

	for _, tag := range tagsIn(string(refs)) {
		version, ok := versionOf(tag)
		if ok && (!found || version.NewerThan(newest.Version)) {
			newest, found = Release{Version: version, Tag: tag, Asset: AssetName(f.GOOS, f.GOARCH)}, true
		}
	}

	if !found {
		return Release{}, ErrNoRelease
	}

	return newest, nil
}

// versionOf reads a tag as a version of the CLI, or says it is not one:
// another tool's tag, or one whose middle is not three numbers.
func versionOf(tag string) (Version, bool) {
	if !strings.HasPrefix(tag, tagPrefix) || !strings.HasSuffix(tag, tagSuffix) {
		return Version{}, false
	}

	return ParseVersion(strings.TrimSuffix(strings.TrimPrefix(tag, tagPrefix), tagSuffix))
}

// tagsIn reads the names of the tags out of a list of references.
//
// The list is a line for each reference: a length, the commit it points at and
// its name, as in `004d2ca6...7f7ec refs/tags/v1.0.7-contextualizer`. Only the
// name is wanted, so a line is read from where `refs/tags/` begins and nothing
// before that is interpreted. Two things can follow a name and are dropped.
// A tag that carries a message is listed a second time with `^{}` after it,
// for the commit behind the tag. And the first line of the list carries what
// the server can do, after a zero byte.
func tagsIn(refs string) []string {
	var tags []string

	for _, line := range strings.Split(refs, "\n") {
		_, name, ok := strings.Cut(line, tagRef)
		if !ok {
			continue
		}

		name, _, _ = strings.Cut(name, "\x00")
		name = strings.TrimSuffix(strings.TrimSpace(name), "^{}")
		if name != "" {
			tags = append(tags, name)
		}
	}

	return tags
}

// get fetches one address and answers with its body, refusing one larger than
// limit so an answer that never ends cannot fill the machine's memory.
func get(ctx context.Context, client Doer, address, userAgent string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build the request for %s: %w", address, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach %s: %w", address, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", address, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read the answer from %s: %w", address, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("the answer from %s is larger than %d bytes", address, limit)
	}

	return body, nil
}

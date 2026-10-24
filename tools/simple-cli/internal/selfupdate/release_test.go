package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	macFile     = "simple-cli-darwin-arm64"
	windowsFile = "simple-cli-windows-amd64.exe"

	// refsAddress is the one address a Finder asks.
	refsAddress = "https://github.com/simple-platform/simple-tools.git/info/refs?service=git-upload-pack"
)

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"darwin", "arm64", macFile},
		{"windows", "amd64", windowsFile},
		{"linux", "amd64", "simple-cli-linux-amd64"},
		{"windows", "arm64", "simple-cli-windows-arm64.exe"},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			if got := AssetName(tt.goos, tt.goarch); got != tt.want {
				t.Errorf("AssetName(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestReleaseAddresses(t *testing.T) {
	release := Release{Version: Version{1, 4, 0}, Tag: "v1.4.0-simple-cli", Asset: macFile}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			"the program",
			release.AssetURL(),
			"https://github.com/simple-platform/simple-tools/releases/download/v1.4.0-simple-cli/simple-cli-darwin-arm64",
		},
		{
			"the checksums",
			release.ChecksumsURL(),
			"https://github.com/simple-platform/simple-tools/releases/download/v1.4.0-simple-cli/simple-cli-checksums.txt",
		},
		{
			"the page",
			release.PageURL(),
			"https://github.com/simple-platform/simple-tools/releases/tag/v1.4.0-simple-cli",
		},
		{"the list of tags", refsURL(), refsAddress},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestFinderLatest(t *testing.T) {
	tests := []struct {
		name        string
		said        answer
		failure     error
		goos        string
		goarch      string
		want        Release
		wantErr     string
		wantNothing bool
	}{
		{
			name: "the newest of the CLI's tags, among every other reference",
			said: answer{body: advertised(
				"HEAD",
				"refs/heads/main",
				"refs/heads/feat/simple-cli-something",
				"refs/pull/219/head",
				"refs/tags/v9.0.0-contextualizer",
				"refs/tags/v9.0.0-contextualizer^{}",
				"refs/tags/v1.4.0-simple-cli",
				"refs/tags/v1.10.0-simple-cli",
				"refs/tags/v1.10.0-simple-cli^{}",
				"refs/tags/v1.9.0-simple-cli",
				"refs/tags/v3.0.0-scl-parser-cli",
			)},
			goos: "darwin", goarch: "arm64",
			want: Release{Version: Version{1, 10, 0}, Tag: "v1.10.0-simple-cli", Asset: macFile},
		},
		{
			name: "the file is the one for the machine that asks",
			said: answer{body: advertised("HEAD", "refs/tags/v1.4.0-simple-cli")},
			goos: "windows", goarch: "amd64",
			want: Release{Version: Version{1, 4, 0}, Tag: "v1.4.0-simple-cli", Asset: windowsFile},
		},
		{
			name: "tags that are not a version of the CLI are not releases",
			said: answer{body: advertised(
				"HEAD",
				"refs/tags/v3.0.0-rc1-simple-cli",
				"refs/tags/v2.x-simple-cli",
				"refs/tags/2.4.0-simple-cli",
				"refs/tags/v2.3.0",
				"refs/tags/v2.2.0-simple-cli-nightly",
				"refs/tags/v1.4.0-simple-cli",
			)},
			goos: "darwin", goarch: "arm64",
			want: Release{Version: Version{1, 4, 0}, Tag: "v1.4.0-simple-cli", Asset: macFile},
		},
		{
			name: "a repository whose first reference is a tag",
			said: answer{body: advertised("refs/tags/v1.4.0-simple-cli")},
			goos: "darwin", goarch: "arm64",
			want: Release{Version: Version{1, 4, 0}, Tag: "v1.4.0-simple-cli", Asset: macFile},
		},
		{
			name: "a CLI that has never been released",
			said: answer{body: advertised("HEAD", "refs/heads/main", "refs/tags/v9.0.0-contextualizer")},
			goos: "darwin", goarch: "arm64",
			wantErr:     "no release was found",
			wantNothing: true,
		},
		{
			name: "an answer with no references at all",
			said: answer{body: ""},
			goos: "darwin", goarch: "arm64",
			wantErr:     "no release was found",
			wantNothing: true,
		},
		{
			name:    "GitHub cannot be reached",
			failure: errBroken,
			goos:    "darwin", goarch: "arm64",
			wantErr: "failed to list the releases: failed to reach " + refsAddress + ": broken on purpose",
		},
		{
			name: "GitHub refuses",
			said: answer{status: http.StatusServiceUnavailable},
			goos: "darwin", goarch: "arm64",
			wantErr: "failed to list the releases: " + refsAddress + " answered 503 Service Unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			github := &fakeGitHub{answers: map[string]answer{refsAddress: tt.said}, failure: tt.failure}
			finder := Finder{Client: github, GOOS: tt.goos, GOARCH: tt.goarch, UserAgent: "simple-cli/test"}

			got, err := finder.Latest(context.Background())

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Latest() error = %v, want one containing %q", err, tt.wantErr)
				}
				if errors.Is(err, ErrNoRelease) != tt.wantNothing {
					t.Errorf("errors.Is(err, ErrNoRelease) = %v, want %v", !tt.wantNothing, tt.wantNothing)
				}
			} else if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Latest() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// One request, to the address git itself uses, saying who asks and carrying no
// sign-in: the releases are public and GitHub's API is not involved.
func TestFinderLatest_AsksOnceAndWithoutASignIn(t *testing.T) {
	github := &fakeGitHub{answers: map[string]answer{
		refsAddress: {body: advertised("HEAD", "refs/tags/v1.4.0-simple-cli")},
	}}
	finder := Finder{Client: github, GOOS: "darwin", GOARCH: "arm64", UserAgent: "simple-cli/1.3.2"}

	if _, err := finder.Latest(context.Background()); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}

	if len(github.asked) != 1 {
		t.Fatalf("GitHub was asked %d times, want 1", len(github.asked))
	}
	asked := github.asked[0]
	if asked.URL.String() != refsAddress {
		t.Errorf("asked %q, want %q", asked.URL, refsAddress)
	}
	if asked.URL.Host != "github.com" {
		t.Errorf("asked the host %q, want github.com and not its API", asked.URL.Host)
	}
	if got := asked.Header.Get("User-Agent"); got != "simple-cli/1.3.2" {
		t.Errorf("User-Agent = %q, want simple-cli/1.3.2", got)
	}
	if got := asked.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none", got)
	}
}

// Each tool released here has a suffix of its own, and one that ends another
// is not mistaken for it.
func TestNewestTagged(t *testing.T) {
	refs := advertised(
		"HEAD",
		"refs/tags/v1.0.1-scl-parser",
		"refs/tags/v1.0.2-scl-parser-cli",
		"refs/tags/v1.0.3-scl-parser-cli",
		"refs/tags/v1.0.7-contextualizer",
		"refs/tags/v2.0.0-simple-cli",
	)

	tests := []struct {
		suffix  string
		want    Version
		wantTag string
		wantErr error
	}{
		{suffix: "-scl-parser-cli", want: Version{1, 0, 3}, wantTag: "v1.0.3-scl-parser-cli"},
		{suffix: "-scl-parser", want: Version{1, 0, 1}, wantTag: "v1.0.1-scl-parser"},
		{suffix: "-contextualizer", want: Version{1, 0, 7}, wantTag: "v1.0.7-contextualizer"},
		{suffix: "-simple-cli", want: Version{2, 0, 0}, wantTag: "v2.0.0-simple-cli"},
		{suffix: "-cli", wantErr: ErrNoRelease},
		{suffix: "-never-released", wantErr: ErrNoRelease},
	}

	for _, tt := range tests {
		t.Run(tt.suffix, func(t *testing.T) {
			github := &fakeGitHub{answers: map[string]answer{refsAddress: {body: refs}}}

			got, tag, err := NewestTagged(context.Background(), github, "simple-cli/test", tt.suffix)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewestTagged() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want || tag != tt.wantTag {
				t.Errorf("NewestTagged() = %v, %q; want %v, %q", got, tag, tt.want, tt.wantTag)
			}
		})
	}
}

func TestTagsIn(t *testing.T) {
	tests := []struct {
		name string
		refs string
		want []string
	}{
		{name: "nothing", refs: "", want: nil},
		{
			name: "tags among branches and pull requests",
			refs: advertised("HEAD", "refs/heads/main", "refs/tags/v1.0.0-a", "refs/pull/3/merge", "refs/tags/v2.0.0-b"),
			want: []string{"v1.0.0-a", "v2.0.0-b"},
		},
		{
			name: "a tag with a message is listed twice, and read as one name both times",
			refs: advertised("HEAD", "refs/tags/v1.0.0-a", "refs/tags/v1.0.0-a^{}"),
			want: []string{"v1.0.0-a", "v1.0.0-a"},
		},
		{
			name: "what the server can do is not part of a name",
			refs: advertised("refs/tags/v1.0.0-a"),
			want: []string{"v1.0.0-a"},
		},
		{
			name: "lines that end the Windows way",
			refs: "004dabc refs/tags/v1.0.0-a\r\n004dabc refs/tags/v2.0.0-b\r\n",
			want: []string{"v1.0.0-a", "v2.0.0-b"},
		},
		{name: "a line that names no tag after all", refs: "0010abc refs/tags/\n", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tagsIn(tt.refs); !slices.Equal(got, tt.want) {
				t.Errorf("tagsIn() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGet(t *testing.T) {
	const address = "https://github.com/a-file"

	tests := []struct {
		name    string
		address string
		said    answer
		limit   int64
		want    string
		wantErr string
	}{
		{name: "an answer within the limit", address: address, said: answer{body: "12345"}, limit: 5, want: "12345"},
		{
			name: "an answer over the limit", address: address, said: answer{body: "123456"}, limit: 5,
			wantErr: "the answer from " + address + " is larger than 5 bytes",
		},
		{
			name: "an answer that breaks off", address: address, said: answer{readErr: errBroken}, limit: 5,
			wantErr: "failed to read the answer from " + address + ": broken on purpose",
		},
		{
			name: "a refusal", address: address, said: answer{status: http.StatusForbidden}, limit: 5,
			wantErr: address + " answered 403 Forbidden",
		},
		{
			name: "an address that is not one", address: "https://github.com/\x7f", limit: 5,
			wantErr: "failed to build the request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			github := &fakeGitHub{answers: map[string]answer{tt.address: tt.said}}

			body, err := get(context.Background(), github, tt.address, "simple-cli/test", tt.limit)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("get() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("get() error = %v", err)
			}
			if string(body) != tt.want {
				t.Errorf("get() = %q, want %q", body, tt.want)
			}
		})
	}
}

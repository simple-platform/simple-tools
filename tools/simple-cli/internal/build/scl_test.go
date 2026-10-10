package build

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// answering stands in for GitHub: it answers every request with one status
// and body, or fails to answer at all.
type answering struct {
	status  int
	body    string
	failure error
	asked   []string
}

func (a *answering) Do(req *http.Request) (*http.Response, error) {
	a.asked = append(a.asked, req.URL.String())
	if a.failure != nil {
		return nil, a.failure
	}

	return &http.Response{
		StatusCode: a.status,
		Status:     http.StatusText(a.status),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(a.body)),
	}, nil
}

// asGitHub puts a stand-in where the releases are asked for, and puts back
// what was there.
func asGitHub(t *testing.T, github *answering) {
	t.Helper()

	previous := sclParserReleases
	sclParserReleases = github
	t.Cleanup(func() { sclParserReleases = previous })
}

func TestFetchSCLParserVersion(t *testing.T) {
	// The repository's references as git is told them: every tool's tags, of
	// which only scl-parser's are read.
	refs := "001e# service=git-upload-pack\n0000" +
		"0041aaaa refs/tags/v1.0.1-scl-parser\n" +
		"0045aaaa refs/tags/v1.0.3-scl-parser-cli\n" +
		"0046aaaa refs/tags/v1.10.0-scl-parser-cli\n" +
		"0045aaaa refs/tags/v1.9.0-scl-parser-cli\n" +
		"0041aaaa refs/tags/v9.0.0-simple-cli\n0000"

	tests := []struct {
		name    string
		github  answering
		want    string
		wantErr string
	}{
		{
			name:   "the newest tag of scl-parser, and no other tool's",
			github: answering{status: http.StatusOK, body: refs},
			want:   "1.10.0",
		},
		{
			name:    "no release of scl-parser",
			github:  answering{status: http.StatusOK, body: "0041aaaa refs/tags/v9.0.0-simple-cli\n"},
			wantErr: "failed to find the newest scl-parser: no release was found",
		},
		{
			name:    "GitHub refuses",
			github:  answering{status: http.StatusNotFound},
			wantErr: "failed to find the newest scl-parser: failed to list the releases",
		},
		{
			name:    "GitHub cannot be reached",
			github:  answering{failure: errors.New("no network")},
			wantErr: "failed to find the newest scl-parser: failed to list the releases",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			github := tt.github
			asGitHub(t, &github)

			version, err := fetchSCLParserVersion()

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("fetchSCLParserVersion() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchSCLParserVersion() error = %v", err)
			}
			if version != tt.want {
				t.Errorf("fetchSCLParserVersion() = %q, want %q", version, tt.want)
			}

			const want = "https://github.com/simple-platform/simple-tools.git/info/refs?service=git-upload-pack"
			if len(github.asked) != 1 || github.asked[0] != want {
				t.Errorf("asked %v, want one request to %s", github.asked, want)
			}
		})
	}
}

func TestMapSCLPlatform(t *testing.T) {
	tests := []struct {
		platform string
		arch     string
		want     string
	}{
		{"macos", "aarch64", "macos-silicon"},
		{"macos", "x86_64", "macos"},
		{"linux", "aarch64", "linux-arm64"},
		{"linux", "x86_64", "linux"},
		{"windows", "x86_64", "windows.exe"},
		{"unknown", "unknown", "unknown"},
	}

	for _, tt := range tests {
		got := mapSCLPlatform(tt.platform, tt.arch)
		if got != tt.want {
			t.Errorf("mapSCLPlatform(%s, %s) = %s, want %s", tt.platform, tt.arch, got, tt.want)
		}
	}
}

func TestNormalizeActionName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-action", "my_action"},
		{"action_name", "action_name"},
		{"mixed-separators_here", "mixed_separators_here"},
	}

	for _, tt := range tests {
		if got := NormalizeActionName(tt.input); got != tt.want {
			t.Errorf("NormalizeActionName(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

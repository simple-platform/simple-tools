package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// answer is what the fake GitHub says at one address.
type answer struct {
	status  int
	body    string
	readErr error
}

// fakeGitHub answers each address with what a test put there and remembers
// what it was asked, so no test reaches the network.
type fakeGitHub struct {
	answers map[string]answer
	asked   []*http.Request
	failure error
}

func (f *fakeGitHub) Do(req *http.Request) (*http.Response, error) {
	f.asked = append(f.asked, req)
	if f.failure != nil {
		return nil, f.failure
	}

	said, ok := f.answers[req.URL.String()]
	if !ok {
		said = answer{status: http.StatusNotFound}
	}
	if said.status == 0 {
		said.status = http.StatusOK
	}

	var body io.Reader = strings.NewReader(said.body)
	if said.readErr != nil {
		body = failingReader{err: said.readErr}
	}

	return &http.Response{
		StatusCode: said.status,
		Status:     fmt.Sprintf("%d %s", said.status, http.StatusText(said.status)),
		Header:     http.Header{},
		Body:       io.NopCloser(body),
	}, nil
}

// failingReader is a body that breaks off.
type failingReader struct {
	err error
}

func (r failingReader) Read([]byte) (int, error) {
	return 0, r.err
}

// advertised writes a repository's references the way GitHub answers git: a
// header, then a line for each reference carrying its length, the commit it
// points at and its name, the first with what the server can do after a zero
// byte.
func advertised(refs ...string) string {
	const commit = "2ca65ed905220566b11b06cb0d5f7a595507f7ec"

	line := func(payload string) string {
		return fmt.Sprintf("%04x%s", len(payload)+4, payload)
	}

	var list strings.Builder
	list.WriteString(line("# service=git-upload-pack\n"))
	list.WriteString("0000")
	for i, ref := range refs {
		payload := commit + " " + ref
		if i == 0 {
			payload += "\x00multi_ack thin-pack side-band-64k symref=HEAD:refs/heads/main agent=git/github"
		}
		list.WriteString(line(payload + "\n"))
	}
	list.WriteString("0000")

	return list.String()
}

// errBroken is the failure tests hand to whatever they want to see fail.
var errBroken = errors.New("broken on purpose")

//go:build integration

package selfupdate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"simple-cli/internal/fsx"
)

// THIS TEST REPLACES A PROGRAM THAT IS RUNNING, ON A REAL DISK.
//
// It is the one thing the unit tests cannot say: whether the operating system
// lets the swap happen. Windows refuses to overwrite or delete the file of a
// running program and allows it to be renamed, and macOS ties a program to the
// file it was started from. Both are behaviour of the system, so both are
// asked of the system, on each one the CLI is released for.
//
// It is kept out of the ordinary run by its build tag, because the ordinary
// tests touch neither the disk nor a socket. Run it with:
//
//	go test -tags integration -run TestUpdateReplacesARunningProgram ./internal/selfupdate/
//
// The program it replaces is this test binary, copied and started, and the
// "release" it installs is the same binary served by a server on this machine
// standing in for GitHub. Nothing leaves the machine.

const holdVariable = "SELFUPDATE_TEST_HOLD"

// TestHoldForUpdate is the running program. Started with the variable set, it
// says it is up and then waits for its input to close, which is how the test
// keeps it alive across the swap and ends it afterwards.
func TestHoldForUpdate(t *testing.T) {
	if os.Getenv(holdVariable) != "1" {
		t.Skip("only run as the program being replaced")
	}

	_, _ = os.Stdout.WriteString("ready\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// toLocal sends every request to one local server, keeping its path, so the
// code under test asks for GitHub's addresses and is answered here.
type toLocal struct {
	server *url.URL
}

func (l toLocal) Do(req *http.Request) (*http.Response, error) {
	redirected := req.Clone(req.Context())
	redirected.URL.Scheme = l.server.Scheme
	redirected.URL.Host = l.server.Host
	redirected.Host = l.server.Host

	return http.DefaultClient.Do(redirected)
}

func TestUpdateReplacesARunningProgram(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to find this test binary: %v", err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("failed to read this test binary: %v", err)
	}

	exe := filepath.Join(t.TempDir(), "simple")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, program, fsx.ExecPerm); err != nil {
		t.Fatalf("failed to install the program to be replaced: %v", err)
	}
	before, err := os.Stat(exe)
	if err != nil {
		t.Fatalf("failed to look at the installed program: %v", err)
	}
	// On Windows a file's identity is not read when the file is looked at. It
	// is read from the path the first time it is compared, which here would be
	// after the swap, when the path names the new program. Comparing the file
	// with itself now settles its identity as the program installed first.
	if !os.SameFile(before, before) {
		t.Fatal("the installed program is not the same file as itself")
	}

	// The program is started from the file that is about to be replaced.
	running := exec.Command(exe, "-test.run=^TestHoldForUpdate$")
	running.Env = append(os.Environ(), holdVariable+"=1")
	stdin, err := running.StdinPipe()
	if err != nil {
		t.Fatalf("failed to open the running program's input: %v", err)
	}
	stdout, err := running.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to open the running program's output: %v", err)
	}
	if err := running.Start(); err != nil {
		t.Fatalf("failed to start the program to be replaced: %v", err)
	}
	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		_ = stdin.Close()

		return running.Wait()
	}
	t.Cleanup(func() { _ = stop() })

	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		if lines.Text() == "ready" {
			break
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatalf("the program to be replaced did not come up: %v", err)
	}

	// A release, as GitHub would list and serve it.
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	const tag = "v9.9.9-simple-cli"
	sum := sha256.Sum256(program)
	github := http.NewServeMux()
	github.HandleFunc("/"+repository+".git/info/refs", func(w http.ResponseWriter, _ *http.Request) {
		ref := "2ca65ed905220566b11b06cb0d5f7a595507f7ec " + tagRef + tag + "\n"
		_, _ = fmt.Fprintf(w, "001e# service=git-upload-pack\n0000%04x%s0000", len(ref)+4, ref)
	})
	github.HandleFunc("/"+repository+"/releases/download/"+tag+"/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(program)
	})
	github.HandleFunc("/"+repository+"/releases/download/"+tag+"/"+checksumsAsset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"))
	})
	server := httptest.NewServer(github)
	defer server.Close()
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to read the local server's address: %v", err)
	}
	client := toLocal{server: address}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	release, err := Finder{Client: client, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, UserAgent: "simple-cli/test"}.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if release.Version != (Version{9, 9, 9}) {
		t.Fatalf("Latest() = %v, want 9.9.9", release.Version)
	}

	installer := Installer{FS: fsx.OSFileSystem{}, Client: client, GOOS: runtime.GOOS, UserAgent: "simple-cli/test"}
	if err := installer.Install(ctx, release, exe); err != nil {
		t.Fatalf("Install() over a running program error = %v", err)
	}

	// The name now belongs to a different file, and nothing is left beside it
	// but what Windows could not let go of.
	after, err := os.Stat(exe)
	if err != nil {
		t.Fatalf("the program is gone after the update: %v", err)
	}
	if os.SameFile(before, after) {
		t.Error("the program is the same file as before: it was written into, not replaced")
	}
	if _, err := os.Stat(exe + freshSuffix); !os.IsNotExist(err) {
		t.Errorf("the downloaded file was left beside the program: %v", err)
	}
	aside, asideErr := os.Stat(exe + asideSuffix)
	if runtime.GOOS == "windows" {
		if asideErr != nil || !os.SameFile(before, aside) {
			t.Errorf("on Windows the running program should have been moved aside: %v", asideErr)
		}
	} else if !os.IsNotExist(asideErr) {
		t.Errorf("a file was moved aside where nothing needs to be: %v", asideErr)
	}

	// The new program starts, while the old one is still running.
	if output, err := exec.Command(exe, "-test.run=^$").CombinedOutput(); err != nil {
		t.Errorf("the installed program does not run: %v\n%s", err, output)
	}

	// The old one was not disturbed: it ends when asked, and cleanly.
	if err := stop(); err != nil {
		t.Errorf("the program that was running through the update ended badly: %v", err)
	}

	// Once it has ended, what it was started from can be removed. Windows
	// lets go of a program's file a moment after the process is gone, so the
	// removal is given a few seconds rather than one try.
	deadline := time.Now().Add(5 * time.Second)
	for {
		installer.Tidy(exe)
		_, err := os.Stat(exe + asideSuffix)
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("the program moved aside is still there after Tidy: %v", err)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

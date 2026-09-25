package cli

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"simple-cli/internal/config"
	"simple-cli/internal/deploy"
	"simple-cli/internal/ui"
)

// recordingReporter records every report as one readable string.
type recordingReporter struct {
	mu    sync.Mutex
	calls []string
}

var _ ui.StepReporter = (*recordingReporter)(nil)

func (r *recordingReporter) record(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
}

func (r *recordingReporter) Start(id ui.StepID, detail string) { r.record("start %s %s", id, detail) }
func (r *recordingReporter) Detail(id ui.StepID, detail string) {
	r.record("detail %s %s", id, detail)
}
func (r *recordingReporter) Note(id ui.StepID, text string) { r.record("note %s %s", id, text) }
func (r *recordingReporter) Progress(id ui.StepID, p ui.Progress) {
	r.record("progress %s %d/%d %d/%d", id, p.Items, p.ItemsTotal, p.Bytes, p.BytesTotal)
}
func (r *recordingReporter) Done(id ui.StepID, detail string) { r.record("done %s %s", id, detail) }
func (r *recordingReporter) Skip(id ui.StepID, reason string) { r.record("skip %s %s", id, reason) }
func (r *recordingReporter) Fail(id ui.StepID, err error)     { r.record("fail %s %v", id, err) }

func (r *recordingReporter) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// fakeAuthenticator hands out tokens in order and records cache clears.
// Each sign-in takes wait.
type fakeAuthenticator struct {
	mu       sync.Mutex
	wait     time.Duration
	tokens   []string
	errs     []error
	calls    int
	clearErr error
	cleared  []string
}

func (a *fakeAuthenticator) GetJWT(_ context.Context, _, _, _ string) (string, error) {
	time.Sleep(a.wait)
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.calls
	a.calls++
	if i < len(a.errs) && a.errs[i] != nil {
		return "", a.errs[i]
	}
	if i < len(a.tokens) {
		return a.tokens[i], nil
	}
	return fmt.Sprintf("jwt-%d", i+1), nil
}

func (a *fakeAuthenticator) ClearCache(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cleared = append(a.cleared, key)
	return a.clearErr
}

// fakeDevopsClient answers each call with its func, or succeeds.
type fakeDevopsClient struct {
	join     func(ctx context.Context, appID string) error
	manifest func(ctx context.Context, files map[string]deploy.FileInfo, version string) ([]string, error)
	upload   func(ctx context.Context, files map[string]deploy.FileInfo, needed []string, onProgress func(deploy.UploadProgress)) error
	publish  func(ctx context.Context) (*deploy.DeployResult, error)
	install  func(ctx context.Context) (*deploy.InstallResult, error)
	closed   atomic.Int32
}

var _ devopsClient = (*fakeDevopsClient)(nil)

func (c *fakeDevopsClient) JoinChannel(ctx context.Context, appID string) error {
	if c.join != nil {
		return c.join(ctx, appID)
	}
	return nil
}

func (c *fakeDevopsClient) SendManifest(ctx context.Context, files map[string]deploy.FileInfo, version string) ([]string, error) {
	if c.manifest != nil {
		return c.manifest(ctx, files, version)
	}
	return nil, nil
}

func (c *fakeDevopsClient) SendFiles(ctx context.Context, files map[string]deploy.FileInfo, needed []string, onProgress func(deploy.UploadProgress)) error {
	if c.upload != nil {
		return c.upload(ctx, files, needed, onProgress)
	}
	return nil
}

func (c *fakeDevopsClient) Deploy(ctx context.Context) (*deploy.DeployResult, error) {
	if c.publish != nil {
		return c.publish(ctx)
	}
	return &deploy.DeployResult{AppID: "com.acme.crm", Version: "1.0.0"}, nil
}

func (c *fakeDevopsClient) Install(ctx context.Context) (*deploy.InstallResult, error) {
	if c.install != nil {
		return c.install(ctx)
	}
	return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.0.0", Success: true}, nil
}

func (c *fakeDevopsClient) Close() { c.closed.Add(1) }

// unsetKeyVar names the variable preview's API key comes from. Tests that
// use preview clear it.
const unsetKeyVar = "SIMPLE_CLI_TEST_UNSET_KEY"

// testSCL is a simple.scl with two environments at acme.simple.dev: dev,
// whose API key is set, and preview, whose key comes from unsetKeyVar.
func testSCL() *config.SimpleSCL {
	return &config.SimpleSCL{
		Tenant: "acme",
		Environments: map[string]*config.Environment{
			"dev":     {Name: "dev", Endpoint: "acme.simple.dev", APIKey: "si_key"},
			"preview": {Name: "preview", Endpoint: "acme.simple.dev", APIKey: "$" + unsetKeyVar},
		},
	}
}

// fakeDevopsDeps returns deps that load testSCL, sign in with auth and dial
// client, recording each dial's JWT.
func fakeDevopsDeps(auth *fakeAuthenticator, dials *[]string, dial func(n int) (devopsClient, error)) devopsDeps {
	var mu sync.Mutex
	return devopsDeps{
		ensureParser:     func(func(string)) (string, error) { return "/fake/scl-parser", nil },
		loadConfig:       func(string, func(string)) (*config.SimpleSCL, error) { return testSCL(), nil },
		newAuthenticator: func() devopsAuthenticator { return auth },
		dial: func(_ context.Context, endpoint, jwt string) (devopsClient, error) {
			mu.Lock()
			*dials = append(*dials, endpoint+" "+jwt)
			n := len(*dials)
			mu.Unlock()
			return dial(n)
		},
	}
}

func TestLoadDevopsTarget(t *testing.T) {
	tests := []struct {
		name      string
		ensure    func(onStatus func(string)) (string, error)
		load      func(string, func(string)) (*config.SimpleSCL, error)
		env       string
		wantErr   string
		wantCalls []string
	}{
		{
			name:   "loads the environment",
			ensure: func(func(string)) (string, error) { return "/bin/scl-parser", nil },
			env:    "dev",
		},
		{
			name: "a download is noted once and its status shown as details",
			ensure: func(onStatus func(string)) (string, error) {
				onStatus("Downloading...")
				onStatus("Downloading 43%...")
				onStatus("Downloading 100%...")
				return "/bin/scl-parser", nil
			},
			env: "dev",
			wantCalls: []string{
				"note config downloading scl-parser",
				"detail config scl-parser: downloading",
				"detail config scl-parser: downloading 43%",
				"detail config scl-parser: downloading 100%",
			},
		},
		{
			name:   "a .env warning becomes a note",
			ensure: func(func(string)) (string, error) { return "/bin/scl-parser", nil },
			load: func(_ string, warn func(string)) (*config.SimpleSCL, error) {
				warn("warning: failed to load .env file .env, continuing without it: unterminated quoted value")
				return testSCL(), nil
			},
			env:       "dev",
			wantCalls: []string{"note config warning: failed to load .env file .env, continuing without it: unterminated quoted value"},
		},
		{
			name:    "parser error",
			ensure:  func(func(string)) (string, error) { return "", errors.New("offline") },
			env:     "dev",
			wantErr: "failed to ensure scl-parser: offline",
		},
		{
			name:   "config error",
			ensure: func(func(string)) (string, error) { return "/bin/scl-parser", nil },
			load: func(string, func(string)) (*config.SimpleSCL, error) {
				return nil, errors.New("simple.scl not found in .")
			},
			env:     "dev",
			wantErr: "failed to load simple.scl: simple.scl not found in .",
		},
		{
			name:    "missing environment",
			ensure:  func(func(string)) (string, error) { return "/bin/scl-parser", nil },
			env:     "prod",
			wantErr: "environment 'prod' not defined in simple.scl",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &fakeAuthenticator{}
			deps := devopsDeps{
				ensureParser:     tt.ensure,
				loadConfig:       tt.load,
				newAuthenticator: func() devopsAuthenticator { return auth },
			}
			if deps.loadConfig == nil {
				deps.loadConfig = func(parserPath string, _ func(string)) (*config.SimpleSCL, error) {
					if parserPath != "/bin/scl-parser" {
						t.Errorf("loadConfig got parser %q", parserPath)
					}
					return testSCL(), nil
				}
			}
			steps := &recordingReporter{}

			target, err := loadDevopsTarget(deps, tt.env, steps, configWarning(progressPlain, steps))
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadDevopsTarget() error = %v", err)
			}
			if target.parserPath != "/bin/scl-parser" || target.envName != "dev" || target.tenantEnvKey != "acme::dev" {
				t.Errorf("target = %+v", target)
			}
			if target.host() != "devops.acme.simple.dev" || target.auth != auth {
				t.Errorf("host = %q, auth = %v", target.host(), target.auth)
			}
			if got := steps.got(); !reflect.DeepEqual(got, tt.wantCalls) && len(got)+len(tt.wantCalls) > 0 {
				t.Errorf("reports = %q, want %q", got, tt.wantCalls)
			}
		})
	}
}

func TestConfigWarning(t *testing.T) {
	// Under --json there is no view to note a warning in, so it must be left
	// to the loader, which writes it to stderr; with a view it is a note on
	// the config step, where it cannot tear a live frame.
	tests := []struct {
		name      string
		mode      progressMode
		wantNil   bool
		wantCalls []string
	}{
		{name: "--json leaves it to the loader", mode: progressNone, wantNil: true},
		{name: "plain notes it", mode: progressPlain, wantCalls: []string{"note config warning: bad .env"}},
		{name: "tty notes it", mode: progressTTY, wantCalls: []string{"note config warning: bad .env"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := &recordingReporter{}
			warn := configWarning(tt.mode, steps)
			if (warn == nil) != tt.wantNil {
				t.Fatalf("configWarning() nil = %v, want %v", warn == nil, tt.wantNil)
			}
			if warn != nil {
				warn("warning: bad .env")
			}
			if got := steps.got(); !reflect.DeepEqual(got, tt.wantCalls) && len(got)+len(tt.wantCalls) > 0 {
				t.Errorf("reports = %q, want %q", got, tt.wantCalls)
			}
		})
	}
}

func TestDevopsTarget_APIKeyIsCheckedWhenSigningIn(t *testing.T) {
	// A deploy dry run loads the target but never signs in, so a missing
	// API key must not fail the load, only the sign-in.
	t.Setenv(unsetKeyVar, "")
	auth := &fakeAuthenticator{}
	var dials []string
	target, err := loadDevopsTarget(fakeDevopsDeps(auth, &dials, nil), "preview", ui.NopReporter{}, nil)
	if err != nil {
		t.Fatalf("loadDevopsTarget() error = %v; the key is not needed yet", err)
	}
	if target.host() != "devops.acme.simple.dev" || target.cfg.Tenant != "acme" {
		t.Errorf("target = %+v", target)
	}

	err = target.authenticate(context.Background())
	if want := "environment variable " + unsetKeyVar + " not set"; err == nil || err.Error() != want {
		t.Fatalf("authenticate() error = %v, want %q", err, want)
	}
	if auth.calls != 0 {
		t.Errorf("signed in %d times without a key", auth.calls)
	}
}

func TestParserStatus(t *testing.T) {
	tests := map[string]string{
		"Downloading...":     "downloading",
		"Downloading 43%...": "downloading 43%",
		" Verifying ":        "verifying",
	}
	for in, want := range tests {
		if got := parserStatus(in); got != want {
			t.Errorf("parserStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDevopsTargetAuthenticate(t *testing.T) {
	tests := []struct {
		name    string
		auth    *fakeAuthenticator
		wantJWT string
		wantErr string
	}{
		{"caches the token", &fakeAuthenticator{tokens: []string{"jwt-a"}}, "jwt-a", ""},
		{"wraps the failure", &fakeAuthenticator{errs: []error{errors.New("401")}}, "", "authentication failed: 401"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dials []string
			target, err := loadDevopsTarget(fakeDevopsDeps(tt.auth, &dials, nil), "dev", ui.NopReporter{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = target.authenticate(context.Background())
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || target.jwt != tt.wantJWT {
				t.Errorf("jwt = %q, err = %v; want %q", target.jwt, err, tt.wantJWT)
			}
		})
	}
}

func TestDevopsTargetConnect(t *testing.T) {
	rejected := &deploy.AuthFailedError{StatusCode: 401}
	const reauthNote = "note connect server rejected the saved session; signing in again"
	tests := []struct {
		name      string
		auth      *fakeAuthenticator
		dial      func(n int, client *fakeDevopsClient) (devopsClient, error)
		joinErr   error
		wantErr   string
		wantDials []string
		wantNotes []string
		wantClear bool
		wantClose int32
	}{
		{
			name:      "connects with the cached token",
			auth:      &fakeAuthenticator{},
			dial:      func(_ int, c *fakeDevopsClient) (devopsClient, error) { return c, nil },
			wantDials: []string{"devops.acme.simple.dev jwt-1"},
		},
		{
			name: "a rejected token is refreshed once",
			auth: &fakeAuthenticator{},
			dial: func(n int, c *fakeDevopsClient) (devopsClient, error) {
				if n == 1 {
					return nil, rejected
				}
				return c, nil
			},
			wantDials: []string{"devops.acme.simple.dev jwt-1", "devops.acme.simple.dev jwt-2"},
			wantNotes: []string{reauthNote},
			wantClear: true,
		},
		{
			name:      "clearing the cache fails",
			auth:      &fakeAuthenticator{clearErr: errors.New("read-only home")},
			dial:      func(int, *fakeDevopsClient) (devopsClient, error) { return nil, rejected },
			wantErr:   "failed to clear token cache: read-only home",
			wantDials: []string{"devops.acme.simple.dev jwt-1"},
			wantNotes: []string{reauthNote},
			wantClear: true,
		},
		{
			name:      "signing in again fails",
			auth:      &fakeAuthenticator{errs: []error{nil, errors.New("key revoked")}},
			dial:      func(int, *fakeDevopsClient) (devopsClient, error) { return nil, rejected },
			wantErr:   "re-authentication failed: key revoked",
			wantDials: []string{"devops.acme.simple.dev jwt-1"},
			wantNotes: []string{reauthNote},
			wantClear: true,
		},
		{
			name:      "the second dial fails",
			auth:      &fakeAuthenticator{},
			dial:      func(int, *fakeDevopsClient) (devopsClient, error) { return nil, rejected },
			wantErr:   "connection failed after token refresh: websocket auth failed: 401",
			wantDials: []string{"devops.acme.simple.dev jwt-1", "devops.acme.simple.dev jwt-2"},
			wantNotes: []string{reauthNote},
			wantClear: true,
		},
		{
			name:      "other dial errors are returned as they are",
			auth:      &fakeAuthenticator{},
			dial:      func(int, *fakeDevopsClient) (devopsClient, error) { return nil, errors.New("no route to host") },
			wantErr:   "no route to host",
			wantDials: []string{"devops.acme.simple.dev jwt-1"},
		},
		{
			name:      "a failed join closes the client",
			auth:      &fakeAuthenticator{},
			dial:      func(_ int, c *fakeDevopsClient) (devopsClient, error) { return c, nil },
			joinErr:   errors.New("failed to join channel: join error: unauthorized"),
			wantErr:   "failed to join channel: join error: unauthorized",
			wantDials: []string{"devops.acme.simple.dev jwt-1"},
			wantClose: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeDevopsClient{}
			var joined string
			client.join = func(_ context.Context, appID string) error {
				joined = appID
				return tt.joinErr
			}
			var dials []string
			deps := fakeDevopsDeps(tt.auth, &dials, func(n int) (devopsClient, error) { return tt.dial(n, client) })
			target, err := loadDevopsTarget(deps, "dev", ui.NopReporter{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := target.authenticate(context.Background()); err != nil {
				t.Fatal(err)
			}
			steps := &recordingReporter{}

			got, err := target.connect(context.Background(), "com.acme.crm", steps)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				if got != nil {
					t.Errorf("connect returned a client with its error")
				}
			} else {
				if err != nil {
					t.Fatalf("connect() error = %v", err)
				}
				if got != client || joined != "com.acme.crm" {
					t.Errorf("client = %v, joined %q", got, joined)
				}
			}
			if !reflect.DeepEqual(dials, tt.wantDials) {
				t.Errorf("dials = %q, want %q", dials, tt.wantDials)
			}
			if notes := steps.got(); !reflect.DeepEqual(notes, tt.wantNotes) && len(notes)+len(tt.wantNotes) > 0 {
				t.Errorf("reports = %q, want %q", notes, tt.wantNotes)
			}
			if cleared := len(tt.auth.cleared) == 1 && tt.auth.cleared[0] == "acme::dev"; cleared != tt.wantClear {
				t.Errorf("cleared = %q, want cleared %v", tt.auth.cleared, tt.wantClear)
			}
			if client.closed.Load() != tt.wantClose {
				t.Errorf("Close called %d times, want %d", client.closed.Load(), tt.wantClose)
			}
		})
	}
}

func TestDefaultDevopsDeps(t *testing.T) {
	deps := defaultDevopsDeps()
	if deps.ensureParser == nil || deps.loadConfig == nil || deps.dial == nil {
		t.Fatal("defaultDevopsDeps left a dependency unset")
	}
	if _, ok := deps.newAuthenticator().(*deploy.Authenticator); !ok {
		t.Error("newAuthenticator does not return a *deploy.Authenticator")
	}
	if _, err := deps.loadConfig("/nonexistent/scl-parser", nil); err == nil {
		t.Error("loadConfig found a simple.scl in the package directory")
	}
}

func TestDialDevops(t *testing.T) {
	// Nothing listens on port 1, so the dial fails at once without leaving
	// the machine.
	client, err := dialDevops(context.Background(), "ws://127.0.0.1:1", "jwt")
	if err == nil || client != nil {
		t.Fatalf("dialDevops() = %v, %v; want a connect error", client, err)
	}
}

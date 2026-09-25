package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"simple-cli/internal/config"
	"simple-cli/internal/deploy"

	"github.com/gorilla/websocket"
)

func TestRunInstall_AuthRetry(t *testing.T) {
	// Bypass cryptographic verification — we return stub JWTs that won't have real signatures.
	origVerify := deploy.VerifyEd25519
	deploy.VerifyEd25519 = func(pub, msg, sig []byte) bool { return true }
	defer func() { deploy.VerifyEd25519 = origVerify }()

	// Save and restore global flag state.
	origEnv, origProgress := installEnv, installProgress
	defer func() { installEnv, installProgress = origEnv, origProgress }()
	installProgress = "auto"

	// Bypass TLS verification for both the HTTP client and the WebSocket dialer.
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		oldTLS := tr.TLSClientConfig
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only bypass
		defer func() { tr.TLSClientConfig = oldTLS }()
	}
	oldWSDialerTLS := websocket.DefaultDialer.TLSClientConfig
	websocket.DefaultDialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	defer func() { websocket.DefaultDialer.TLSClientConfig = oldWSDialerTLS }()

	var authAttempts int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "login") || strings.Contains(r.URL.Path, "enroll"):
			// Identity service: return a stub JWT.
			// Header {"alg":"EdDSA","kid":"test-key"} → eyJhbGciOiJFZERTQSIsImtpZCI6InRlc3Qta2V5In0
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"eyJhbGciOiJFZERTQSIsImtpZCI6InRlc3Qta2V5In0.e30.ZmFrZQ"}`))

		case strings.Contains(r.URL.Path, ".well-known/jwks.json"):
			// JWKS: 32-byte zero public key (VerifyEd25519 is bypassed so value doesn't matter).
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"test-key","x":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`))

		case strings.Contains(r.URL.Path, "/websocket"):
			authAttempts++
			if authAttempts == 1 {
				// First attempt: reject with 401 to trigger the retry loop.
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Second attempt: full Phoenix Channel server responding to phx_join + install.
			upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			for {
				msgType, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if msgType != websocket.TextMessage {
					continue
				}
				var msg []interface{}
				if err := json.Unmarshal(data, &msg); err != nil || len(msg) != 5 {
					continue
				}
				ref := msg[1]
				topic := msg[2]
				event, _ := msg[3].(string)
				switch event {
				case "phx_join":
					reply, _ := json.Marshal([]interface{}{msg[0], ref, topic, "phx_reply", map[string]interface{}{"status": "ok", "response": map[string]interface{}{}}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				case "install":
					reply, _ := json.Marshal([]interface{}{msg[0], ref, topic, "phx_reply", map[string]interface{}{"status": "ok", "response": map[string]interface{}{"version": "1.0.0"}}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
					return
				}
			}
		}
	}))
	defer server.Close()

	port := strings.Split(server.URL, ":")[2]

	// Write simple.scl in a temp dir and cd into it.
	tmpDir := t.TempDir()
	// api_key must satisfy ParseIDSuffix: prefix "si_", then id_suffix, then 64 random chars.
	sclContent := "tenant \"test\"\nenv \"dev\" {\n\tendpoint \"localhost:" + port + "\"\n\tapi_key \"si_testkey12345ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789XX\"\n}\n"
	if err := os.WriteFile(tmpDir+"/simple.scl", []byte(sclContent), 0644); err != nil {
		t.Fatalf("failed to write simple.scl: %v", err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	installEnv = "dev"

	var out bytes.Buffer
	if err := runInstall(context.Background(), &out, "com.example.myapp"); err != nil {
		t.Fatalf("runInstall() failed unexpectedly: %v", err)
	}

	if authAttempts < 2 {
		t.Errorf("expected at least 2 WebSocket auth attempts (retry on 401), got %d", authAttempts)
	}
	if !strings.Contains(out.String(), "✅ Installed com.example.myapp (Version: 1.0.0) to dev in ") {
		t.Errorf("output does not report the install:\n%s", out.String())
	}
}

func TestRunInstall_ValidatesFlags(t *testing.T) {
	origEnv, origProgress := installEnv, installProgress
	defer func() { installEnv, installProgress = origEnv, origProgress }()
	tests := []struct {
		name, env, progress, wantErr string
	}{
		{"missing --env", "", "auto", "--env flag is required (dev, staging, or prod)"},
		{"invalid --progress", "dev", "fancy", `invalid --progress value "fancy" (want auto, tty or plain)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installEnv, installProgress = tt.env, tt.progress
			if err := runInstall(context.Background(), &bytes.Buffer{}, "com.acme.crm"); err == nil || err.Error() != tt.wantErr {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// installFixture installs com.acme.crm on dev with fakes that take the
// time the design's example shows.
type installFixture struct {
	auth      *fakeAuthenticator
	client    *fakeDevopsClient
	signals   *fakeSignals
	ensureErr error
	dial      func(ctx context.Context, n int) (devopsClient, error)
	dials     int
}

func newInstallFixture() *installFixture {
	return &installFixture{
		auth:    &fakeAuthenticator{wait: 900 * time.Millisecond},
		signals: &fakeSignals{},
		client: &fakeDevopsClient{install: func(context.Context) (*deploy.InstallResult, error) {
			time.Sleep(112 * time.Second)
			return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}, nil
		}},
	}
}

func (f *installFixture) deps() installDeps {
	devops := devopsDeps{
		ensureParser: func(func(string)) (string, error) {
			time.Sleep(400 * time.Millisecond)
			if f.ensureErr != nil {
				return "", f.ensureErr
			}
			return "/fake/scl-parser", nil
		},
		loadConfig:       func(string, func(string)) (*config.SimpleSCL, error) { return testSCL(), nil },
		newAuthenticator: func() devopsAuthenticator { return f.auth },
		dial: func(ctx context.Context, _, _ string) (devopsClient, error) {
			f.dials++
			if f.dial != nil {
				return f.dial(ctx, f.dials)
			}
			time.Sleep(300 * time.Millisecond)
			return f.client, nil
		},
	}
	return installDeps{devops: devops, runner: runnerDeps{notifySignals: f.signals.notify}}
}

func (f *installFixture) interruptAfter(ctx context.Context, d time.Duration) error {
	time.Sleep(d)
	f.signals.interrupt()
	<-ctx.Done()
	return ctx.Err()
}

// installConnected is the transcript of an install up to the install step.
const installConnected = `📥 Installing com.acme.crm to dev
[1/4] Load project config
[1/4] ✓ Load project config: tenant acme (0.4s)
[2/4] Authenticate
[2/4] ✓ Authenticate (0.9s)
[3/4] Connect
[3/4] ✓ Connect: devops.acme.simple.dev (0.3s)
[4/4] Install to dev
`

func TestRunInstallWith(t *testing.T) {
	tests := []struct {
		name    string
		json    bool
		setup   func(f *installFixture)
		want    string
		wantErr string
		// wantJSON, when set, is the only thing out may hold. Under --json
		// without it, out must stay empty.
		wantJSON map[string]any
		// noClose: the run never got a client to close.
		noClose bool
	}{
		{
			name: "installs",
			want: installConnected + `      still running (30s)
      still running (1m00s)
      still running (1m30s)
[4/4] ✓ Install to dev: 1.4.3-dev.5 (1m52s)
✅ Installed com.acme.crm (Version: 1.4.3-dev.5) to dev in 1m53.6s
`,
		},
		{
			name: "--json prints only the result",
			json: true,
			wantJSON: map[string]any{
				"status":      "success",
				"app_id":      "com.acme.crm",
				"version":     "1.4.3-dev.5",
				"env":         "dev",
				"duration_ms": 113600.0,
			},
		},
		{
			name: "a rejected session is signed in again",
			setup: func(f *installFixture) {
				f.dial = func(_ context.Context, n int) (devopsClient, error) {
					time.Sleep(300 * time.Millisecond)
					if n == 1 {
						return nil, &deploy.AuthFailedError{StatusCode: 403}
					}
					return f.client, nil
				}
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}, nil
				}
			},
			want: `📥 Installing com.acme.crm to dev
[1/4] Load project config
[1/4] ✓ Load project config: tenant acme (0.4s)
[2/4] Authenticate
[2/4] ✓ Authenticate (0.9s)
[3/4] Connect
      server rejected the saved session; signing in again
[3/4] ✓ Connect: devops.acme.simple.dev (1.5s)
[4/4] Install to dev
[4/4] ✓ Install to dev: 1.4.3-dev.5 (0s)
✅ Installed com.acme.crm (Version: 1.4.3-dev.5) to dev in 2.8s
`,
		},
		{
			name: "the server rejects the install",
			setup: func(f *installFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					time.Sleep(4 * time.Second)
					return nil, errors.New("no deployed version of com.acme.crm")
				}
			},
			want:    installConnected + "[4/4] ✗ Install to dev: no deployed version of com.acme.crm (4s)\n",
			wantErr: "no deployed version of com.acme.crm",
		},
		{
			name: "the connection drops during the install",
			setup: func(f *installFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					time.Sleep(20 * time.Second)
					return nil, fmt.Errorf("install: %w: websocket: close 1006 (abnormal closure)", deploy.ErrConnectionLost)
				}
			},
			want: installConnected + `[4/4] ✗ Install to dev: install: connection to the devops server was lost: websocket: close 1006 (abnormal closure) (20s)
The CLI stopped waiting, but the server may still be installing com.acme.crm on dev. Let it finish before running simple install again.
`,
			wantErr: "install: connection to the devops server was lost: websocket: close 1006 (abnormal closure)",
		},
		{
			name: "interrupted while installing",
			setup: func(f *installFixture) {
				f.client.install = func(ctx context.Context) (*deploy.InstallResult, error) {
					return nil, f.interruptAfter(ctx, 48200*time.Millisecond)
				}
			},
			want: installConnected + `      still running (30s)
[4/4] ■ Install to dev: interrupted (48.2s)
The server does not cancel an install when the CLI disconnects: it will keep installing com.acme.crm on dev, and its result is not reported here. Let it finish before running simple install again.
`,
			wantErr: `install interrupted during "Install to dev" after 48.2s`,
		},
		{
			name: "interrupted while connecting",
			setup: func(f *installFixture) {
				f.dial = func(ctx context.Context, _ int) (devopsClient, error) {
					return nil, f.interruptAfter(ctx, 5*time.Second)
				}
			},
			want: `📥 Installing com.acme.crm to dev
[1/4] Load project config
[1/4] ✓ Load project config: tenant acme (0.4s)
[2/4] Authenticate
[2/4] ✓ Authenticate (0.9s)
[3/4] Connect
[3/4] ■ Connect: interrupted (5s)
`,
			wantErr: `install interrupted during "Connect" after 5s`,
			noClose: true,
		},
		{
			name:    "loading the config fails",
			setup:   func(f *installFixture) { f.ensureErr = errors.New("offline") },
			want:    "📥 Installing com.acme.crm to dev\n[1/4] Load project config\n[1/4] ✗ Load project config: failed to ensure scl-parser: offline (0.4s)\n",
			wantErr: "failed to ensure scl-parser: offline",
		},
		{
			name: "the connection drops under --json",
			json: true,
			setup: func(f *installFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					return nil, fmt.Errorf("install: %w: EOF", deploy.ErrConnectionLost)
				}
			},
			wantErr: "install: connection to the devops server was lost: EOF",
		},
		{
			name: "an interrupt under --json prints nothing",
			json: true,
			setup: func(f *installFixture) {
				f.client.install = func(ctx context.Context) (*deploy.InstallResult, error) {
					return nil, f.interruptAfter(ctx, 3*time.Second)
				}
			},
			wantErr: `install interrupted during "Install to dev" after 3s`,
		},
		{
			name: "authentication fails",
			setup: func(f *installFixture) {
				f.auth.errs = []error{errors.New("unreachable")}
			},
			want: `📥 Installing com.acme.crm to dev
[1/4] Load project config
[1/4] ✓ Load project config: tenant acme (0.4s)
[2/4] Authenticate
[2/4] ✗ Authenticate: authentication failed: unreachable (0.9s)
`,
			wantErr: "authentication failed: unreachable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newInstallFixture()
				if tt.setup != nil {
					tt.setup(f)
				}
				opts := installOptions{appID: "com.acme.crm", env: "dev", json: tt.json, mode: progressPlain}
				if tt.json {
					opts.mode = progressNone
				}
				var out bytes.Buffer
				err := runInstallWith(context.Background(), &out, f.deps(), opts)

				switch {
				case tt.wantErr == "" && err != nil:
					t.Errorf("err = %v, want nil", err)
				case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
					t.Errorf("err = %v, want %q", err, tt.wantErr)
				}
				if tt.wantJSON != nil {
					assertOneJSONDocument(t, out.Bytes(), tt.wantJSON)
				} else if got := out.String(); got != tt.want {
					t.Errorf("transcript:\n%s\nwant:\n%s", got, tt.want)
				}
				if f.dials > 0 {
					wantClosed := int32(1)
					if tt.noClose {
						wantClosed = 0
					}
					if got := f.client.closed.Load(); got != wantClosed {
						t.Errorf("client closed %d times, want %d", got, wantClosed)
					}
				}
			})
		})
	}
}

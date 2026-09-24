package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"simple-cli/internal/deploy"

	"github.com/gorilla/websocket"
)

func TestRunInstall_AuthRetry(t *testing.T) {
	// Bypass cryptographic verification — we return stub JWTs that won't have real signatures.
	origVerify := deploy.VerifyEd25519
	deploy.VerifyEd25519 = func(pub, msg, sig []byte) bool { return true }
	defer func() { deploy.VerifyEd25519 = origVerify }()

	// Save and restore global flag state.
	origEnv := installEnv
	defer func() { installEnv = origEnv }()

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

func TestRunInstallWith(t *testing.T) {
	tests := []struct {
		name      string
		opts      installOptions
		dial      func(n int, c *fakeDevopsClient) (devopsClient, error)
		install   func(context.Context) (*deploy.InstallResult, error)
		wantErr   string
		wantOut   []string
		wantDials int
	}{
		{
			name:      "installs",
			opts:      installOptions{appID: "com.acme.crm", env: "dev"},
			wantOut:   []string{"🚀 Installing com.acme.crm to dev...\n✅ Installed com.acme.crm (Version: 1.4.3-dev.5) to dev in "},
			wantDials: 1,
		},
		{
			name:      "--json prints one document",
			opts:      installOptions{appID: "com.acme.crm", env: "dev", json: true},
			wantOut:   []string{`"status": "success"`, `"env": "dev"`, `"version": "1.4.3-dev.5"`},
			wantDials: 1,
		},
		{
			name: "a rejected token prints the refresh notice",
			opts: installOptions{appID: "com.acme.crm", env: "dev"},
			dial: func(n int, c *fakeDevopsClient) (devopsClient, error) {
				if n == 1 {
					return nil, &deploy.AuthFailedError{StatusCode: 403}
				}
				return c, nil
			},
			wantOut:   []string{"🔄 Auth token expired, refreshing...\n"},
			wantDials: 2,
		},
		{
			name:    "a missing environment fails first",
			opts:    installOptions{appID: "com.acme.crm", env: "prod"},
			wantErr: "environment 'prod' not defined in simple.scl",
		},
		{
			name:      "connect failure",
			opts:      installOptions{appID: "com.acme.crm", env: "dev"},
			dial:      func(int, *fakeDevopsClient) (devopsClient, error) { return nil, errors.New("no route to host") },
			wantErr:   "no route to host",
			wantDials: 1,
		},
		{
			name:      "install failure",
			opts:      installOptions{appID: "com.acme.crm", env: "dev"},
			install:   func(context.Context) (*deploy.InstallResult, error) { return nil, errors.New("record sync failed") },
			wantErr:   "record sync failed",
			wantDials: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeDevopsClient{install: tt.install}
			if client.install == nil {
				client.install = func(context.Context) (*deploy.InstallResult, error) {
					return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}, nil
				}
			}
			var dials []string
			deps := fakeDevopsDeps(&fakeAuthenticator{}, &dials, func(n int) (devopsClient, error) {
				if tt.dial != nil {
					return tt.dial(n, client)
				}
				return client, nil
			})
			var out bytes.Buffer
			err := runInstallWith(context.Background(), &out, deps, tt.opts)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("runInstallWith() error = %v", err)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if len(dials) != tt.wantDials {
				t.Errorf("dialled %d times, want %d", len(dials), tt.wantDials)
			}
			if tt.wantDials > 0 && tt.wantErr != "no route to host" && client.closed.Load() != 1 {
				t.Errorf("client closed %d times, want 1", client.closed.Load())
			}
		})
	}
}

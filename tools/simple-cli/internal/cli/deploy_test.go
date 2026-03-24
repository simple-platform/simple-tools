package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"simple-cli/internal/deploy"
	"simple-cli/internal/fsx"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRunDeploy(t *testing.T) {
	// Save original flag values
	origEnv := deployEnv
	origBump := deployBump
	origDryRun := deployDryRun
	defer func() {
		deployEnv = origEnv
		deployBump = origBump
		deployDryRun = origDryRun
	}()

	tests := []struct {
		name        string
		args        []string
		env         string
		bump        string
		dryRun      bool
		setupDir    func(t *testing.T, dir string)
		wantErr     bool
		errContains string
	}{
		{
			name:        "missing env flag",
			args:        []string{"apps/myapp"},
			env:         "", // Missing --env
			wantErr:     true,
			errContains: "--env flag is required",
		},
		{
			name:        "app not found",
			args:        []string{"apps/nonexistent"},
			env:         "dev",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "missing simple.scl",
			args: []string{"apps/myapp"},
			env:  "dev",
			bump: "patch",
			setupDir: func(t *testing.T, dir string) {
				// Create app directory but no simple.scl
				appDir := filepath.Join(dir, "apps", "myapp")
				_ = os.MkdirAll(appDir, 0755)
				_ = os.WriteFile(filepath.Join(appDir, "app.scl"), []byte("id test\nversion 1.0.0"), 0644)
			},
			wantErr:     true,
			errContains: "simple.scl", // Now fails at simple.scl loading since scl-parser is available
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup directory
			tmpDir := t.TempDir()
			oldWd, _ := os.Getwd()
			_ = os.Chdir(tmpDir)
			defer func() { _ = os.Chdir(oldWd) }()

			// Set flags
			deployEnv = tt.env
			deployBump = tt.bump
			deployDryRun = tt.dryRun

			// Run setup if provided
			if tt.setupDir != nil {
				tt.setupDir(t, tmpDir)
			}

			err := runDeploy(context.Background(), fsx.OSFileSystem{}, tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("runDeploy() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("runDeploy() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("runDeploy() unexpected error = %v", err)
			}
		})
	}
}

func TestRunDeploy_AuthRetry(t *testing.T) {
	// Temporarily bypass cryptographic verification since we are returning mock JWTs
	origVerify := deploy.VerifyEd25519
	deploy.VerifyEd25519 = func(pub, msg, sig []byte) bool { return true }
	defer func() { deploy.VerifyEd25519 = origVerify }()

	// Enforce clean baseline defaults (include deployNoInstall so a preceding test can't silently skip the install phase)
	origEnv, origBump, origDryRun, origNoInstall := deployEnv, deployBump, deployDryRun, deployNoInstall
	defer func() {
		deployEnv, deployBump, deployDryRun, deployNoInstall = origEnv, origBump, origDryRun, origNoInstall
	}()
	deployNoInstall = false // ensure install phase runs

	// Bypass HTTPS TLS checking for local mock
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		oldTLS := t.TLSClientConfig
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		defer func() { t.TLSClientConfig = oldTLS }()
	}

	oldWSDialerTLS := websocket.DefaultDialer.TLSClientConfig
	websocket.DefaultDialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	defer func() { websocket.DefaultDialer.TLSClientConfig = oldWSDialerTLS }()

	var authAttempts int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "login") || strings.Contains(r.URL.Path, "enroll") {
			w.Header().Set("Content-Type", "application/json")
			// Return a mock JWT signed natively mapping back directly against our fake JWKS.
			// header: {"alg":"EdDSA","kid":"test-key"} -> eyJhbGciOiJFZERTQSIsImtpZCI6InRlc3Qta2V5In0
			_, _ = w.Write([]byte(`{"access_token": "eyJhbGciOiJFZERTQSIsImtpZCI6InRlc3Qta2V5In0.e30.ZmFrZQ"}`))
			return
		}
		if strings.Contains(r.URL.Path, ".well-known/jwks.json") {
			w.Header().Set("Content-Type", "application/json")
			// 32-byte public key padding mapping explicitly correctly
			_, _ = w.Write([]byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"test-key","x":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/websocket") {
			authAttempts++
			if authAttempts == 1 {
				// Eject immediately returning 401 Unauthorized mapping an AuthFailedError
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Connection 2 - accept upgrade mapping standard Deploy channels natively.
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
				if msgType == websocket.TextMessage {
					var msg []interface{}
					if err := json.Unmarshal(data, &msg); err == nil && len(msg) == 5 {
						ref := msg[1]
						topic := msg[2]
						event, _ := msg[3].(string)
						switch event {
						case "phx_join":
							reply, _ := json.Marshal([]interface{}{msg[0], ref, topic, "phx_reply", map[string]interface{}{"status": "ok", "response": map[string]interface{}{}}})
							_ = conn.WriteMessage(websocket.TextMessage, reply)
						case "manifest":
							reply, _ := json.Marshal([]interface{}{msg[0], ref, topic, "phx_reply", map[string]interface{}{"status": "ok", "response": map[string]interface{}{"need_files": []string{}}}})
							_ = conn.WriteMessage(websocket.TextMessage, reply)
						case "deploy":
							reply, _ := json.Marshal([]interface{}{msg[0], ref, topic, "phx_reply", map[string]interface{}{"status": "ok", "response": map[string]interface{}{"version": "1.0.0", "file_count": 0}}})
							_ = conn.WriteMessage(websocket.TextMessage, reply)
						case "install":
							reply, _ := json.Marshal([]interface{}{msg[0], ref, topic, "phx_reply", map[string]interface{}{"status": "ok", "response": map[string]interface{}{"version": "1.0.0"}}})
							_ = conn.WriteMessage(websocket.TextMessage, reply)
							return
						}
					}
				}
			}
		}
	}))
	defer server.Close()

	port := strings.Split(server.URL, ":")[2]

	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "apps", "myapp")
	_ = os.MkdirAll(appDir, 0755)

	sclContent := fmt.Sprintf("tenant \"test\"\nenv \"dev\" {\n\tendpoint \"localhost:%s\"\n\tapi_key \"si_testkey12345ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789XX\"\n}\n", port)
	_ = os.WriteFile(filepath.Join(tmpDir, "simple.scl"), []byte(sclContent), 0644)
	_ = os.WriteFile(filepath.Join(appDir, "app.scl"), []byte("id \"myapp\"\nversion \"1.0.0\""), 0644)

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	deployEnv = "dev"
	deployBump = "patch"
	deployDryRun = false

	if err := runDeploy(context.Background(), fsx.OSFileSystem{}, []string{"apps/myapp"}); err != nil {
		t.Fatalf("runDeploy failed unexpectedly: %v", err)
	}
	if authAttempts < 2 {
		t.Errorf("expected at least 2 auth attempts (retry on 401), got %d", authAttempts)
	}
}

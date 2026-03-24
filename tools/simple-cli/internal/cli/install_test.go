package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
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

	if err := runInstall(context.Background(), "com.example.myapp"); err != nil {
		t.Fatalf("runInstall() failed unexpectedly: %v", err)
	}

	if authAttempts < 2 {
		t.Errorf("expected at least 2 WebSocket auth attempts (retry on 401), got %d", authAttempts)
	}
}

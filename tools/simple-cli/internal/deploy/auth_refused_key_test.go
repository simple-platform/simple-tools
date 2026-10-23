package deploy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"simple-cli/internal/keystore"
)

// A key pair marked enrolled can still be refused, after a tenant reset or a
// key revoked on the instance. Its folder is then moved aside, never deleted,
// but only when the refusal cannot be a wrong clock or anything else.
func TestRefusedSetUpKeyIsMovedAsideOnlyWhenSure(t *testing.T) {
	// This computer's clock for the test, on a whole second like a Date header.
	now := time.Now().Truncate(time.Second)
	tests := []struct {
		name string
		// enrolled marks the pair enrolled before the run.
		enrolled bool
		status   int
		// date sets the login reply's Date header; nil keeps the server's own.
		date      func(header http.Header)
		closed    bool
		wantMoved bool
		wantError string
	}{
		{
			name: "refused with the right time", enrolled: true, status: http.StatusUnauthorized, wantMoved: true,
			wantError: "The instance no longer accepts this workspace's deploy key KEY000002, so its key files were moved to ",
		},
		{
			name: "refused with the clock 5 minutes off", enrolled: true, status: http.StatusUnauthorized,
			date:      func(header http.Header) { header.Set("Date", now.Add(-5*time.Minute).UTC().Format(http.TimeFormat)) },
			wantError: "this computer's clock is off by 300 seconds",
		},
		{
			name: "refused with no time in the reply", enrolled: true, status: http.StatusUnauthorized,
			date:      func(header http.Header) { header["Date"] = nil },
			wantError: "its reply had no time to check this computer's clock against",
		},
		{name: "server error", enrolled: true, status: http.StatusInternalServerError, wantError: "authentication failed (500)"},
		{name: "server unreachable", enrolled: true, closed: true, wantError: "login request failed"},
		{name: "refused right after enrolling", enrolled: false, status: http.StatusUnauthorized, wantError: "authentication failed (401)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cliHome := t.TempDir()
			t.Setenv("SIMPLE_CLI_HOME", cliHome)
			if _, err := keystore.GenerateAndSave("acme", "devstudio", "000002"); err != nil {
				t.Fatalf("set up key pair: %v", err)
			}
			if test.enrolled {
				if err := keystore.MarkEnrolled("acme", "devstudio", "000002"); err != nil {
					t.Fatalf("mark enrolled: %v", err)
				}
			}
			keyDir := filepath.Join(cliHome, ".simple", "keys", "acme", "devstudio", "000002")

			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/auth/api-key/enroll" {
					writer.WriteHeader(http.StatusOK)
					return
				}
				if test.date != nil {
					test.date(writer.Header())
				}
				writer.WriteHeader(test.status)
				_, _ = fmt.Fprint(writer, `{"error":"invalid credentials"}`)
			}))
			client := server.Client()
			endpoint := strings.TrimPrefix(server.URL, "https://")
			if test.closed {
				server.Close()
			} else {
				t.Cleanup(server.Close)
			}
			store := &MockTokenStore{Cache: &TokenCache{Tokens: map[string]CachedToken{
				// About to expire, so a new login is needed.
				"acme::devstudio": {AccessToken: "old", ExpiresAt: time.Now().Add(time.Minute)},
			}}}
			auth := &Authenticator{
				Client:          client,
				Store:           store,
				Decoder:         &MockJWTDecoder{ExpiresAt: time.Now().Add(time.Hour)},
				TimeNow:         func() time.Time { return now },
				VerifySignature: func(_, _, _ []byte) bool { return true },
			}

			_, err := auth.GetJWT(context.Background(), endpoint, "si_000002"+strings.Repeat("a", 64), "acme::devstudio")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("GetJWT error = %v, want it to contain %q", err, test.wantError)
			}

			aside, _ := filepath.Glob(keyDir + ".refused-*")
			_, cached := store.Cache.Tokens["acme::devstudio"]
			if !test.wantMoved {
				if _, statErr := os.Stat(filepath.Join(keyDir, "private.pem")); statErr != nil || len(aside) != 0 {
					t.Fatalf("key files moved although they should stay: %v, set aside %v", statErr, aside)
				}
				if !cached {
					t.Fatal("cached session cleared although the key stayed")
				}
				return
			}
			if len(aside) != 1 || !strings.Contains(err.Error(), aside[0]) {
				t.Fatalf("set aside = %v, error = %v, want one folder named in the error", aside, err)
			}
			for _, name := range []string{"private.pem", ".enrolled"} {
				if _, statErr := os.Stat(filepath.Join(aside[0], name)); statErr != nil {
					t.Fatalf("%s was not kept in the moved folder: %v", name, statErr)
				}
			}
			if _, statErr := os.Stat(keyDir); !os.IsNotExist(statErr) {
				t.Fatalf("key folder still in place: %v", statErr)
			}
			if cached {
				t.Fatal("cached session for the refused key was kept")
			}
			// The bridge then sees no set-up key and takes the browser's.
			if keystore.IsEnrolledUnder(cliHome, "acme", "devstudio", "000002") {
				t.Fatal("the moved-aside key still counts as set up")
			}
		})
	}
}

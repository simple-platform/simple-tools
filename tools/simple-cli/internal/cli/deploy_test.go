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
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"simple-cli/internal/config"
	"simple-cli/internal/deploy"
	"simple-cli/internal/fsx"

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

			err := runDeploy(context.Background(), fsx.OSFileSystem{}, &bytes.Buffer{}, tt.args)

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

func TestPrepareVersionedFiles(t *testing.T) {
	versionBumper := &fakeVersionBumper{version: "1.2.3-local.4"}
	collector := &fakeDeploymentFileCollector{
		collect: func() (map[string]deploy.FileInfo, error) {
			if !versionBumper.called {
				t.Fatal("file collection started before the app version was updated")
			}
			return map[string]deploy.FileInfo{
				"app.scl": {Content: []byte("id example\nversion 1.2.3-local.4\n")},
			}, nil
		},
	}

	version, files, err := prepareVersionedFiles(
		"apps/example",
		"local",
		"",
		versionBumper,
		collector,
	)

	if err != nil {
		t.Fatalf("prepareVersionedFiles() error = %v", err)
	}
	if version != "1.2.3-local.4" {
		t.Errorf("version = %q, want %q", version, "1.2.3-local.4")
	}
	if got := string(files["app.scl"].Content); !strings.Contains(got, "1.2.3-local.4") {
		t.Errorf("uploaded app.scl = %q, want the bumped version", got)
	}
}

type fakeVersionBumper struct {
	called  bool
	version string
}

func (b *fakeVersionBumper) BumpVersion(_ string, _ string, _ string) (string, error) {
	b.called = true
	return b.version, nil
}

type fakeDeploymentFileCollector struct {
	collect func() (map[string]deploy.FileInfo, error)
}

func (c *fakeDeploymentFileCollector) CollectFiles(_ string) (map[string]deploy.FileInfo, error) {
	return c.collect()
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

	var out bytes.Buffer
	if err := runDeploy(context.Background(), fsx.OSFileSystem{}, &out, []string{"apps/myapp"}); err != nil {
		t.Fatalf("runDeploy failed unexpectedly: %v", err)
	}
	if authAttempts < 2 {
		t.Errorf("expected at least 2 auth attempts (retry on 401), got %d", authAttempts)
	}
	if !strings.Contains(out.String(), "✅ Deployed myapp@1.0.0 (Installed) in ") {
		t.Errorf("output does not report the deploy:\n%s", out.String())
	}
}

// fakeVersioner serves app.scl's id and version and records bumps.
type fakeVersioner struct {
	mu       sync.Mutex
	id       string
	current  string
	next     string
	parseErr error
	bumpErr  error
	bumps    int
}

func (v *fakeVersioner) ParseAppSCL(string) (*deploy.AppSCL, error) {
	if v.parseErr != nil {
		return nil, v.parseErr
	}
	return &deploy.AppSCL{ID: v.id, Version: v.current}, nil
}

func (v *fakeVersioner) BumpVersion(string, string, string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.bumps++
	if v.bumpErr != nil {
		return "", v.bumpErr
	}
	return v.next, nil
}

// deployFixture is a deploy of com.acme.crm from 1.4.3-dev.4 to
// 1.4.3-dev.5 with fakes for every side effect.
type deployFixture struct {
	auth      *fakeAuthenticator
	client    *fakeDevopsClient
	versioner *fakeVersioner
	files     map[string]deploy.FileInfo
	collect   func() (map[string]deploy.FileInfo, error)
	dial      func(n int) (devopsClient, error)
	dials     []string
}

func newDeployFixture() *deployFixture {
	f := &deployFixture{
		auth:      &fakeAuthenticator{},
		versioner: &fakeVersioner{id: "com.acme.crm", current: "1.4.3-dev.4", next: "1.4.3-dev.5"},
		files: map[string]deploy.FileInfo{
			"app.scl":        {Path: "app.scl", Hash: "aaaaaaaa11", Size: 120},
			"tables.scl":     {Path: "tables.scl", Hash: "bbbbbbbb22", Size: 880},
			"actions/a.wasm": {Path: "actions/a.wasm", Hash: "cccccccc33", Size: 4000},
		},
	}
	f.client = &fakeDevopsClient{
		manifest: func(context.Context, map[string]deploy.FileInfo, string) ([]string, error) {
			return []string{"actions/a.wasm"}, nil
		},
		publish: func(context.Context) (*deploy.DeployResult, error) {
			return &deploy.DeployResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5"}, nil
		},
		install: func(context.Context) (*deploy.InstallResult, error) {
			return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}, nil
		},
	}
	return f
}

func (f *deployFixture) deps() deployDeps {
	return deployDeps{
		devops: fakeDevopsDeps(f.auth, &f.dials, func(n int) (devopsClient, error) {
			if f.dial != nil {
				return f.dial(n)
			}
			return f.client, nil
		}),
		newVersioner: func(string) appVersioner { return f.versioner },
		newCollector: func() deploymentFileCollector {
			return &fakeDeploymentFileCollector{collect: func() (map[string]deploy.FileInfo, error) {
				if f.collect != nil {
					return f.collect()
				}
				return f.files, nil
			}}
		},
	}
}

func TestRunDeployWith(t *testing.T) {
	tests := []struct {
		name       string
		opts       deployOptions
		setup      func(f *deployFixture)
		wantErr    string
		wantOut    []string
		wantNotOut []string
		wantJSON   map[string]any // the only thing on out, when set
		wantDials  int
	}{
		{
			name: "deploys and installs",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			wantOut: []string{
				"📦 Version: 1.4.3-dev.5\n📁 Files: 3\n⬆️  Uploading 1 files (2 cached)\n🚀 Installing com.acme.crm@1.4.3-dev.5 to dev...\n",
				"✅ Deployed com.acme.crm@1.4.3-dev.5 (Installed) in ",
			},
			wantDials: 1,
		},
		{
			name:       "--no-install stops after the publish",
			opts:       deployOptions{appPath: "apps/com.acme.crm", env: "dev", noInstall: true},
			setup:      func(f *deployFixture) { f.client.install = nil },
			wantOut:    []string{"✅ Deployed com.acme.crm@1.4.3-dev.5 in "},
			wantNotOut: []string{"Installing", "(Installed)"},
			wantDials:  1,
		},
		{
			name:      "--json prints one document",
			opts:      deployOptions{appPath: "apps/com.acme.crm", env: "dev", json: true},
			wantOut:   []string{`"status": "success"`, `"install_success": true`, `"new": 1`},
			wantDials: 1,
		},
		{
			name: "a rejected token prints the refresh notice",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup: func(f *deployFixture) {
				f.dial = func(n int) (devopsClient, error) {
					if n == 1 {
						return nil, &deploy.AuthFailedError{StatusCode: 401}
					}
					return f.client, nil
				}
			},
			wantOut:   []string{"🔄 Auth token expired, refreshing...\n", "✅ Deployed com.acme.crm@1.4.3-dev.5 (Installed)"},
			wantDials: 2,
		},
		{
			name:    "a missing environment fails first",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "prod"},
			wantErr: "environment 'prod' not defined in simple.scl",
		},
		{
			name:    "a bad app.scl fails before connecting",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup:   func(f *deployFixture) { f.versioner.parseErr = errors.New("id not found in app.scl") },
			wantErr: "id not found in app.scl",
		},
		{
			name:    "authentication failure",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup:   func(f *deployFixture) { f.auth.errs = []error{errors.New("bad key")} },
			wantErr: "authentication failed: bad key",
		},
		{
			name:    "bump failure",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup:   func(f *deployFixture) { f.versioner.bumpErr = errors.New("--bump required") },
			wantErr: "--bump required",
		},
		{
			name: "collect failure",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup: func(f *deployFixture) {
				f.collect = func() (map[string]deploy.FileInfo, error) { return nil, errors.New("disk") }
			},
			wantErr: "disk",
		},
		{
			name: "manifest failure",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup: func(f *deployFixture) {
				f.client.manifest = func(context.Context, map[string]deploy.FileInfo, string) ([]string, error) {
					return nil, errors.New("manifest rejected")
				}
			},
			wantErr:   "manifest rejected",
			wantDials: 1,
		},
		{
			name: "upload failure",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup: func(f *deployFixture) {
				f.client.upload = func(context.Context, map[string]deploy.FileInfo, []string, func(deploy.UploadProgress)) error {
					return errors.New("upload actions/a.wasm: send queue full")
				}
			},
			wantErr:   "upload actions/a.wasm: send queue full",
			wantDials: 1,
		},
		{
			name: "publish failure",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup: func(f *deployFixture) {
				f.client.publish = func(context.Context) (*deploy.DeployResult, error) { return nil, errors.New("deploy failed: boom") }
			},
			wantErr:   "deploy failed: boom",
			wantDials: 1,
		},
		{
			name: "install failure",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) { return nil, errors.New("record sync failed") }
			},
			wantErr:   "record sync failed",
			wantOut:   []string{"⚠️  Deploy successful but install failed: record sync failed\n"},
			wantDials: 1,
		},
		{
			name: "install failure with --json prints one document and fails",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev", json: true},
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) { return nil, errors.New("record sync failed") }
			},
			wantErr: "record sync failed",
			wantJSON: map[string]any{
				"status":  "error",
				"error":   "deploy successful but install failed: record sync failed",
				"app_id":  "com.acme.crm",
				"version": "1.4.3-dev.5",
			},
			wantNotOut: []string{"⚠️"},
			wantDials:  1,
		},
		{
			name: "connect failure",
			opts: deployOptions{appPath: "apps/com.acme.crm", env: "dev"},
			setup: func(f *deployFixture) {
				f.client.join = func(context.Context, string) error { return errors.New("failed to join channel") }
			},
			wantErr:   "failed to join channel",
			wantDials: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newDeployFixture()
			if tt.setup != nil {
				tt.setup(f)
			}
			var out bytes.Buffer
			err := runDeployWith(context.Background(), &out, f.deps(), tt.opts)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("runDeployWith() error = %v\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if tt.wantJSON != nil {
				assertOneJSONDocument(t, out.Bytes(), tt.wantJSON)
			}
			for _, unwanted := range tt.wantNotOut {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("output has %q:\n%s", unwanted, out.String())
				}
			}
			if len(f.dials) != tt.wantDials {
				t.Errorf("dialled %d times, want %d", len(f.dials), tt.wantDials)
			}
		})
	}
}

// assertOneJSONDocument fails t unless data is exactly one JSON object equal
// to want.
func assertOneJSONDocument(t *testing.T, data []byte, want map[string]any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var got map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("output is not a JSON document: %v\n%s", err, data)
	}
	if dec.More() {
		t.Fatalf("output has more than one JSON document:\n%s", data)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON = %v, want %v", got, want)
	}
}

func TestRunDeployWith_DryRunIsSideEffectFree(t *testing.T) {
	t.Setenv(unsetKeyVar, "")
	tests := []struct {
		name    string
		opts    deployOptions
		current string
		wantErr string
		wantOut []string
	}{
		{
			name:    "text",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "dev", dryRun: true},
			current: "1.4.3-dev.4",
			wantOut: []string{
				"📦 Version: 1.4.3-dev.5\n",
				"Total: 3 files, version: 1.4.3-dev.5\napp.scl is listed as it is on disk; a real deploy uploads it with version 1.4.3-dev.5.\n",
			},
		},
		{
			name:    "json",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "staging", dryRun: true, json: true},
			current: "1.4.3-dev.4",
			wantOut: []string{`"dry_run": true`, `"version": "1.4.3-staging.1"`},
		},
		{
			name:    "the version rules still apply",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "dev", dryRun: true},
			current: "1.4.3",
			wantErr: "--bump required for first deploy after prod release",
		},
		{
			name:    "with --bump",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "dev", bump: "minor", dryRun: true},
			current: "1.4.3",
			wantOut: []string{"📦 Version: 1.5.0-dev.1\n"},
		},
		{
			name:    "without an API key",
			opts:    deployOptions{appPath: "apps/com.acme.crm", env: "preview", dryRun: true},
			current: "1.4.3-dev.4",
			wantOut: []string{"📦 Version: 1.4.3-preview.1\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newDeployFixture()
			f.versioner.current = tt.current
			deps := f.deps()
			// staging exists only for this test.
			deps.devops.loadConfig = func(string) (*config.SimpleSCL, error) {
				cfg := testSCL()
				cfg.Environments["staging"] = &config.Environment{Name: "staging", Endpoint: "acme-staging.simple.dev", APIKey: "si_key"}
				return cfg, nil
			}
			var out bytes.Buffer
			err := runDeployWith(context.Background(), &out, deps, tt.opts)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("runDeployWith() error = %v", err)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if f.versioner.bumps != 0 {
				t.Errorf("the dry run bumped app.scl %d times", f.versioner.bumps)
			}
			if f.auth.calls != 0 {
				t.Errorf("the dry run signed in %d times", f.auth.calls)
			}
			if len(f.dials) != 0 {
				t.Errorf("the dry run connected: %q", f.dials)
			}
		})
	}
}

func TestInstallDeployedVersion(t *testing.T) {
	stale := errors.New("Version `1.4.3-dev.4` of application `com.acme.crm` is already installed")
	current := errors.New("Version `1.4.3-dev.5` of application `com.acme.crm` is already installed")
	ok := &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}
	tests := []struct {
		name        string
		replies     []error
		wantErr     error
		wantCalls   int
		wantRetries []string
		wantElapsed time.Duration
	}{
		{name: "first try", replies: []error{nil}, wantCalls: 1},
		{name: "already installed at the deployed version", replies: []error{current}, wantCalls: 1},
		{
			name:        "stale then installed",
			replies:     []error{stale, stale, nil},
			wantCalls:   3,
			wantRetries: []string{"1.4.3-dev.4", "1.4.3-dev.4"},
			wantElapsed: 7 * time.Second,
		},
		{
			name:        "stale until the attempts run out",
			replies:     []error{stale, stale, stale, stale},
			wantErr:     stale,
			wantCalls:   4,
			wantRetries: []string{"1.4.3-dev.4", "1.4.3-dev.4", "1.4.3-dev.4"},
			wantElapsed: 17 * time.Second,
		},
		{name: "other errors are not retried", replies: []error{errors.New("boom")}, wantErr: errors.New("boom"), wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				inst := installerFunc(func(context.Context) (*deploy.InstallResult, error) {
					err := tt.replies[calls]
					calls++
					if err != nil {
						return nil, err
					}
					return ok, nil
				})
				var retries []string
				began := time.Now()
				got, err := installDeployedVersion(context.Background(), inst, "1.4.3-dev.5", func(resolved string) {
					retries = append(retries, resolved)
				})
				if tt.wantErr != nil {
					if err == nil || err.Error() != tt.wantErr.Error() {
						t.Fatalf("err = %v, want %v", err, tt.wantErr)
					}
				} else if err != nil || got == nil || !got.Success || got.Version != "1.4.3-dev.5" {
					t.Fatalf("installDeployedVersion() = %+v, %v", got, err)
				}
				if calls != tt.wantCalls {
					t.Errorf("Install called %d times, want %d", calls, tt.wantCalls)
				}
				if !reflect.DeepEqual(retries, tt.wantRetries) {
					t.Errorf("retries = %q, want %q", retries, tt.wantRetries)
				}
				if elapsed := time.Since(began); elapsed != tt.wantElapsed {
					t.Errorf("waited %s, want %s", elapsed, tt.wantElapsed)
				}
			})
		})
	}
}

// installerFunc adapts a function to the installer interface.
type installerFunc func(ctx context.Context) (*deploy.InstallResult, error)

func (f installerFunc) Install(ctx context.Context) (*deploy.InstallResult, error) { return f(ctx) }

func TestDryRunOutput(t *testing.T) {
	files := map[string]deploy.FileInfo{
		"tables.scl":     {Hash: "bbbbbbbb22", Size: 880},
		"app.scl":        {Hash: "0123456789abcdef", Size: 42},
		"actions/a.wasm": {Hash: "cccccccc33", Size: 4000},
		"records/r.scl":  {Hash: "dddddddd44", Size: 7},
	}
	tests := []struct {
		name string
		json bool
		want string
	}{
		{
			name: "text is sorted by path",
			want: "\n📋 Dry run - files to deploy:\n" +
				"  actions/a.wasm (4000 bytes, hash: cccccccc...)\n" +
				"  app.scl (42 bytes, hash: 01234567...)\n" +
				"  records/r.scl (7 bytes, hash: dddddddd...)\n" +
				"  tables.scl (880 bytes, hash: bbbbbbbb...)\n" +
				"\nTotal: 4 files, version: 1.0.1-dev.1\n" +
				"app.scl is listed as it is on disk; a real deploy uploads it with version 1.0.1-dev.1.\n",
		},
		{
			name: "json is sorted by path",
			json: true,
			want: `{
  "dry_run": true,
  "files": [
    {
      "hash": "cccccccc33",
      "path": "actions/a.wasm",
      "size": 4000
    },
    {
      "hash": "0123456789abcdef",
      "path": "app.scl",
      "size": 42
    },
    {
      "hash": "dddddddd44",
      "path": "records/r.scl",
      "size": 7
    },
    {
      "hash": "bbbbbbbb22",
      "path": "tables.scl",
      "size": 880
    }
  ],
  "version": "1.0.1-dev.1"
}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Map iteration order is random; several runs make an unsorted
			// listing all but certain to show up.
			for range 20 {
				var out bytes.Buffer
				if err := dryRunOutput(&out, files, "1.0.1-dev.1", tt.json); err != nil {
					t.Fatal(err)
				}
				if out.String() != tt.want {
					t.Fatalf("output =\n%s\nwant:\n%s", out.String(), tt.want)
				}
			}
		})
	}
}

package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"simple-cli/internal/deploy"
	"simple-cli/internal/fsx"

	"github.com/gorilla/websocket"
)

func TestRunDeploy(t *testing.T) {
	// Save original flag values
	origEnv := deployEnv
	origBump := deployBump
	origDryRun := deployDryRun
	origProgress := deployProgress
	defer func() {
		deployEnv = origEnv
		deployBump = origBump
		deployDryRun = origDryRun
		deployProgress = origProgress
	}()

	tests := []struct {
		name        string
		args        []string
		env         string
		bump        string
		dryRun      bool
		progress    string
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
			name:        "invalid --progress, checked before the app path",
			args:        []string{"apps/nonexistent"},
			env:         "dev",
			progress:    "fancy",
			wantErr:     true,
			errContains: `invalid --progress value "fancy" (want auto, tty or plain)`,
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
			deployProgress = tt.progress
			if deployProgress == "" {
				deployProgress = "auto"
			}

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
	mu        sync.Mutex
	id        string
	current   string
	next      string
	parseErr  error
	bumpErr   error
	bumpTakes time.Duration
	bumps     int
}

func (v *fakeVersioner) ParseAppSCL(string) (*deploy.AppSCL, error) {
	if v.parseErr != nil {
		return nil, v.parseErr
	}
	return &deploy.AppSCL{ID: v.id, Version: v.current}, nil
}

func (v *fakeVersioner) BumpVersion(string, string, string) (string, error) {
	time.Sleep(v.bumpTakes)
	v.mu.Lock()
	defer v.mu.Unlock()
	v.bumps++
	if v.bumpErr != nil {
		return "", v.bumpErr
	}
	return v.next, nil
}

func (v *fakeVersioner) bumped() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.bumps
}

type fakeDeploymentFileCollector struct {
	collect func() (map[string]deploy.FileInfo, error)
}

func (c *fakeDeploymentFileCollector) CollectFiles(string) (map[string]deploy.FileInfo, error) {
	return c.collect()
}

// Sizes that add up to the design's app: 312 new files making 12.4 MB, and
// 184 the server has, 18.2 MB in all.
const (
	specNewFiles    = 312
	specNewSize     = 39_744
	specCachedFiles = 184
	specCachedSize  = 31_521
)

// specApp returns the design's 496-file app and the 312 paths the server
// asks for.
func specApp() (map[string]deploy.FileInfo, []string) {
	files := make(map[string]deploy.FileInfo, specNewFiles+specCachedFiles)
	needed := make([]string, 0, specNewFiles)
	for i := range specNewFiles {
		path := fmt.Sprintf("actions/a%03d/build/release.wasm", i)
		files[path] = deploy.FileInfo{Path: path, Hash: fmt.Sprintf("%064x", i), Size: specNewSize}
		needed = append(needed, path)
	}
	for i := range specCachedFiles {
		path := fmt.Sprintf("records/r%03d.scl", i)
		files[path] = deploy.FileInfo{Path: path, Hash: fmt.Sprintf("%064x", 1000+i), Size: specCachedSize}
	}
	return files, needed
}

// uploadChunks is how the fake server acknowledges an upload: 12 bursts of
// 26 files, 2.33s apart. The odd interval keeps every burst off the plain
// reporter's whole-second ticks, so no transcript depends on which of two
// simultaneous events runs first.
const (
	uploadChunks     = 12
	uploadChunkFiles = specNewFiles / uploadChunks
	uploadChunkEvery = 2330 * time.Millisecond
)

// deployFixture deploys com.acme.crm from 1.4.3-dev.4 to 1.4.3-dev.5 with
// fakes for every side effect. Each fake takes the time the design's
// example shows (0.4s to load the config, 41.2s to compare, and so on), so
// under synctest the plain transcripts read like a real run.
type deployFixture struct {
	auth      *fakeAuthenticator
	client    *fakeDevopsClient
	versioner *fakeVersioner
	signals   *fakeSignals
	files     map[string]deploy.FileInfo
	collect   func(onProgress func(done, total int)) (map[string]deploy.FileInfo, error)
	ensure    func(onStatus func(string)) (string, error)
	dial      func(n int) (devopsClient, error)
	dials     []string
}

func newDeployFixture() *deployFixture {
	files, needed := specApp()
	f := &deployFixture{
		auth:      &fakeAuthenticator{wait: 900 * time.Millisecond},
		versioner: &fakeVersioner{id: "com.acme.crm", current: "1.4.3-dev.4", next: "1.4.3-dev.5", bumpTakes: 200 * time.Millisecond},
		signals:   &fakeSignals{},
		files:     files,
	}
	f.client = &fakeDevopsClient{
		manifest: func(context.Context, map[string]deploy.FileInfo, string) ([]string, error) {
			time.Sleep(41200 * time.Millisecond)
			return needed, nil
		},
		upload: func(_ context.Context, files map[string]deploy.FileInfo, needed []string, onProgress func(deploy.UploadProgress)) error {
			var p deploy.UploadProgress
			for _, path := range needed {
				p.FilesTotal++
				p.BytesTotal += files[path].Size
			}
			for chunk := range uploadChunks {
				time.Sleep(uploadChunkEvery)
				for _, path := range needed[chunk*uploadChunkFiles : (chunk+1)*uploadChunkFiles] {
					p.FilesDone++
					p.BytesDone += files[path].Size
					onProgress(p)
				}
			}
			return nil
		},
		publish: func(context.Context) (*deploy.DeployResult, error) {
			time.Sleep(18300 * time.Millisecond)
			return &deploy.DeployResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", FileCount: specNewFiles + specCachedFiles}, nil
		},
		install: func(context.Context) (*deploy.InstallResult, error) {
			time.Sleep(112 * time.Second)
			return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}, nil
		},
	}
	return f
}

func (f *deployFixture) deps() deployDeps {
	devops := fakeDevopsDeps(f.auth, &f.dials, func(n int) (devopsClient, error) {
		time.Sleep(300 * time.Millisecond)
		if f.dial != nil {
			return f.dial(n)
		}
		return f.client, nil
	})
	devops.ensureParser = func(onStatus func(string)) (string, error) {
		time.Sleep(400 * time.Millisecond)
		if f.ensure != nil {
			return f.ensure(onStatus)
		}
		return "/fake/scl-parser", nil
	}
	return deployDeps{
		devops:       devops,
		runner:       runnerDeps{notifySignals: f.signals.notify},
		newVersioner: func(string) appVersioner { return f.versioner },
		newCollector: func(onProgress func(done, total int)) deploymentFileCollector {
			return &fakeDeploymentFileCollector{collect: func() (map[string]deploy.FileInfo, error) {
				if f.collect != nil {
					return f.collect(onProgress)
				}
				for i := range len(f.files) {
					onProgress(i+1, len(f.files))
				}
				time.Sleep(600 * time.Millisecond)
				return f.files, nil
			}}
		},
	}
}

// interruptAfter is a fake server call that runs for d, is interrupted, and
// returns when the work's context is cancelled, as a real request does.
func (f *deployFixture) interruptAfter(ctx context.Context, d time.Duration) error {
	time.Sleep(d)
	f.signals.interrupt()
	<-ctx.Done()
	return ctx.Err()
}

// smallApp is a three-file app for the dry-run listings.
var smallApp = map[string]deploy.FileInfo{
	"tables.scl":                  {Path: "tables.scl", Hash: "bbbbbbbb22", Size: 880},
	"app.scl":                     {Path: "app.scl", Hash: "aaaaaaaa11", Size: 120},
	"actions/a001/build/app.wasm": {Path: "actions/a001/build/app.wasm", Hash: "cccccccc33", Size: 4000},
}

// throughPublish is the transcript of a deploy up to a successful publish.
const throughPublish = `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✓ Authenticate (0.9s)
[3/9] Bump version
[3/9] ✓ Bump version: 1.4.3-dev.5 · app.scl updated (0.2s)
[4/9] Collect files
[4/9] ✓ Collect files: 496 files · 18.2 MB (0.6s)
[5/9] Connect
[5/9] ✓ Connect: devops.acme.simple.dev (0.3s)
[6/9] Compare with server
      still running (30s)
[6/9] ✓ Compare with server: 312 to upload · 184 already on server (41.2s)
[7/9] Upload files
      104/312 files · 4.1/12.4 MB (33%)
      208/312 files · 8.3/12.4 MB (66%)
[7/9] ✓ Upload files: 312 files · 12.4 MB (28s)
[8/9] Publish version
[8/9] ✓ Publish version: com.acme.crm@1.4.3-dev.5 (18.3s)
[9/9] Install to dev
`

func TestRunDeployWith(t *testing.T) {
	t.Setenv(unsetKeyVar, "")
	stale := errors.New("Version `1.4.3-dev.4` of application `com.acme.crm` is already installed")
	tests := []struct {
		name    string
		opts    deployOptions
		setup   func(f *deployFixture)
		want    string
		wantErr string
		// errHasStack: the error is a recovered panic, whose first line is
		// wantErr and whose stack follows.
		errHasStack bool
		// wantJSON, when set, is the only thing out may hold.
		wantJSON map[string]any
	}{
		{
			name: "deploys and installs",
			want: throughPublish + `      still running (30s)
      still running (1m00s)
      still running (1m30s)
[9/9] ✓ Install to dev: 1.4.3-dev.5 (1m52s)
✅ Deployed com.acme.crm@1.4.3-dev.5 (Installed) in 3m21.86s
`,
		},
		{
			name: "--no-install stops after the publish",
			opts: deployOptions{noInstall: true},
			want: throughPublish[:len(throughPublish)-len("[9/9] Install to dev\n")] + `[9/9] - Install to dev: --no-install
✅ Deployed com.acme.crm@1.4.3-dev.5 in 1m29.86s
   Install it with: simple install com.acme.crm --env dev
`,
		},
		{
			name: "--json prints only the result",
			opts: deployOptions{json: true, mode: progressNone},
			wantJSON: map[string]any{
				"status":          "success",
				"app_id":          "com.acme.crm",
				"version":         "1.4.3-dev.5",
				"files":           map[string]any{"total": 496.0, "new": 312.0, "cached": 184.0},
				"duration_ms":     201860.0,
				"installed":       true,
				"install_success": true,
			},
		},
		{
			name: "the parser download and a rejected session are noted",
			setup: func(f *deployFixture) {
				f.ensure = func(onStatus func(string)) (string, error) {
					onStatus("Downloading...")
					onStatus("Downloading 50%...")
					return "/fake/scl-parser", nil
				}
				f.dial = func(n int) (devopsClient, error) {
					if n == 1 {
						return nil, &deploy.AuthFailedError{StatusCode: 401}
					}
					return f.client, nil
				}
				f.client.manifest = func(context.Context, map[string]deploy.FileInfo, string) ([]string, error) {
					return nil, errors.New("manifest rejected: quota")
				}
			},
			want: `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
      downloading scl-parser
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✓ Authenticate (0.9s)
[3/9] Bump version
[3/9] ✓ Bump version: 1.4.3-dev.5 · app.scl updated (0.2s)
[4/9] Collect files
[4/9] ✓ Collect files: 496 files · 18.2 MB (0.6s)
[5/9] Connect
      server rejected the saved session; signing in again
[5/9] ✓ Connect: devops.acme.simple.dev (1.5s)
[6/9] Compare with server
[6/9] ✗ Compare with server: manifest rejected: quota (0s)
Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk.
`,
			wantErr: "manifest rejected: quota",
		},
		{
			name: "a bad app.scl fails before anything is written or dialled",
			setup: func(f *deployFixture) {
				f.versioner.parseErr = errors.New("failed to parse app.scl: id not found in app.scl")
			},
			want: `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✗ Load project config: failed to parse app.scl: id not found in app.scl (0.4s)
`,
			wantErr: "failed to parse app.scl: id not found in app.scl",
		},
		{
			name:  "sign-in fails",
			setup: func(f *deployFixture) { f.auth.errs = []error{errors.New("key revoked")} },
			want: `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✗ Authenticate: authentication failed: key revoked (0.9s)
`,
			wantErr: "authentication failed: key revoked",
		},
		{
			name: "collecting fails after the bump",
			setup: func(f *deployFixture) {
				f.collect = func(func(done, total int)) (map[string]deploy.FileInfo, error) {
					return nil, errors.New("failed to read actions/a/build/release.wasm: permission denied")
				}
			},
			want: `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✓ Authenticate (0.9s)
[3/9] Bump version
[3/9] ✓ Bump version: 1.4.3-dev.5 · app.scl updated (0.2s)
[4/9] Collect files
[4/9] ✗ Collect files: failed to read actions/a/build/release.wasm: permission denied (0s)
Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk.
`,
			wantErr: "failed to read actions/a/build/release.wasm: permission denied",
		},
		{
			name: "connecting fails after the bump",
			setup: func(f *deployFixture) {
				f.dial = func(int) (devopsClient, error) { return nil, errors.New("dial tcp: no route to host") }
			},
			want: `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✓ Authenticate (0.9s)
[3/9] Bump version
[3/9] ✓ Bump version: 1.4.3-dev.5 · app.scl updated (0.2s)
[4/9] Collect files
[4/9] ✓ Collect files: 496 files · 18.2 MB (0.6s)
[5/9] Connect
[5/9] ✗ Connect: dial tcp: no route to host (0.3s)
Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk.
`,
			wantErr: "dial tcp: no route to host",
		},
		{
			name: "a failure before the bump leaves nothing behind",
			setup: func(f *deployFixture) {
				f.versioner.bumpErr = errors.New("--bump required for first deploy after prod release")
			},
			want: `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✓ Authenticate (0.9s)
[3/9] Bump version
[3/9] ✗ Bump version: --bump required for first deploy after prod release (0.2s)
`,
			wantErr: "--bump required for first deploy after prod release",
		},
		{
			name: "nothing to upload",
			setup: func(f *deployFixture) {
				f.client.manifest = func(context.Context, map[string]deploy.FileInfo, string) ([]string, error) {
					return nil, nil
				}
				f.client.upload = func(context.Context, map[string]deploy.FileInfo, []string, func(deploy.UploadProgress)) error {
					return errors.New("SendFiles called with nothing to upload")
				}
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}, nil
				}
			},
			want: `🚀 Deploying apps/com.acme.crm to dev
[1/9] Load project config
[1/9] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/9] Authenticate
[2/9] ✓ Authenticate (0.9s)
[3/9] Bump version
[3/9] ✓ Bump version: 1.4.3-dev.5 · app.scl updated (0.2s)
[4/9] Collect files
[4/9] ✓ Collect files: 496 files · 18.2 MB (0.6s)
[5/9] Connect
[5/9] ✓ Connect: devops.acme.simple.dev (0.3s)
[6/9] Compare with server
[6/9] ✓ Compare with server: nothing to upload · 496 already on server (0s)
[7/9] - Upload files: all files already on server
[8/9] Publish version
[8/9] ✓ Publish version: com.acme.crm@1.4.3-dev.5 (18.3s)
[9/9] Install to dev
[9/9] ✓ Install to dev: 1.4.3-dev.5 (0s)
✅ Deployed com.acme.crm@1.4.3-dev.5 (Installed) in 20.7s
`,
		},
		{
			name: "the connection drops during the upload",
			setup: func(f *deployFixture) {
				f.client.upload = func(context.Context, map[string]deploy.FileInfo, []string, func(deploy.UploadProgress)) error {
					time.Sleep(4200 * time.Millisecond)
					return fmt.Errorf("upload actions/crm/build/release.wasm: %w: websocket: close 1006 (abnormal closure): unexpected EOF", deploy.ErrConnectionLost)
				}
			},
			want: throughPublish[:strings.Index(throughPublish, "[7/9] Upload files")] + `[7/9] Upload files
[7/9] ✗ Upload files: upload actions/crm/build/release.wasm: connection to the devops server was lost: websocket: close 1006 (abnormal closure): unexpected EOF (4.2s)
Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk.
`,
			wantErr: "upload actions/crm/build/release.wasm: connection to the devops server was lost: websocket: close 1006 (abnormal closure): unexpected EOF",
		},
		{
			name: "the server rejects the publish",
			setup: func(f *deployFixture) {
				f.client.publish = func(context.Context) (*deploy.DeployResult, error) {
					time.Sleep(1200 * time.Millisecond)
					return nil, errors.New("deploy failed: manifest hash mismatch")
				}
			},
			want: throughPublish[:strings.Index(throughPublish, "[8/9] ✓")] + `[8/9] ✗ Publish version: deploy failed: manifest hash mismatch (1.2s)
com.acme.crm@1.4.3-dev.5 was not published. app.scl was already bumped to 1.4.3-dev.5 on disk.
`,
			wantErr: "deploy failed: manifest hash mismatch",
		},
		{
			name: "the publish reply times out",
			setup: func(f *deployFixture) {
				f.client.publish = func(context.Context) (*deploy.DeployResult, error) {
					time.Sleep(1200 * time.Millisecond)
					return nil, fmt.Errorf("deploy: %w after 15m0s", deploy.ErrReplyTimeout)
				}
			},
			want: throughPublish[:strings.Index(throughPublish, "[8/9] ✓")] + `[8/9] ✗ Publish version: deploy: reply timeout after 15m0s (1.2s)
The CLI stopped waiting, but the server may still finish publishing com.acme.crm@1.4.3-dev.5. If it does, install it with: simple install com.acme.crm --env dev
`,
			wantErr: "deploy: reply timeout after 15m0s",
		},
		{
			name: "the install fails",
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					time.Sleep(72 * time.Second)
					return nil, errors.New("record sync failed: field `owner` references missing table `users`")
				}
			},
			want: throughPublish + `      still running (30s)
      still running (1m00s)
[9/9] ✗ Install to dev: record sync failed: field ` + "`owner`" + ` references missing table ` + "`users`" + ` (1m12s)
⚠️  Deploy successful but install failed: record sync failed: field ` + "`owner`" + ` references missing table ` + "`users`" + `
   com.acme.crm@1.4.3-dev.5 is published. Retry the install with: simple install com.acme.crm --env dev
`,
			wantErr: "record sync failed: field `owner` references missing table `users`",
		},
		{
			name: "the connection drops during the install",
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					time.Sleep(12 * time.Second)
					return nil, fmt.Errorf("install: %w: EOF", deploy.ErrConnectionLost)
				}
			},
			want: throughPublish + `[9/9] ✗ Install to dev: install: connection to the devops server was lost: EOF (12s)
⚠️  Deploy successful but the install result is unknown: install: connection to the devops server was lost: EOF
   The CLI stopped waiting, but the server may still be installing com.acme.crm@1.4.3-dev.5. Let it finish before retrying with: simple install com.acme.crm --env dev
`,
			wantErr: "install: connection to the devops server was lost: EOF",
		},
		{
			name: "the install fails under --json",
			opts: deployOptions{json: true, mode: progressNone},
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					return nil, errors.New("record sync failed")
				}
			},
			wantErr: "record sync failed",
			wantJSON: map[string]any{
				"status":  "error",
				"error":   "deploy successful but install failed: record sync failed",
				"app_id":  "com.acme.crm",
				"version": "1.4.3-dev.5",
			},
		},
		{
			name: "the install result is unknown under --json",
			opts: deployOptions{json: true, mode: progressNone},
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					return nil, fmt.Errorf("install: %w after 15m0s", deploy.ErrReplyTimeout)
				}
			},
			wantErr: "install: reply timeout after 15m0s",
			wantJSON: map[string]any{
				"status":  "error",
				"error":   "deploy successful but the install result is unknown: install: reply timeout after 15m0s",
				"app_id":  "com.acme.crm",
				"version": "1.4.3-dev.5",
			},
		},
		{
			name: "a panic during the install keeps its stack off stdout",
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) { panic("boom") }
			},
			want: throughPublish + `[9/9] ✗ Install to dev: internal error: boom (0s)
⚠️  Deploy successful but install failed: internal error: boom
   com.acme.crm@1.4.3-dev.5 is published. Retry the install with: simple install com.acme.crm --env dev
`,
			wantErr:     "internal error: boom",
			errHasStack: true,
		},
		{
			name: "a panic during the install keeps its stack out of the JSON",
			opts: deployOptions{json: true, mode: progressNone},
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) { panic("boom") }
			},
			wantErr:     "internal error: boom",
			errHasStack: true,
			wantJSON: map[string]any{
				"status":  "error",
				"error":   "deploy successful but install failed: internal error: boom",
				"app_id":  "com.acme.crm",
				"version": "1.4.3-dev.5",
			},
		},
		{
			name: "an interrupt under --json prints no document",
			opts: deployOptions{json: true, mode: progressNone},
			setup: func(f *deployFixture) {
				f.client.install = func(ctx context.Context) (*deploy.InstallResult, error) {
					return nil, f.interruptAfter(ctx, 3*time.Second)
				}
			},
			wantErr: `deploy interrupted during "Install to dev" after 3s`,
		},
		{
			name: "interrupted while comparing",
			setup: func(f *deployFixture) {
				f.client.manifest = func(ctx context.Context, _ map[string]deploy.FileInfo, _ string) ([]string, error) {
					return nil, f.interruptAfter(ctx, 12300*time.Millisecond)
				}
			},
			want: throughPublish[:strings.Index(throughPublish, "      still running")] + `[6/9] ■ Compare with server: interrupted (12.3s)
Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk.
`,
			wantErr: `deploy interrupted during "Compare with server" after 12.3s`,
		},
		{
			name: "interrupted while publishing",
			setup: func(f *deployFixture) {
				f.client.publish = func(ctx context.Context) (*deploy.DeployResult, error) {
					return nil, f.interruptAfter(ctx, 5*time.Second)
				}
			},
			want: throughPublish[:strings.Index(throughPublish, "[8/9] ✓")] + `[8/9] ■ Publish version: interrupted (5s)
The server does not cancel a publish when the CLI disconnects: com.acme.crm@1.4.3-dev.5 may still be published, but it will not be installed. If it is, install it with: simple install com.acme.crm --env dev
`,
			wantErr: `deploy interrupted during "Publish version" after 5s`,
		},
		{
			name: "interrupted while installing",
			setup: func(f *deployFixture) {
				f.client.install = func(ctx context.Context) (*deploy.InstallResult, error) {
					return nil, f.interruptAfter(ctx, 48200*time.Millisecond)
				}
			},
			want: throughPublish + `      still running (30s)
[9/9] ■ Install to dev: interrupted (48.2s)
The server does not cancel an install when the CLI disconnects: it will keep installing com.acme.crm@1.4.3-dev.5 on dev, and its result is not reported here. Let it finish before running simple install again.
`,
			wantErr: `deploy interrupted during "Install to dev" after 48.2s`,
		},
		{
			name: "interrupted while waiting to retry the install",
			setup: func(f *deployFixture) {
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					time.Sleep(3 * time.Second)
					go func() {
						time.Sleep(time.Second)
						f.signals.interrupt()
					}()
					return nil, stale
				}
			},
			want: throughPublish + `      ↻ server resolved 1.4.3-dev.4; retrying install of 1.4.3-dev.5 in 2s (1/4)
[9/9] ■ Install to dev: interrupted (4s)
No install is running: com.acme.crm@1.4.3-dev.5 is published but not installed. Install it with: simple install com.acme.crm --env dev
`,
			wantErr: `deploy interrupted during "Install to dev" after 4s`,
		},
		{
			name: "a stale install is retried",
			setup: func(f *deployFixture) {
				calls := 0
				f.client.install = func(context.Context) (*deploy.InstallResult, error) {
					calls++
					time.Sleep(3 * time.Second)
					if calls == 1 {
						return nil, stale
					}
					return &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}, nil
				}
			},
			want: throughPublish + `      ↻ server resolved 1.4.3-dev.4; retrying install of 1.4.3-dev.5 in 2s (1/4)
[9/9] ✓ Install to dev: 1.4.3-dev.5 (8s)
✅ Deployed com.acme.crm@1.4.3-dev.5 (Installed) in 1m37.86s
`,
		},
		{
			name: "dry run",
			opts: deployOptions{dryRun: true},
			setup: func(f *deployFixture) {
				f.files = smallApp
			},
			want: `🔍 Dry run: apps/com.acme.crm to dev (nothing is written or uploaded)
[1/3] Load project config
[1/3] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/3] Bump version
[2/3] ✓ Bump version: 1.4.3-dev.5 · app.scl not changed (0s)
[3/3] Collect files
[3/3] ✓ Collect files: 3 files · 5.0 kB (0.6s)
📋 Dry run - files to deploy:
  actions/a001/build/app.wasm (4000 bytes, hash: cccccccc...)
  app.scl (120 bytes, hash: aaaaaaaa...)
  tables.scl (880 bytes, hash: bbbbbbbb...)

Total: 3 files, version: 1.4.3-dev.5
app.scl is listed as it is on disk; a real deploy uploads it with version 1.4.3-dev.5.
`,
		},
		{
			name: "dry run under --json",
			opts: deployOptions{dryRun: true, json: true, mode: progressNone},
			setup: func(f *deployFixture) {
				f.files = smallApp
			},
			wantJSON: map[string]any{
				"dry_run": true,
				"version": "1.4.3-dev.5",
				"files": []any{
					map[string]any{"path": "actions/a001/build/app.wasm", "hash": "cccccccc33", "size": 4000.0},
					map[string]any{"path": "app.scl", "hash": "aaaaaaaa11", "size": 120.0},
					map[string]any{"path": "tables.scl", "hash": "bbbbbbbb22", "size": 880.0},
				},
			},
		},
		{
			name: "a dry run needs no API key",
			opts: deployOptions{dryRun: true, env: "preview"},
			setup: func(f *deployFixture) {
				f.files = smallApp
			},
			want: `🔍 Dry run: apps/com.acme.crm to preview (nothing is written or uploaded)
[1/3] Load project config
[1/3] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/3] Bump version
[2/3] ✓ Bump version: 1.4.3-preview.1 · app.scl not changed (0s)
[3/3] Collect files
[3/3] ✓ Collect files: 3 files · 5.0 kB (0.6s)
📋 Dry run - files to deploy:
  actions/a001/build/app.wasm (4000 bytes, hash: cccccccc...)
  app.scl (120 bytes, hash: aaaaaaaa...)
  tables.scl (880 bytes, hash: bbbbbbbb...)

Total: 3 files, version: 1.4.3-preview.1
app.scl is listed as it is on disk; a real deploy uploads it with version 1.4.3-preview.1.
`,
		},
		{
			name: "a dry run fails where the version cannot be computed",
			opts: deployOptions{dryRun: true},
			setup: func(f *deployFixture) {
				f.versioner.current = "1.4.2"
			},
			want: `🔍 Dry run: apps/com.acme.crm to dev (nothing is written or uploaded)
[1/3] Load project config
[1/3] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/3] Bump version
[2/3] ✗ Bump version: --bump required for first deploy after prod release (0s)
`,
			wantErr: "--bump required for first deploy after prod release",
		},
		{
			name: "an interrupted dry run leaves nothing behind",
			opts: deployOptions{dryRun: true},
			setup: func(f *deployFixture) {
				// The collector cannot be cancelled: after the grace period
				// the run gives up on it.
				f.collect = func(func(done, total int)) (map[string]deploy.FileInfo, error) {
					time.Sleep(300 * time.Millisecond)
					f.signals.interrupt()
					time.Sleep(5 * time.Second)
					return smallApp, nil
				}
			},
			want: `🔍 Dry run: apps/com.acme.crm to dev (nothing is written or uploaded)
[1/3] Load project config
[1/3] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/3] Bump version
[2/3] ✓ Bump version: 1.4.3-dev.5 · app.scl not changed (0s)
[3/3] Collect files
[3/3] ■ Collect files: interrupted (0.3s)
`,
			wantErr: `deploy interrupted during "Collect files" after 0.3s`,
		},
		{
			name: "a dry run the interrupt came too late for completes",
			opts: deployOptions{dryRun: true},
			setup: func(f *deployFixture) {
				f.collect = func(func(done, total int)) (map[string]deploy.FileInfo, error) {
					f.signals.interrupt()
					time.Sleep(300 * time.Millisecond)
					return map[string]deploy.FileInfo{"app.scl": smallApp["app.scl"]}, nil
				}
			},
			want: `🔍 Dry run: apps/com.acme.crm to dev (nothing is written or uploaded)
[1/3] Load project config
[1/3] ✓ Load project config: com.acme.crm · tenant acme (0.4s)
[2/3] Bump version
[2/3] ✓ Bump version: 1.4.3-dev.5 · app.scl not changed (0s)
[3/3] Collect files
[3/3] ✓ Collect files: 1 file · 120 B (0.3s)
📋 Dry run - files to deploy:
  app.scl (120 bytes, hash: aaaaaaaa...)

Total: 1 files, version: 1.4.3-dev.5
app.scl is listed as it is on disk; a real deploy uploads it with version 1.4.3-dev.5.
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newDeployFixture()
				if tt.setup != nil {
					tt.setup(f)
				}
				opts := tt.opts
				opts.appPath = "apps/com.acme.crm"
				if opts.env == "" {
					opts.env = "dev"
				}
				if !opts.json {
					opts.mode = progressPlain
				}
				var out bytes.Buffer
				err := runDeployWith(context.Background(), &out, f.deps(), opts)

				switch {
				case tt.wantErr == "" && err != nil:
					t.Errorf("err = %v, want nil", err)
				case tt.wantErr != "" && err == nil:
					t.Errorf("err = nil, want %q", tt.wantErr)
				case tt.errHasStack:
					if first, stack, _ := strings.Cut(err.Error(), "\n"); first != tt.wantErr || !strings.Contains(stack, "goroutine") {
						t.Errorf("err = %q, want %q and a stack", err, tt.wantErr)
					}
				case tt.wantErr != "" && err.Error() != tt.wantErr:
					t.Errorf("err = %v, want %q", err, tt.wantErr)
				}
				if tt.wantJSON != nil {
					assertOneJSONDocument(t, out.Bytes(), tt.wantJSON)
				} else if got := out.String(); got != tt.want {
					t.Errorf("transcript:\n%s\nwant:\n%s", got, tt.want)
				}
				if opts.dryRun && (f.versioner.bumped() != 0 || f.auth.calls != 0 || len(f.dials) != 0) {
					t.Errorf("the dry run bumped %d times, signed in %d times, dialled %q", f.versioner.bumped(), f.auth.calls, f.dials)
				}

				// A work abandoned after an interrupt runs on; whatever it
				// reports once it ends must not reach the output.
				printed := out.String()
				time.Sleep(time.Minute)
				if out.String() != printed {
					t.Errorf("an abandoned work printed:\n%s", strings.TrimPrefix(out.String(), printed))
				}
			})
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

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

func TestRunDeployWith_JSONWriteFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newDeployFixture()
		f.client.install = func(context.Context) (*deploy.InstallResult, error) { return nil, errors.New("record sync failed") }
		opts := deployOptions{appPath: "apps/com.acme.crm", env: "dev", json: true, mode: progressNone}
		// The encoder's own error comes back when the document cannot be
		// written; the install error it would have carried is lost with it.
		if err := runDeployWith(context.Background(), failingWriter{}, f.deps(), opts); err == nil || err.Error() != "stdout closed" {
			t.Errorf("err = %v, want the write error", err)
		}
	})
}

func TestRunDeployWith_BumpsBeforeCollect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newDeployFixture()
		f.collect = func(func(done, total int)) (map[string]deploy.FileInfo, error) {
			if f.versioner.bumped() != 1 {
				t.Error("file collection started before app.scl was bumped")
			}
			return smallApp, nil
		}
		opts := deployOptions{appPath: "apps/com.acme.crm", env: "dev", noInstall: true, mode: progressNone}
		if err := runDeployWith(context.Background(), io.Discard, f.deps(), opts); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRunDeployWith_WritesOnlyToOut(t *testing.T) {
	// Progress, results and aftermath go to out, which RunE sets to cobra's
	// stdout; nothing may bypass it, or --json output and tests break.
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout, origStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	restore := func() {
		os.Stdout, os.Stderr = origStdout, origStderr
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
	}
	defer restore()

	for _, opts := range []deployOptions{
		{mode: progressPlain},
		{mode: progressNone, json: true},
		{mode: progressPlain, dryRun: true},
	} {
		synctest.Test(t, func(t *testing.T) {
			f := newDeployFixture()
			f.client.install = func(context.Context) (*deploy.InstallResult, error) { return nil, errors.New("record sync failed") }
			opts.appPath, opts.env = "apps/com.acme.crm", "dev"
			var out bytes.Buffer
			_ = runDeployWith(context.Background(), &out, f.deps(), opts)
			if out.Len() == 0 {
				t.Errorf("%+v wrote nothing to out", opts)
			}
		})
	}

	restore()
	for name, r := range map[string]*os.File{"stdout": stdoutReader, "stderr": stderrReader} {
		leaked, _ := io.ReadAll(r)
		if len(leaked) > 0 {
			t.Errorf("deploy wrote to os.%s:\n%s", name, leaked)
		}
		_ = r.Close()
	}
}

func TestInstallDeployedVersion(t *testing.T) {
	stale := errors.New("Version `1.4.3-dev.4` of application `com.acme.crm` is already installed")
	current := errors.New("Version `1.4.3-dev.5` of application `com.acme.crm` is already installed")
	ok := &deploy.InstallResult{AppID: "com.acme.crm", Version: "1.4.3-dev.5", Success: true}
	tests := []struct {
		name        string
		replies     []error
		cancelAfter time.Duration // cancel ctx this long in, when set
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
			wantRetries: []string{"1.4.3-dev.4 1/4 2s", "1.4.3-dev.4 2/4 5s"},
			wantElapsed: 7 * time.Second,
		},
		{
			name:        "stale until the attempts run out",
			replies:     []error{stale, stale, stale, stale},
			wantErr:     stale,
			wantCalls:   4,
			wantRetries: []string{"1.4.3-dev.4 1/4 2s", "1.4.3-dev.4 2/4 5s", "1.4.3-dev.4 3/4 10s"},
			wantElapsed: 17 * time.Second,
		},
		{name: "other errors are not retried", replies: []error{errors.New("boom")}, wantErr: errors.New("boom"), wantCalls: 1},
		{
			name:        "a cancel during the wait sends no more installs",
			replies:     []error{stale, stale, nil},
			cancelAfter: 3 * time.Second,
			wantErr:     context.Canceled,
			wantCalls:   2,
			wantRetries: []string{"1.4.3-dev.4 1/4 2s", "1.4.3-dev.4 2/4 5s"},
			wantElapsed: 3 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if tt.cancelAfter > 0 {
					time.AfterFunc(tt.cancelAfter, cancel)
				}
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
				got, err := installDeployedVersion(ctx, inst, "1.4.3-dev.5", func(resolved string, attempt, attempts int, wait time.Duration) {
					retries = append(retries, fmt.Sprintf("%s %d/%d %s", resolved, attempt, attempts, wait))
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
		"records/r.scl":  {Hash: "short", Size: 7},
	}
	tests := []struct {
		name string
		json bool
		want string
	}{
		{
			name: "text is sorted by path",
			want: "📋 Dry run - files to deploy:\n" +
				"  actions/a.wasm (4000 bytes, hash: cccccccc...)\n" +
				"  app.scl (42 bytes, hash: 01234567...)\n" +
				"  records/r.scl (7 bytes, hash: short...)\n" +
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
      "hash": "short",
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

func TestManifestSummary(t *testing.T) {
	tests := []struct {
		toUpload, total int
		want            string
	}{
		{312, 496, "312 to upload · 184 already on server"},
		{0, 496, "nothing to upload · 496 already on server"},
		{3, 3, "3 to upload · 0 already on server"},
	}
	for _, tt := range tests {
		if got := manifestSummary(tt.toUpload, tt.total); got != tt.want {
			t.Errorf("manifestSummary(%d, %d) = %q, want %q", tt.toUpload, tt.total, got, tt.want)
		}
	}
}

func TestCountFiles(t *testing.T) {
	if got := countFiles(1) + ", " + countFiles(2) + ", " + countUploadedFiles(1) + ", " + countUploadedFiles(312); got != "1 file, 2 files, 1 uploaded file, 312 uploaded files" {
		t.Errorf("got %q", got)
	}
}

func TestNewUploadPlan(t *testing.T) {
	files := map[string]deploy.FileInfo{"a": {Size: 10}, "b": {Size: 20}}
	got := newUploadPlan(files, []string{"a", "missing", "b"})
	if got != (uploadPlan{files: 2, bytes: 30}) {
		t.Errorf("newUploadPlan() = %+v; a path missing from the collection must not count", got)
	}
}

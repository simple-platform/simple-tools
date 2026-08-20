package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func setupTestProductionRoot(t *testing.T, instanceIDs ...string) string {
	t.Helper()
	dir := t.TempDir()
	oldRoot := devStudioProductionRoot
	devStudioProductionRoot = dir
	t.Cleanup(func() { devStudioProductionRoot = oldRoot })

	for _, id := range instanceIDs {
		instanceRoot := filepath.Join(dir, id)
		if err := os.MkdirAll(filepath.Join(instanceRoot, devStudioAppsDirectory), 0755); err != nil {
			t.Fatalf("mkdir test instance %s: %v", id, err)
		}
		if err := os.WriteFile(filepath.Join(instanceRoot, "simple.scl"), []byte("tenant acme\nenv dev {\n  endpoint acme.simple.lcl\n  api_key $OTHER_KEY\n}\n"), 0644); err != nil {
			t.Fatalf("write test workspace config %s: %v", id, err)
		}
	}
	return dir
}

func TestDevStudioWorkspaceRoot(t *testing.T) {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve user home directory: %v", err)
	}
	want := filepath.Join(homeDirectory, "workspace", "simple-devstudio")
	if devStudioWorkspaceRoot != want {
		t.Fatalf("devStudioWorkspaceRoot = %q, want %q", devStudioWorkspaceRoot, want)
	}
}

func TestDevStudioServeCommandIsRegistered(t *testing.T) {
	command, _, err := RootCmd.Find([]string{"devstudio", "serve"})
	if err != nil {
		t.Fatalf("find devstudio serve command: %v", err)
	}
	if command != devStudioServeCmd {
		t.Fatalf("command = %p, want devStudioServeCmd %p", command, devStudioServeCmd)
	}
	if command.Args == nil {
		t.Fatal("devStudioServeCmd.Args is nil, want cobra.NoArgs")
	}
	if err := command.Args(command, []string{"extra"}); err == nil {
		t.Fatal("expected error for positional args on serve, got nil")
	}
}

func TestDevStudioBridgeStartsOnLoopback(t *testing.T) {
	bridge := newDevStudioBridge()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type = %T, want *net.TCPAddr", listener.Addr())
	}
	if !address.IP.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("listener IP = %s, want 127.0.0.1", address.IP)
	}

	server := &http.Server{Handler: bridge.handler()}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		<-done
	})

	response, err := http.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var health map[string]string
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health["status"] != "ok" {
		t.Fatalf("health status payload = %q, want ok", health["status"])
	}
}

func TestDevStudioServePrintsLocalEndpointsAndStopsWithContext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- serveDevStudio(ctx, listener, &output) }()

	response, err := http.Get("http://" + listener.Addr().String() + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	response.Body.Close()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveDevStudio returned error: %v", err)
	}

	address := listener.Addr().String()
	if !strings.Contains(output.String(), "Bound address: "+address) {
		t.Fatalf("startup output missing address %q: %s", address, output.String())
	}
	if !strings.Contains(output.String(), "Health: http://"+address+"/health") {
		t.Fatalf("startup output missing health endpoint: %s", output.String())
	}
	if !strings.Contains(output.String(), "WebSocket: ws://"+address+devStudioWebSocketPath) {
		t.Fatalf("startup output missing WebSocket endpoint: %s", output.String())
	}
	if !strings.Contains(output.String(), "Press Ctrl+C to stop.") {
		t.Fatalf("startup output missing shutdown guidance: %s", output.String())
	}
}

func TestDevStudioWorkspaceResolver(t *testing.T) {
	t.Run("valid root and discovers apps beneath apps directory", func(t *testing.T) {
		baseDir := setupTestProductionRoot(t, "inst_valid")
		instDir := filepath.Join(baseDir, "inst_valid")
		legacyApp := filepath.Join(instDir, "legacy-app")
		if err := os.MkdirAll(legacyApp, 0755); err != nil {
			t.Fatalf("mkdir legacyApp: %v", err)
		}
		if err := os.WriteFile(filepath.Join(legacyApp, "app.scl"), []byte("app legacy-app"), 0644); err != nil {
			t.Fatalf("write legacy app.scl: %v", err)
		}

		// App 1 with app.scl
		app1 := filepath.Join(instDir, devStudioAppsDirectory, "my-app")
		if err := os.MkdirAll(app1, 0755); err != nil {
			t.Fatalf("mkdir app1: %v", err)
		}
		if err := os.WriteFile(filepath.Join(app1, "app.scl"), []byte("app my-app"), 0644); err != nil {
			t.Fatalf("write app.scl: %v", err)
		}

		// Non-app dir without app.scl
		otherDir := filepath.Join(instDir, devStudioAppsDirectory, "regular-dir")
		if err := os.MkdirAll(otherDir, 0755); err != nil {
			t.Fatalf("mkdir otherDir: %v", err)
		}

		ws, err := resolveDevStudioWorkspace("inst_valid")
		if err != nil {
			t.Fatalf("resolveDevStudioWorkspace unexpected error: %v", err)
		}
		if ws == nil {
			t.Fatal("expected workspace non-nil")
		}
		if ws.instanceID != "inst_valid" {
			t.Fatalf("ws.instanceID = %q, want inst_valid", ws.instanceID)
		}
		if len(ws.appDirs) != 1 || ws.appDirs[0] != "my-app" {
			t.Fatalf("ws.appDirs = %v, want [my-app]", ws.appDirs)
		}
	})

	t.Run("missing root clear failure", func(t *testing.T) {
		setupTestProductionRoot(t)
		_, err := resolveDevStudioWorkspace("inst_missing")
		if err == nil {
			t.Fatal("expected error for missing root directory, got nil")
		}
	})

	t.Run("bad instance ID cannot escape", func(t *testing.T) {
		setupTestProductionRoot(t)
		for _, badID := range []string{".", "..", "../escaped", "foo/bar", "inst;bad", ""} {
			_, err := resolveDevStudioWorkspace(badID)
			if err == nil {
				t.Fatalf("expected error for bad instance ID %q, got nil", badID)
			}
		}
	})

	t.Run("symlink escape rejected", func(t *testing.T) {
		baseDir := setupTestProductionRoot(t)
		outsideDir := t.TempDir()

		symlinkPath := filepath.Join(baseDir, "inst_symlink")
		if err := os.Symlink(outsideDir, symlinkPath); err != nil {
			t.Fatalf("create symlink: %v", err)
		}

		_, err := resolveDevStudioWorkspace("inst_symlink")
		if err == nil {
			t.Fatal("expected error for symlink escaping base dir, got nil")
		}
	})

	t.Run("apps directory symlink containment", func(t *testing.T) {
		baseDir := setupTestProductionRoot(t, "inst_symlink_apps")
		instDir := filepath.Join(baseDir, "inst_symlink_apps")

		outsideDir := t.TempDir()
		outsideApp := filepath.Join(outsideDir, "outside-app")
		if err := os.MkdirAll(outsideApp, 0755); err != nil {
			t.Fatalf("mkdir outsideApp: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outsideApp, "app.scl"), []byte("mock"), 0644); err != nil {
			t.Fatalf("write app.scl: %v", err)
		}

		if err := os.Symlink(outsideApp, filepath.Join(instDir, devStudioAppsDirectory, "symlink-outside")); err != nil {
			t.Fatalf("create symlink outside: %v", err)
		}

		insideApp := filepath.Join(instDir, devStudioAppsDirectory, "inside-app")
		if err := os.MkdirAll(insideApp, 0755); err != nil {
			t.Fatalf("mkdir insideApp: %v", err)
		}
		if err := os.WriteFile(filepath.Join(insideApp, "app.scl"), []byte("mock"), 0644); err != nil {
			t.Fatalf("write app.scl: %v", err)
		}

		ws, err := resolveDevStudioWorkspace("inst_symlink_apps")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ws.appDirs) != 1 || ws.appDirs[0] != "inside-app" {
			t.Fatalf("appDirs = %v, want [inside-app]", ws.appDirs)
		}
	})
}

func TestDevStudioInjectableResolver(t *testing.T) {
	t.Run("bridge uses injected resolver", func(t *testing.T) {
		baseDir := setupTestProductionRoot(t, "inst_custom")
		customCalled := false
		customResolver := func(id string) (*devStudioWorkspace, error) {
			customCalled = true
			return &devStudioWorkspace{
				instanceID:   id,
				instanceRoot: filepath.Join(baseDir, id),
			}, nil
		}

		bridge := newDevStudioBridge(customResolver)
		server := httptest.NewServer(bridge.handler())
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + devStudioWebSocketPath
		connection := dialDevStudio(t, wsURL)
		defer connection.Close()

		sendDevStudioMessage(t, connection, sessionConnectMessage("inst_custom"))
		message := readDevStudioMessage(t, connection)

		if !customCalled {
			t.Fatal("custom resolver was not called by bridge")
		}
		if message["type"] != "session.ready" {
			t.Fatalf("type = %q, want session.ready", message["type"])
		}
	})

}

func TestDevStudioCandidatePathsAndPathRequired(t *testing.T) {
	t.Run("returns instance_path_required when instance not found in candidate paths", func(t *testing.T) {
		setupTestProductionRoot(t) // Empty production root
		bridge := newDevStudioBridge()
		server := httptest.NewServer(bridge.handler())
		t.Cleanup(server.Close)

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + devStudioWebSocketPath
		connection := dialDevStudio(t, wsURL)
		defer connection.Close()

		sendDevStudioMessage(t, connection, sessionConnectMessage("inst_nonexistent_candidate"))
		message := readDevStudioMessage(t, connection)
		if message["type"] != "error" {
			t.Fatalf("type = %q, want error", message["type"])
		}
		if message["code"] != "instance_path_required" {
			t.Fatalf("code = %q, want instance_path_required", message["code"])
		}
	})

	t.Run("initializes and connects when custom instancePath is provided", func(t *testing.T) {
		baseDir := t.TempDir()
		customInstancePath := filepath.Join(baseDir, "my_custom_instance")

		bridge := newDevStudioBridge()
		server := httptest.NewServer(bridge.handler())
		t.Cleanup(server.Close)

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + devStudioWebSocketPath
		connection := dialDevStudio(t, wsURL)
		defer connection.Close()

		connectMsg := map[string]any{
			"type":            "session.connect",
			"protocolVersion": 1,
			"instanceId":      "inst_custom_path",
			"instanceHost":    "inst_custom_path.simple.lcl",
			"deployKey":       "test_deploy_key_secret",
			"instancePath":    customInstancePath,
		}
		sendDevStudioMessage(t, connection, connectMsg)
		message := readDevStudioMessage(t, connection)
		if message["type"] != "session.ready" {
			t.Fatalf("type = %q, want session.ready", message["type"])
		}

		info, err := os.Stat(customInstancePath)
		if err != nil || !info.IsDir() {
			t.Fatalf("created custom instance root = %v, %v; want directory", info, err)
		}
		if appsInfo, err := os.Stat(filepath.Join(customInstancePath, devStudioAppsDirectory)); err != nil || !appsInfo.IsDir() {
			t.Fatalf("apps directory not created in custom instance root: %v", err)
		}
		if sclInfo, err := os.Stat(filepath.Join(customInstancePath, "simple.scl")); err != nil || sclInfo.IsDir() {
			t.Fatalf("simple.scl not created in custom instance root: %v", err)
		}
		if agentsInfo, err := os.Stat(filepath.Join(customInstancePath, "AGENTS.md")); err != nil || agentsInfo.IsDir() {
			t.Fatalf("AGENTS.md not created in custom instance root: %v", err)
		}
	})
}

func TestDevStudioBridgeHandshake(t *testing.T) {
	_, wsURL := startDevStudioBridge(t)
	connection := dialDevStudio(t, wsURL)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_acme_dev"))

	message := readDevStudioMessage(t, connection)
	if message["type"] != "session.ready" {
		t.Fatalf("type = %q, want session.ready", message["type"])
	}
	if message["protocolVersion"] != float64(1) {
		t.Fatalf("protocolVersion = %v, want 1", message["protocolVersion"])
	}
	if message["instanceId"] != "inst_acme_dev" {
		t.Fatalf("instanceId = %q, want inst_acme_dev", message["instanceId"])
	}
	if message["instanceRootReady"] != true {
		t.Fatalf("instanceRootReady = %v, want true", message["instanceRootReady"])
	}

	// Verify no path, apps, or prohibited fields present
	for _, forbidden := range []string{"path", "apps", "app", "page", "route", "record", "view", "deployKey"} {
		if _, exists := message[forbidden]; exists {
			t.Fatalf("response unexpectedly contained forbidden field %q", forbidden)
		}
	}
}

func TestDevStudioBridgeRequiresDeployKey(t *testing.T) {
	_, wsURL := startDevStudioBridge(t)
	connection := dialDevStudio(t, wsURL)
	defer connection.Close()

	// Missing deployKey
	sendDevStudioMessage(t, connection, map[string]any{
		"type":            "session.connect",
		"protocolVersion": 1,
		"instanceId":      "inst_acme_dev",
		"instanceHost":    "inst_acme_dev.simple.lcl",
	})

	message := readDevStudioMessage(t, connection)
	if message["type"] != "error" {
		t.Fatalf("type = %q, want error", message["type"])
	}
	if message["code"] != "invalid_deploy_key" {
		t.Fatalf("code = %q, want invalid_deploy_key", message["code"])
	}
}

func TestDevStudioBridgeDeployKeyHandlingAndClearing(t *testing.T) {
	bridge, wsURL := startDevStudioBridge(t)
	connection := dialDevStudio(t, wsURL)

	keySecret := "secret_deploy_key_999"
	sendDevStudioMessage(t, connection, map[string]any{
		"type":            "session.connect",
		"protocolVersion": 1,
		"instanceId":      "inst_acme_dev",
		"instanceHost":    "inst_acme_dev.simple.lcl",
		"deployKey":       keySecret,
	})

	message := readDevStudioMessage(t, connection)
	if message["type"] != "session.ready" {
		t.Fatalf("type = %q, want session.ready", message["type"])
	}

	// Check that deployKey is present in internal session state
	bridge.mu.Lock()
	sess := bridge.session
	var storedKey string
	if sess != nil {
		storedKey = sess.deployKey
	}
	bridge.mu.Unlock()

	if storedKey != keySecret {
		t.Fatalf("in-memory deployKey = %q, want %q", storedKey, keySecret)
	}

	// Close connection and verify key cleared
	connection.Close()
	time.Sleep(50 * time.Millisecond)

	bridge.mu.Lock()
	sessAfter := bridge.session
	bridge.mu.Unlock()

	if sessAfter != nil && sessAfter.deployKey != "" {
		t.Fatalf("deployKey after disconnect = %q, want empty", sessAfter.deployKey)
	}
}

func TestDevStudioBridgeRejectsInvalidHandshake(t *testing.T) {
	tests := []struct {
		name    string
		message map[string]any
		code    string
	}{
		{
			name: "invalid protocol version",
			message: map[string]any{
				"type":            "session.connect",
				"protocolVersion": 2,
				"instanceId":      "inst_acme_dev",
				"deployKey":       "test_key",
			},
			code: "unsupported_protocol_version",
		},
		{
			name: "invalid instance id",
			message: map[string]any{
				"type":            "session.connect",
				"protocolVersion": 1,
				"instanceId":      "inst/acme",
				"deployKey":       "test_key",
			},
			code: "invalid_instance_id",
		},
		{
			name: "dot dot instance id",
			message: map[string]any{
				"type":            "session.connect",
				"protocolVersion": 1,
				"instanceId":      "..",
				"deployKey":       "test_key",
			},
			code: "invalid_instance_id",
		},
		{
			name: "instance host does not match instance name",
			message: map[string]any{
				"type":            "session.connect",
				"protocolVersion": 1,
				"instanceId":      "acme",
				"instanceHost":    "other.simple.lcl",
				"deployKey":       "test_key",
			},
			code: "invalid_instance_host",
		},
		{
			name: "unknown message type",
			message: map[string]any{
				"type":            "session.other",
				"protocolVersion": 1,
				"instanceId":      "inst_acme_dev",
				"deployKey":       "test_key",
			},
			code: "unsupported_message_type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, wsURL := startDevStudioBridge(t)
			connection := dialDevStudio(t, wsURL)
			defer connection.Close()

			sendDevStudioMessage(t, connection, test.message)
			message := readDevStudioMessage(t, connection)
			if message["type"] != "error" {
				t.Fatalf("type = %q, want error", message["type"])
			}
			if message["code"] != test.code {
				t.Fatalf("code = %q, want %q", message["code"], test.code)
			}
		})
	}
}

func TestDevStudioBridgeRejectsMalformedMessages(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "invalid JSON",
			payload: `{"type":"session.connect"`,
		},
		{
			name:    "unexpected app context",
			payload: `{"type":"session.connect","protocolVersion":1,"instanceId":"inst_acme_dev","deployKey":"k","appId":"com.example.app"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, wsURL := startDevStudioBridge(t)
			connection := dialDevStudio(t, wsURL)
			defer connection.Close()

			if err := connection.WriteMessage(websocket.TextMessage, []byte(test.payload)); err != nil {
				t.Fatalf("write message: %v", err)
			}
			if message := readDevStudioMessage(t, connection); message["code"] != "invalid_message" {
				t.Fatalf("code = %q, want invalid_message", message["code"])
			}
		})
	}
}

func TestDevStudioBridgeRejectsSecondActiveSession(t *testing.T) {
	_, wsURL := startDevStudioBridge(t)
	first := dialDevStudio(t, wsURL)
	defer first.Close()
	sendDevStudioMessage(t, first, sessionConnectMessage("inst_acme_dev"))
	if message := readDevStudioMessage(t, first); message["type"] != "session.ready" {
		t.Fatalf("first type = %q, want session.ready", message["type"])
	}

	second := dialDevStudio(t, wsURL)
	defer second.Close()
	sendDevStudioMessage(t, second, sessionConnectMessage("inst_acme_dev"))
	if message := readDevStudioMessage(t, second); message["code"] != "session_already_active" {
		t.Fatalf("second code = %q, want session_already_active", message["code"])
	}
}

func TestDevStudioBridgeClearsSessionWhenBrowserDisconnects(t *testing.T) {
	_, wsURL := startDevStudioBridge(t)
	first := dialDevStudio(t, wsURL)
	sendDevStudioMessage(t, first, sessionConnectMessage("inst_acme_dev"))
	if message := readDevStudioMessage(t, first); message["type"] != "session.ready" {
		t.Fatalf("first type = %q, want session.ready", message["type"])
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	second := dialDevStudio(t, wsURL)
	defer second.Close()
	sendDevStudioMessage(t, second, sessionConnectMessage("inst_acme_dev"))
	if message := readDevStudioMessage(t, second); message["type"] != "session.ready" {
		t.Fatalf("second type = %q, want session.ready", message["type"])
	}
}

func TestDevStudioBridgeDoesNotExposeOtherEndpoints(t *testing.T) {
	bridge := newDevStudioBridge()
	server := httptest.NewServer(bridge.handler())
	defer server.Close()

	for _, path := range []string{"/", "/shell", "/v1/devstudio/extra", "/v1/anything"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, response.StatusCode, http.StatusNotFound)
		}
	}
}

func TestDevStudioNoSecretsInErrorsOrServeOutput(t *testing.T) {
	knownSecretKey := "SUPER_SECRET_DEPLOY_KEY_9999"

	protoErr := devStudioProtocolError{
		Type:    "error",
		Code:    "invalid_deploy_key",
		Message: "The deploy key is missing or invalid.",
	}
	encoded, err := json.Marshal(protoErr)
	if err != nil {
		t.Fatalf("json.Marshal devStudioProtocolError: %v", err)
	}
	if strings.Contains(string(encoded), knownSecretKey) || strings.Contains(string(encoded), "deployKey") {
		t.Fatalf("serialized protocol error contained forbidden data: %s", string(encoded))
	}

	setupTestProductionRoot(t, "inst_acme_dev")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdoutBuffer bytes.Buffer

	bridge := newDevStudioBridge()
	done := make(chan error, 1)
	go func() {
		done <- serveDevStudioWithBridge(ctx, listener, &stdoutBuffer, bridge)
	}()

	wsURL := "ws://" + listener.Addr().String() + devStudioWebSocketPath
	conn := dialDevStudio(t, wsURL)

	sendDevStudioMessage(t, conn, map[string]any{
		"type":            "session.connect",
		"protocolVersion": 1,
		"instanceId":      "inst_acme_dev",
		"instanceHost":    "inst_acme_dev.simple.lcl",
		"deployKey":       knownSecretKey,
	})

	msg := readDevStudioMessage(t, conn)
	conn.Close()

	cancel()
	_ = <-done

	rawMsgBytes, _ := json.Marshal(msg)
	if strings.Contains(string(rawMsgBytes), knownSecretKey) {
		t.Fatalf("response payload contained secret key: %s", string(rawMsgBytes))
	}

	if strings.Contains(stdoutBuffer.String(), knownSecretKey) {
		t.Fatalf("serve stdout output contained secret key: %s", stdoutBuffer.String())
	}
}

func TestDevStudioServeShutdownClearsDeployKey(t *testing.T) {
	setupTestProductionRoot(t, "inst_acme_dev")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	bridge := newDevStudioBridge()

	done := make(chan error, 1)
	go func() {
		done <- serveDevStudioWithBridge(ctx, listener, &output, bridge)
	}()

	wsURL := "ws://" + listener.Addr().String() + devStudioWebSocketPath
	conn := dialDevStudio(t, wsURL)
	defer conn.Close()

	secretKey := "shutdown_secret_key_888"
	sendDevStudioMessage(t, conn, map[string]any{
		"type":            "session.connect",
		"protocolVersion": 1,
		"instanceId":      "inst_acme_dev",
		"instanceHost":    "inst_acme_dev.simple.lcl",
		"deployKey":       secretKey,
	})

	msg := readDevStudioMessage(t, conn)
	if msg["type"] != "session.ready" {
		t.Fatalf("type = %q, want session.ready", msg["type"])
	}

	bridge.mu.Lock()
	if bridge.session == nil || bridge.session.deployKey != secretKey {
		bridge.mu.Unlock()
		t.Fatalf("session deployKey = %v, want %q", bridge.session, secretKey)
	}
	bridge.mu.Unlock()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveDevStudioWithBridge error: %v", err)
	}

	bridge.mu.Lock()
	sess := bridge.session
	bridge.mu.Unlock()

	if sess != nil {
		t.Fatalf("bridge.session after shutdown = %v, want nil", sess)
	}
}

func startDevStudioBridge(t *testing.T) (*devStudioBridge, string) {
	t.Helper()
	setupTestProductionRoot(t, "inst_acme_dev")
	bridge := newDevStudioBridge()
	server := httptest.NewServer(bridge.handler())
	t.Cleanup(server.Close)

	return bridge, "ws" + strings.TrimPrefix(server.URL, "http") + devStudioWebSocketPath
}

const defaultTestDevStudioOrigin = "http://inst_acme_dev.simple.lcl"

func dialDevStudio(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	return dialDevStudioWithOrigin(t, wsURL, defaultTestDevStudioOrigin)
}

func dialDevStudioWithOrigin(t *testing.T, wsURL, origin string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	connection, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial %s with origin %q: %v", wsURL, origin, err)
	}
	return connection
}

func dialDevStudioRaw(wsURL string, header http.Header) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial(wsURL, header)
}

func sendDevStudioMessage(t *testing.T, connection *websocket.Conn, message map[string]any) {
	t.Helper()
	if err := connection.WriteJSON(message); err != nil {
		t.Fatalf("write message: %v", err)
	}
}

func readDevStudioMessage(t *testing.T, connection *websocket.Conn) map[string]any {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var message map[string]any
	if err := connection.ReadJSON(&message); err != nil {
		t.Fatalf("read message: %v", err)
	}
	return message
}

func sessionConnectMessage(instanceID string) map[string]any {
	return map[string]any{
		"type":            "session.connect",
		"protocolVersion": 1,
		"instanceId":      instanceID,
		"instanceHost":    instanceID + ".simple.lcl",
		"deployKey":       "test_deploy_key_secret",
	}
}

func TestDevStudioWebSocketURLUsesConfiguredPort(t *testing.T) {
	endpoint := (&url.URL{Scheme: "ws", Host: net.JoinHostPort(devStudioLoopbackHost, devStudioDefaultPort), Path: devStudioWebSocketPath}).String()
	if endpoint != "ws://127.0.0.1:47831/v1/devstudio" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestDevStudioOriginValidation(t *testing.T) {
	allowedCases := []string{
		"http://acme.simple.lcl",
		"https://acme.simple.lcl",
		"http://acme.simple.dev",
		"https://acme.simple.dev",
		"https://acme.on.simple.dev",
		"http://inst_acme_dev.simple.lcl",
		"http://sub.inst.simple.lcl:8080",
		"https://acme.on.simple.dev:443",
		"http://my-app.simple.dev",
		"http://a.b.c.simple.lcl",
		"HTTP://ACME.SIMPLE.LCL",
		"https://Acme.On.Simple.Dev",
		"http://acme.simple.dev/",
	}

	for _, origin := range allowedCases {
		t.Run("allowed: "+origin, func(t *testing.T) {
			if !isValidDevStudioOrigin(origin) {
				t.Fatalf("expected origin %q to be allowed, but was rejected", origin)
			}
		})
	}

	rejectedCases := []struct {
		name   string
		origin string
	}{
		{name: "empty origin", origin: ""},
		{name: "spaces only", origin: "   "},
		{name: "leading space", origin: " http://acme.simple.dev"},
		{name: "trailing space", origin: "http://acme.simple.dev "},
		{name: "null origin", origin: "null"},
		{name: "ws scheme", origin: "ws://acme.simple.lcl"},
		{name: "wss scheme", origin: "wss://acme.simple.dev"},
		{name: "ftp scheme", origin: "ftp://acme.simple.dev"},
		{name: "file scheme", origin: "file:///acme.simple.dev"},
		{name: "javascript scheme", origin: "javascript:alert(1)"},
		{name: "missing subdomain simple.lcl", origin: "http://simple.lcl"},
		{name: "missing subdomain simple.dev", origin: "https://simple.dev"},
		{name: "look-alike not-simple.dev", origin: "http://not-simple.dev"},
		{name: "look-alike evilsimple.dev", origin: "http://evilsimple.dev"},
		{name: "look-alike evil-simple.lcl", origin: "http://evil-simple.lcl"},
		{name: "look-alike prefix simple.dev.attacker.com", origin: "http://simple.dev.attacker.com"},
		{name: "look-alike prefix acme.simple.dev.attacker.com", origin: "http://acme.simple.dev.attacker.com"},
		{name: "look-alike prefix acme.simple.lcl.evil.com", origin: "http://acme.simple.lcl.evil.com"},
		{name: "look-alike suffix acme.simple.dev.evil", origin: "http://acme.simple.dev.evil"},
		{name: "look-alike tld acme.simple.developer.com", origin: "http://acme.simple.developer.com"},
		{name: "look-alike acme.simple.lclevil.com", origin: "http://acme.simple.lclevil.com"},
		{name: "fragment look-alike attacker.com#.simple.dev", origin: "http://attacker.com#.simple.dev"},
		{name: "query look-alike attacker.com?.simple.dev", origin: "http://attacker.com?.simple.dev"},
		{name: "path look-alike attacker.com/.simple.dev", origin: "http://attacker.com/.simple.dev"},
		{name: "empty prefix .simple.dev", origin: "http://.simple.dev"},
		{name: "double dot prefix ..simple.dev", origin: "http://..simple.dev"},
		{name: "empty middle label acme..simple.dev", origin: "http://acme..simple.dev"},
		{name: "trailing dot acme.simple.dev.", origin: "http://acme.simple.dev."},
		{name: "userinfo in origin", origin: "http://user:pass@acme.simple.dev"},
		{name: "path in origin", origin: "http://acme.simple.dev/evil/path"},
		{name: "query in origin", origin: "http://acme.simple.dev?param=value"},
		{name: "fragment in origin", origin: "http://acme.simple.dev#fragment"},
		{name: "invalid port in origin", origin: "http://acme.simple.dev:invalidport"},
		{name: "loopback IP origin", origin: "http://127.0.0.1:47831"},
		{name: "localhost origin", origin: "http://localhost:3000"},
		{name: "unrelated domain google.com", origin: "https://google.com"},
		{name: "unrelated domain github.com", origin: "https://github.com"},
		{name: "unrelated domain example.com", origin: "http://example.com"},
		{name: "malformed URI", origin: "not a url %%%"},
	}

	for _, tc := range rejectedCases {
		t.Run("rejected: "+tc.name, func(t *testing.T) {
			if isValidDevStudioOrigin(tc.origin) {
				t.Fatalf("expected origin %q (%s) to be rejected, but was accepted", tc.origin, tc.name)
			}
		})
	}
}

func TestDevStudioWebSocketOriginEnforcement(t *testing.T) {
	_, wsURL := startDevStudioBridge(t)

	t.Run("accepts valid origin and establishes session", func(t *testing.T) {
		validOrigins := []string{
			"http://inst_acme_dev.simple.lcl",
			"https://inst_acme_dev.simple.dev",
			"https://acme.on.simple.dev",
			"http://inst_acme_dev.simple.lcl:8080",
		}
		for _, origin := range validOrigins {
			conn := dialDevStudioWithOrigin(t, wsURL, origin)
			sendDevStudioMessage(t, conn, sessionConnectMessage("inst_acme_dev"))
			msg := readDevStudioMessage(t, conn)
			if msg["type"] != "session.ready" {
				t.Fatalf("origin %q: expected session.ready, got %v", origin, msg)
			}
			conn.Close()
			time.Sleep(20 * time.Millisecond)
		}
	})

	t.Run("rejects missing origin header at WebSocket upgrade", func(t *testing.T) {
		conn, resp, err := dialDevStudioRaw(wsURL, nil)
		if err == nil {
			if conn != nil {
				conn.Close()
			}
			t.Fatal("expected dial without origin to fail, got nil error")
		}
		if resp != nil && resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected status 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	t.Run("rejects empty origin header", func(t *testing.T) {
		header := http.Header{"Origin": []string{""}}
		conn, resp, err := dialDevStudioRaw(wsURL, header)
		if err == nil {
			if conn != nil {
				conn.Close()
			}
			t.Fatal("expected dial with empty origin to fail, got nil error")
		}
		if resp != nil && resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected status 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	t.Run("rejects multiple origin headers", func(t *testing.T) {
		header := http.Header{"Origin": []string{"http://inst_acme_dev.simple.lcl", "http://evil.com"}}
		conn, resp, err := dialDevStudioRaw(wsURL, header)
		if err == nil {
			if conn != nil {
				conn.Close()
			}
			t.Fatal("expected dial with multiple origins to fail, got nil error")
		}
		if resp != nil && resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected status 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	rejectedOrigins := []struct {
		name   string
		origin string
	}{
		{name: "look-alike not-simple.dev", origin: "http://not-simple.dev"},
		{name: "look-alike evilsimple.dev", origin: "http://evilsimple.dev"},
		{name: "prefix look-alike simple.dev.attacker.com", origin: "http://simple.dev.attacker.com"},
		{name: "prefix look-alike acme.simple.lcl.evil.com", origin: "http://acme.simple.lcl.evil.com"},
		{name: "suffix look-alike acme.simple.dev.evil", origin: "http://acme.simple.dev.evil"},
		{name: "unrelated localhost", origin: "http://localhost:3000"},
		{name: "unrelated loopback IP", origin: "http://127.0.0.1:47831"},
		{name: "unrelated domain", origin: "https://evil.com"},
		{name: "null origin", origin: "null"},
		{name: "ws scheme", origin: "ws://inst_acme_dev.simple.lcl"},
		{name: "malformed label", origin: "http://acme..simple.dev"},
	}

	for _, tc := range rejectedOrigins {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			header := http.Header{"Origin": []string{tc.origin}}
			conn, resp, err := dialDevStudioRaw(wsURL, header)
			if err == nil {
				if conn != nil {
					conn.Close()
				}
				t.Fatalf("expected dial with origin %q (%s) to fail, got nil error", tc.origin, tc.name)
			}
			if resp != nil && resp.StatusCode != http.StatusForbidden {
				t.Fatalf("expected status 403 Forbidden, got %d", resp.StatusCode)
			}
		})
	}
}

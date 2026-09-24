package deploy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewClient(t *testing.T) {
	cfg := ClientConfig{
		Endpoint: "devops.acme.simple.dev",
		JWT:      "test-jwt",
		Timeout:  10 * time.Second,
	}

	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.endpoint != cfg.Endpoint {
		t.Errorf("NewClient() endpoint = %q, want %q", client.endpoint, cfg.Endpoint)
	}
	if client.jwt != cfg.JWT {
		t.Errorf("NewClient() jwt = %q, want %q", client.jwt, cfg.JWT)
	}
	if client.timeout != cfg.Timeout {
		t.Errorf("NewClient() timeout = %v, want %v", client.timeout, cfg.Timeout)
	}
}

func TestNewClient_DefaultTimeout(t *testing.T) {
	cfg := ClientConfig{
		Endpoint: "devops.acme.simple.dev",
		JWT:      "test-jwt",
		// No timeout specified
	}

	client := NewClient(cfg)

	// Installs are synchronous server-side and scale with app size, so the
	// default must be generous enough that a slow-but-healthy install is never
	// reported as a failure.
	if client.timeout != DefaultTimeout {
		t.Errorf("NewClient() default timeout = %v, want %v", client.timeout, DefaultTimeout)
	}

	if DefaultTimeout < 10*time.Minute {
		t.Errorf("DefaultTimeout = %v, too short for record-heavy app installs", DefaultTimeout)
	}
}

func TestClient_JoinChannel_NotConnected(t *testing.T) {
	client := &Client{
		endpoint: "test",
		jwt:      "jwt",
		timeout:  5 * time.Second,
	}

	err := client.JoinChannel("com.example.app")
	if err == nil {
		t.Error("JoinChannel() expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("JoinChannel() error = %v, want containing 'not connected'", err)
	}
}

func TestClient_SendManifest_NotJoined(t *testing.T) {
	client := &Client{
		timeout: 5 * time.Second,
	}

	_, err := client.SendManifest(nil, "1.0.0")
	if err == nil {
		t.Error("SendManifest() expected error when not joined")
	}
	if !strings.Contains(err.Error(), "not joined") {
		t.Errorf("SendManifest() error = %v, want containing 'not joined'", err)
	}
}

func TestClient_SendFiles_NotJoined(t *testing.T) {
	client := &Client{
		timeout: 5 * time.Second,
	}

	err := client.SendFiles(nil, []string{"file1.txt"})
	if err == nil {
		t.Error("SendFiles() expected error when not joined")
	}
	if !strings.Contains(err.Error(), "not joined") {
		t.Errorf("SendFiles() error = %v, want containing 'not joined'", err)
	}
}

func TestClient_SendFiles_EmptyList(t *testing.T) {
	client := &Client{
		timeout: 5 * time.Second,
	}

	err := client.SendFiles(nil, []string{})
	if err != nil && !strings.Contains(err.Error(), "not joined") {
		t.Errorf("SendFiles() unexpected error = %v", err)
	}
}

func TestClient_Deploy_NotJoined(t *testing.T) {
	client := &Client{
		timeout: 5 * time.Second,
	}

	_, err := client.Deploy()
	if err == nil {
		t.Error("Deploy() expected error when not joined")
	}
	if !strings.Contains(err.Error(), "not joined") {
		t.Errorf("Deploy() error = %v, want containing 'not joined'", err)
	}
}

func TestClient_Install_NotConnected(t *testing.T) {
	client := &Client{timeout: 5 * time.Second}

	_, err := client.Install()
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("Install() error = %v, want containing 'not connected'", err)
	}
}

func TestClient_Close_NilSocketAndChannel(t *testing.T) {
	client := &Client{}

	// Should not panic
	client.Close()
}

func TestClient_IsConnected_NotConnected(t *testing.T) {
	client := &Client{}

	if client.IsConnected() {
		t.Error("IsConnected() should return false when socket is nil")
	}
}

// Integration tests using mock Phoenix server

func startClientMockServer(_ *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/websocket") {
			http.NotFound(w, r)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		handler(conn)
	}))

	return server
}

func TestClient_ConnectAndJoin_Placeholder(t *testing.T) {
	// Placeholder test - full connect testing done via socket injection
	// This tests that the mock server infrastructure works
	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				msg := decodeJSONMessageFast(data)
				if msg != nil && msg.Event == "phx_join" {
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	// Verify server is running
	if server.URL == "" {
		t.Error("Mock server URL is empty")
	}
}

func TestClient_SendManifest_Integration(t *testing.T) {
	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				msg := decodeJSONMessageFast(data)
				if msg == nil {
					continue
				}

				switch msg.Event {
				case "phx_join":
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				case "manifest":
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{"need_files": []string{"file1.txt"}}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	// Test with direct socket injection
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := parseEndpointURL(wsURL)

	socket := NewPhoenixSocket(u)
	if err := socket.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer socket.Disconnect()

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(5 * time.Second); err != nil {
		t.Fatalf("Join error: %v", err)
	}

	// Create client with injected socket/channel
	client := &Client{
		socket:   socket,
		channel:  channel,
		appID:    "com.test.app",
		timeout:  5 * time.Second,
		endpoint: wsURL,
	}

	files := map[string]FileInfo{
		"file1.txt": {Path: "file1.txt", Hash: "abc123", Size: 100},
	}

	needed, err := client.SendManifest(files, "1.0.0")
	if err != nil {
		t.Fatalf("SendManifest error: %v", err)
	}

	if len(needed) != 1 || needed[0] != "file1.txt" {
		t.Errorf("SendManifest() needed = %v, want [file1.txt]", needed)
	}
}

func TestClient_SendFiles_Integration(t *testing.T) {
	receivedFiles := make(map[string]bool)

	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				msg := decodeJSONMessageFast(data)
				if msg == nil {
					continue
				}

				if msg.Event == "phx_join" {
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			} else if msgType == websocket.BinaryMessage {
				// Binary file received
				receivedFiles["binary"] = true

				// Decode to get ref and send reply
				msg := decodeBinaryMessageFast(data)
				if msg != nil {
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := parseEndpointURL(wsURL)

	socket := NewPhoenixSocket(u)
	if err := socket.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer socket.Disconnect()

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(5 * time.Second); err != nil {
		t.Fatalf("Join error: %v", err)
	}

	client := &Client{
		socket:  socket,
		channel: channel,
		appID:   "com.test.app",
		timeout: 5 * time.Second,
	}

	files := map[string]FileInfo{
		"file1.txt": {Path: "file1.txt", Hash: "abc123", Size: 11, Content: []byte("hello world")},
	}

	err := client.SendFiles(files, []string{"file1.txt"})
	if err != nil {
		t.Fatalf("SendFiles error: %v", err)
	}

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	if !receivedFiles["binary"] {
		t.Error("Server did not receive binary file")
	}
}

func TestClient_Deploy_Integration(t *testing.T) {
	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				msg := decodeJSONMessageFast(data)
				if msg == nil {
					continue
				}

				switch msg.Event {
				case "phx_join":
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				case "deploy":
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{"version": "1.0.0", "file_count": float64(5)}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := parseEndpointURL(wsURL)

	socket := NewPhoenixSocket(u)
	if err := socket.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer socket.Disconnect()

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(5 * time.Second); err != nil {
		t.Fatalf("Join error: %v", err)
	}

	client := &Client{
		socket:  socket,
		channel: channel,
		appID:   "com.test.app",
		timeout: 5 * time.Second,
	}

	result, err := client.Deploy()
	if err != nil {
		t.Fatalf("Deploy error: %v", err)
	}

	if result.Version != "1.0.0" {
		t.Errorf("Deploy() version = %q, want %q", result.Version, "1.0.0")
	}
	if result.FileCount != 5 {
		t.Errorf("Deploy() file_count = %d, want 5", result.FileCount)
	}
	if result.AppID != "com.test.app" {
		t.Errorf("Deploy() app_id = %q, want %q", result.AppID, "com.test.app")
	}
}

func TestClient_Close_WithConnection(t *testing.T) {
	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				msg := decodeJSONMessageFast(data)
				if msg != nil && msg.Event == "phx_join" {
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := parseEndpointURL(wsURL)

	socket := NewPhoenixSocket(u)
	if err := socket.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(5 * time.Second); err != nil {
		t.Fatalf("Join error: %v", err)
	}

	client := &Client{
		socket:  socket,
		channel: channel,
		appID:   "com.test.app",
		timeout: 5 * time.Second,
	}

	// Should not panic
	client.Close()

	// Should be disconnected
	if client.IsConnected() {
		t.Error("IsConnected() should return false after Close()")
	}
}

// Helper to parse endpoint URL
func parseEndpointURL(rawURL string) (*url.URL, error) {
	return url.Parse(rawURL)
}

// joinedClient connects a Client to server and joins the deploy channel of
// com.test.app, the way the CLI does.
func joinedClient(t *testing.T, server *httptest.Server, timeout time.Duration) *Client {
	t.Helper()
	client := NewClient(ClientConfig{
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		JWT:      "test-token",
		Timeout:  timeout,
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(client.Close)

	if err := client.JoinChannel("com.test.app"); err != nil {
		t.Fatalf("JoinChannel() error = %v", err)
	}
	return client
}

// TestClient_Replies drives each request of the deploy protocol through the
// server's possible answers: success, a server-reported error, and silence.
func TestClient_Replies(t *testing.T) {
	manifest := func(c *Client) (any, error) {
		return c.SendManifest(map[string]FileInfo{"a.txt": {Hash: "h", Size: 1}}, "1.0.0")
	}
	publish := func(c *Client) (any, error) { return c.Deploy() }
	install := func(c *Client) (any, error) { return c.Install() }
	upload := func(c *Client) (any, error) {
		return nil, c.SendFiles(map[string]FileInfo{"a.txt": {Hash: "h", Size: 1, Content: []byte("x")}}, []string{"a.txt"})
	}

	tests := []struct {
		name  string
		event string
		// status is the server's reply status; "" means it never answers.
		status   string
		response any
		call     func(*Client) (any, error)
		want     any
		wantErr  string
		wantIs   error
	}{
		{
			name: "manifest lists the needed files", event: "manifest", status: "ok",
			response: map[string]any{"need_files": []string{"a.txt"}},
			call:     manifest, want: []string{"a.txt"},
		},
		{
			name: "manifest without need_files needs nothing", event: "manifest", status: "ok",
			response: map[string]any{},
			call:     manifest, want: []string{},
		},
		{
			name: "manifest rejected", event: "manifest", status: "error",
			response: map[string]any{"message": "Invalid version format"},
			call:     manifest, wantErr: "manifest rejected: map[message:Invalid version format]",
		},
		{
			name: "manifest unanswered", event: "manifest",
			call: manifest, wantErr: "manifest: reply timeout", wantIs: ErrReplyTimeout,
		},
		{
			name: "upload acknowledged", event: "file", status: "ok",
			response: map[string]any{},
			call:     upload,
		},
		{
			name: "upload rejected", event: "file", status: "error",
			response: map[string]any{"message": "Invalid binary format"},
			call:     upload, wantErr: "file rejected for a.txt: map[message:Invalid binary format]",
		},
		{
			name: "upload unanswered", event: "file",
			call: upload, wantErr: "upload a.txt: reply timeout", wantIs: ErrReplyTimeout,
		},
		{
			name: "deploy published", event: "deploy", status: "ok",
			response: map[string]any{"version": "1.0.0", "file_count": 3},
			call:     publish, want: &DeployResult{AppID: "com.test.app", Version: "1.0.0", FileCount: 3},
		},
		{
			name: "deploy failed with a message", event: "deploy", status: "error",
			response: map[string]any{"message": "storage unavailable"},
			call:     publish, wantErr: "deploy failed: storage unavailable",
		},
		{
			name: "deploy failed without a message", event: "deploy", status: "error",
			response: "boom",
			call:     publish, wantErr: "deploy failed: unknown error",
		},
		{
			name: "deploy unanswered", event: "deploy",
			call: publish, wantErr: "deploy: reply timeout", wantIs: ErrReplyTimeout,
		},
		{
			name: "install done", event: "install", status: "ok",
			response: map[string]any{"version": "1.0.0"},
			call:     install, want: &InstallResult{AppID: "com.test.app", Version: "1.0.0", Success: true},
		},
		{
			name: "install failure keeps the server message", event: "install", status: "error",
			response: map[string]any{"message": "Version `1.0.0` of application `com.test.app` is already installed"},
			call:     install, wantErr: "Version `1.0.0` of application `com.test.app` is already installed",
		},
		{
			name: "install failure without a message", event: "install", status: "error",
			response: map[string]any{"code": "S3_ERROR"},
			call:     install, wantErr: "install failed",
		},
		{
			name: "install unanswered", event: "install",
			call: install, wantErr: "install: reply timeout", wantIs: ErrReplyTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
				if msg.Event == tt.event && tt.status != "" {
					writeReply(conn, msg, tt.status, tt.response)
				}
				return true
			}))
			defer server.Close()

			timeout := 5 * time.Second
			if tt.status == "" {
				timeout = 100 * time.Millisecond
			}
			client := joinedClient(t, server, timeout)

			got, err := tt.call(client)
			if tt.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want starting with %q", err, tt.wantErr)
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Errorf("error = %v, want wrapping %v", err, tt.wantIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error = %v", err)
			}
			if tt.want != nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("result = %#v, want %#v", got, tt.want)
			}
		})
	}
}

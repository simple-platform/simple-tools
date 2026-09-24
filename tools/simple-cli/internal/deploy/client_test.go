package deploy

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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

	err := client.JoinChannel(t.Context(), "com.example.app")
	if err == nil {
		t.Error("JoinChannel() expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("JoinChannel() error = %v, want containing 'not connected'", err)
	}
}

func TestClient_JoinWait(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "default reply timeout is capped", timeout: DefaultTimeout, want: joinTimeout},
		{name: "longer reply timeout is capped", timeout: time.Hour, want: joinTimeout},
		{name: "shorter reply timeout is kept", timeout: 100 * time.Millisecond, want: 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(ClientConfig{Endpoint: "devops.acme.simple.dev", Timeout: tt.timeout})
			if got := client.joinWait(); got != tt.want {
				t.Errorf("joinWait() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A server that accepts the socket but never answers the join must not hold
// the CLI for the reply timeout.
func TestClient_JoinChannel_Timeout(t *testing.T) {
	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		JWT:      "test-token",
		Timeout:  100 * time.Millisecond,
	})
	if err := client.Connect(t.Context()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	err := client.JoinChannel(t.Context(), "com.test.app")
	if !errors.Is(err, ErrReplyTimeout) {
		t.Fatalf("JoinChannel() error = %v, want ErrReplyTimeout", err)
	}
	if want := "failed to join channel: join: reply timeout after 100ms"; err.Error() != want {
		t.Errorf("JoinChannel() error = %q, want %q", err, want)
	}
}

func TestClient_SendManifest_NotJoined(t *testing.T) {
	client := &Client{
		timeout: 5 * time.Second,
	}

	_, err := client.SendManifest(t.Context(), nil, "1.0.0")
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

	err := client.SendFiles(t.Context(), nil, []string{"file1.txt"}, nil)
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

	err := client.SendFiles(t.Context(), nil, []string{}, nil)
	if err != nil && !strings.Contains(err.Error(), "not joined") {
		t.Errorf("SendFiles() unexpected error = %v", err)
	}
}

func TestClient_Deploy_NotJoined(t *testing.T) {
	client := &Client{
		timeout: 5 * time.Second,
	}

	_, err := client.Deploy(t.Context())
	if err == nil {
		t.Error("Deploy() expected error when not joined")
	}
	if !strings.Contains(err.Error(), "not joined") {
		t.Errorf("Deploy() error = %v, want containing 'not joined'", err)
	}
}

func TestClient_Install_NotJoined(t *testing.T) {
	client := &Client{timeout: 5 * time.Second}

	_, err := client.Install(t.Context())
	if err == nil || !strings.Contains(err.Error(), "not joined") {
		t.Errorf("Install() error = %v, want containing 'not joined'", err)
	}
}

func TestClient_Close_NilSocketAndChannel(t *testing.T) {
	client := &Client{}

	// Should not panic
	client.Close()
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
	if err := socket.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer socket.Disconnect()

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(t.Context(), 5*time.Second); err != nil {
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

	needed, err := client.SendManifest(t.Context(), files, "1.0.0")
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
	if err := socket.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer socket.Disconnect()

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(t.Context(), 5*time.Second); err != nil {
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

	err := client.SendFiles(t.Context(), files, []string{"file1.txt"}, nil)
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
	if err := socket.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer socket.Disconnect()

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(t.Context(), 5*time.Second); err != nil {
		t.Fatalf("Join error: %v", err)
	}

	client := &Client{
		socket:  socket,
		channel: channel,
		appID:   "com.test.app",
		timeout: 5 * time.Second,
	}

	result, err := client.Deploy(t.Context())
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
	if err := socket.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	channel := socket.Channel("deploy:com.test.app")
	if err := channel.Join(t.Context(), 5*time.Second); err != nil {
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
	if socket.IsConnected() {
		t.Error("socket still connected after Close()")
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
	if err := client.Connect(t.Context()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(client.Close)

	if err := client.JoinChannel(t.Context(), "com.test.app"); err != nil {
		t.Fatalf("JoinChannel() error = %v", err)
	}
	return client
}

// TestClient_Replies drives each request of the deploy protocol through the
// server's possible answers: success, a server-reported error, and silence.
func TestClient_Replies(t *testing.T) {
	manifest := func(ctx context.Context, c *Client) (any, error) {
		return c.SendManifest(ctx, map[string]FileInfo{"a.txt": {Hash: "h", Size: 1}}, "1.0.0")
	}
	publish := func(ctx context.Context, c *Client) (any, error) { return c.Deploy(ctx) }
	install := func(ctx context.Context, c *Client) (any, error) { return c.Install(ctx) }
	upload := func(ctx context.Context, c *Client) (any, error) {
		return nil, c.SendFiles(ctx, map[string]FileInfo{"a.txt": {Hash: "h", Size: 1, Content: []byte("x")}}, []string{"a.txt"}, nil)
	}

	tests := []struct {
		name  string
		event string
		// status is the server's reply status; "" means it never answers.
		status   string
		response any
		call     func(context.Context, *Client) (any, error)
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

			got, err := tt.call(t.Context(), client)
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

// A server restart or a dropped load balancer used to leave the CLI waiting
// out the whole reply timeout (15 minutes by default) for an answer that could
// no longer come. Each wait now ends as soon as the connection or channel goes.
func TestClient_WaitsEndWhenConnectionDrops(t *testing.T) {
	files := map[string]FileInfo{"a.txt": {Hash: "h", Size: 1, Content: []byte("x")}}

	tests := []struct {
		name  string
		event string
		// drop is how the server goes away once the request arrives.
		drop    func(conn *websocket.Conn, msg *phoenixMessage) bool
		call    func(context.Context, *Client) error
		wantErr string
		wantIs  error
	}{
		{
			name:  "install loses the connection",
			event: "install",
			drop:  func(*websocket.Conn, *phoenixMessage) bool { return false },
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Install(ctx)
				return err
			},
			wantErr: "install: connection to the devops server was lost: ",
			wantIs:  ErrConnectionLost,
		},
		{
			name:  "deploy channel crashes",
			event: "deploy",
			drop: func(conn *websocket.Conn, msg *phoenixMessage) bool {
				writeChannelEvent(conn, msg.JoinRef, msg.Topic, "phx_error")
				return true
			},
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Deploy(ctx)
				return err
			},
			wantErr: "deploy: the devops server closed the deploy channel (phx_error)",
			wantIs:  ErrChannelClosed,
		},
		{
			name:  "manifest channel closes",
			event: "manifest",
			drop: func(conn *websocket.Conn, msg *phoenixMessage) bool {
				writeChannelEvent(conn, msg.JoinRef, msg.Topic, "phx_close")
				return true
			},
			call: func(ctx context.Context, c *Client) error {
				_, err := c.SendManifest(ctx, files, "1.0.0")
				return err
			},
			wantErr: "manifest: the devops server closed the deploy channel (phx_close)",
			wantIs:  ErrChannelClosed,
		},
		{
			name:    "upload loses the connection",
			event:   "file",
			drop:    func(*websocket.Conn, *phoenixMessage) bool { return false },
			call:    func(ctx context.Context, c *Client) error { return c.SendFiles(ctx, files, []string{"a.txt"}, nil) },
			wantErr: "upload a.txt: connection to the devops server was lost: ",
			wantIs:  ErrConnectionLost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
				if msg.Event == tt.event {
					return tt.drop(conn, msg)
				}
				return true
			}))
			defer server.Close()
			client := joinedClient(t, server, time.Minute)

			start := time.Now()
			err := tt.call(t.Context(), client)
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("the wait took %s after the server went away", elapsed)
			}
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want starting with %q", err, tt.wantErr)
			}
			if !errors.Is(err, tt.wantIs) {
				t.Errorf("error = %v, want wrapping %v", err, tt.wantIs)
			}
		})
	}
}

// Once the connection is gone, a later request is refused before it is sent,
// so its error must not claim the server might still be acting on it.
func TestClient_RequestAfterDropIsNotSent(t *testing.T) {
	server := startClientMockServer(t, channelServer(func(*websocket.Conn, *phoenixMessage) bool {
		return false
	}))
	defer server.Close()
	client := joinedClient(t, server, time.Minute)

	if _, err := client.Deploy(t.Context()); !errors.Is(err, ErrConnectionLost) {
		t.Fatalf("Deploy() error = %v, want ErrConnectionLost", err)
	}

	_, err := client.Install(t.Context())
	if err == nil || !strings.HasPrefix(err.Error(), "install: request not sent: connection to the devops server was lost") {
		t.Fatalf("Install() error = %v, want an install that was not sent", err)
	}
	if errors.Is(err, ErrConnectionLost) {
		t.Errorf("Install() error = %v wraps ErrConnectionLost, but the install never left the client", err)
	}
}

// Cancelling the context ends a request's wait at once. The server is not
// told, so the error is the context's.
func TestClient_ContextEndsWait(t *testing.T) {
	files := map[string]FileInfo{"a.txt": {Hash: "h", Size: 1, Content: []byte("x")}}

	tests := []struct {
		name    string
		event   string
		call    func(context.Context, *Client) error
		wantErr string
	}{
		{
			name:  "manifest",
			event: "manifest",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.SendManifest(ctx, files, "1.0.0")
				return err
			},
			wantErr: "manifest: context canceled",
		},
		{
			name:    "upload",
			event:   "file",
			call:    func(ctx context.Context, c *Client) error { return c.SendFiles(ctx, files, []string{"a.txt"}, nil) },
			wantErr: "upload a.txt: context canceled",
		},
		{
			name:  "deploy",
			event: "deploy",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Deploy(ctx)
				return err
			},
			wantErr: "deploy: context canceled",
		},
		{
			name:  "install",
			event: "install",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.Install(ctx)
				return err
			},
			wantErr: "install: context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := startClientMockServer(t, channelServer(func(_ *websocket.Conn, msg *phoenixMessage) bool {
				if msg.Event == tt.event {
					cancel() // and never reply
				}
				return true
			}))
			defer server.Close()
			client := joinedClient(t, server, time.Minute)

			start := time.Now()
			err := tt.call(ctx, client)
			if err == nil || err.Error() != tt.wantErr || !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want %q wrapping context.Canceled", err, tt.wantErr)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("the wait took %s after the context was cancelled", elapsed)
			}
		})
	}
}

// uploadFiles builds n small files named f0000.txt, f0001.txt, … and the
// list of their paths.
func uploadFiles(n int) (map[string]FileInfo, []string) {
	files := make(map[string]FileInfo, n)
	paths := make([]string, 0, n)
	for i := range n {
		path := fmt.Sprintf("f%04d.txt", i)
		content := []byte(path)
		files[path] = FileInfo{Path: path, Hash: "h" + path, Size: int64(len(content)), Content: content}
		paths = append(paths, path)
	}
	return files, paths
}

// uploadedPath returns the path in a "file" push's metadata.
func uploadedPath(msg *phoenixMessage) string {
	payload, _ := msg.Payload.([]byte)
	if len(payload) < 4 {
		return ""
	}
	var meta map[string]string
	_ = json.Unmarshal(payload[4:4+binary.BigEndian.Uint32(payload[0:4])], &meta)
	return meta["path"]
}

func TestClient_UploadWorkers(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
		jobs        int
		want        int
	}{
		{name: "unset uses the default", concurrency: 0, jobs: 1000, want: defaultUploadConcurrency},
		{name: "negative uses the default", concurrency: -1, jobs: 1000, want: defaultUploadConcurrency},
		{name: "configured", concurrency: 4, jobs: 1000, want: 4},
		{name: "never more workers than files", concurrency: 32, jobs: 3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(ClientConfig{UploadConcurrency: tt.concurrency})
			if got := client.uploadWorkers(tt.jobs); got != tt.want {
				t.Errorf("uploadWorkers(%d) = %d, want %d", tt.jobs, got, tt.want)
			}
		})
	}
}

func TestClient_SendFiles_Uploads(t *testing.T) {
	many, manyPaths := uploadFiles(1500)
	two, _ := uploadFiles(2)

	tests := []struct {
		name   string
		files  map[string]FileInfo
		needed []string
		want   int // files the server should receive
	}{
		// More than the socket's 1000-message send queue: starting every
		// upload at once used to fail with "send queue full".
		{name: "many files", files: many, needed: manyPaths, want: 1500},
		{name: "needed path that was not collected is skipped", files: two, needed: []string{"f0000.txt", "missing.txt", "f0001.txt"}, want: 2},
		{name: "nothing needed", files: two, needed: nil, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			received := map[string]bool{}
			server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
				mu.Lock()
				received[uploadedPath(msg)] = true
				mu.Unlock()
				writeReply(conn, msg, "ok", map[string]any{})
				return true
			}))
			defer server.Close()
			client := joinedClient(t, server, 10*time.Second)

			if err := client.SendFiles(t.Context(), tt.files, tt.needed, nil); err != nil {
				t.Fatalf("SendFiles() error = %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(received) != tt.want {
				t.Errorf("server received %d files, want %d", len(received), tt.want)
			}
			if received["missing.txt"] || received[""] {
				t.Errorf("server received a path that was not collected: %v", received)
			}
		})
	}
}

// The server holds its acknowledgements until the client goes quiet, so every
// upload the client starts is outstanding at once and can be counted.
func TestClient_SendFiles_BoundsInFlight(t *testing.T) {
	const limit = 4
	files, paths := uploadFiles(20)

	var maxOutstanding, total atomic.Int64
	server := startClientMockServer(t, func(conn *websocket.Conn) {
		incoming := make(chan *phoenixMessage)
		go func() {
			defer close(incoming)
			for {
				msgType, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if msg := decodeFrame(msgType, data); msg != nil && msg.Topic != "phoenix" {
					incoming <- msg
				}
			}
		}()

		var held []*phoenixMessage
		for {
			select {
			case msg, ok := <-incoming:
				if !ok {
					return
				}
				if msg.Event == "phx_join" {
					writeReply(conn, msg, "ok", map[string]any{})
					continue
				}
				held = append(held, msg)
				total.Add(1)
				if n := int64(len(held)); n > maxOutstanding.Load() {
					maxOutstanding.Store(n)
				}
			case <-time.After(50 * time.Millisecond):
				// Quiet: the client has sent all it will without an answer.
				for _, msg := range held {
					writeReply(conn, msg, "ok", map[string]any{})
				}
				held = held[:0]
			}
		}
	})
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint:          "ws" + strings.TrimPrefix(server.URL, "http"),
		JWT:               "test-token",
		Timeout:           10 * time.Second,
		UploadConcurrency: limit,
	})
	if err := client.Connect(t.Context()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()
	if err := client.JoinChannel(t.Context(), "com.test.app"); err != nil {
		t.Fatalf("JoinChannel() error = %v", err)
	}

	if err := client.SendFiles(t.Context(), files, paths, nil); err != nil {
		t.Fatalf("SendFiles() error = %v", err)
	}
	if got := total.Load(); got != int64(len(paths)) {
		t.Errorf("server received %d uploads, want %d", got, len(paths))
	}
	if got := maxOutstanding.Load(); got > limit {
		t.Errorf("%d uploads were in flight at once, want at most %d", got, limit)
	} else if got < 2 {
		t.Errorf("at most %d upload was in flight at once, want them to overlap", got)
	}
}

func TestClient_SendFiles_StopsAfterFirstError(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
	}{
		{name: "one at a time", concurrency: 1},
		{name: "four at a time", concurrency: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, paths := uploadFiles(20)
			var received atomic.Int64
			server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
				received.Add(1)
				writeReply(conn, msg, "error", map[string]any{"message": "Invalid binary format"})
				return true
			}))
			defer server.Close()

			client := joinedClient(t, server, 10*time.Second)
			client.uploadConcurrency = tt.concurrency

			err := client.SendFiles(t.Context(), files, paths, nil)
			if err == nil || !strings.HasPrefix(err.Error(), "file rejected for f00") {
				t.Fatalf("SendFiles() error = %v, want the server's rejection", err)
			}
			// Only the uploads already in flight when the first one failed
			// were sent; no new one started after it.
			if got := received.Load(); got > int64(tt.concurrency) {
				t.Errorf("server received %d uploads, want at most %d", got, tt.concurrency)
			}
		})
	}
}

func TestClient_SendFiles_ContextCanceled(t *testing.T) {
	tests := []struct {
		name         string
		cancelBefore bool // cancel before SendFiles is called
		wantReceived int64
	}{
		{name: "cancelled before the first upload", cancelBefore: true, wantReceived: 0},
		// The server cancels on the first upload and never answers it.
		{name: "cancelled while uploading", wantReceived: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			files, paths := uploadFiles(10)
			var received atomic.Int64
			server := startClientMockServer(t, channelServer(func(*websocket.Conn, *phoenixMessage) bool {
				received.Add(1)
				cancel()
				return true
			}))
			defer server.Close()

			client := joinedClient(t, server, time.Minute)
			client.uploadConcurrency = 1
			if tt.cancelBefore {
				cancel()
			}

			start := time.Now()
			err := client.SendFiles(ctx, files, paths, nil)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("SendFiles() error = %v, want context.Canceled", err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("SendFiles() took %s after the context was cancelled", elapsed)
			}
			if got := received.Load(); got != tt.wantReceived {
				t.Errorf("server received %d uploads, want %d", got, tt.wantReceived)
			}
		})
	}
}

func TestClient_SendFiles_ReportsProgress(t *testing.T) {
	files := map[string]FileInfo{
		"a.txt": {Hash: "ha", Size: 10, Content: make([]byte, 10)},
		"b.txt": {Hash: "hb", Size: 20, Content: make([]byte, 20)},
		"c.txt": {Hash: "hc", Size: 30, Content: make([]byte, 30)},
	}
	// missing.txt was never collected: it is skipped and left out of the
	// totals, so the count can still reach them.
	needed := []string{"a.txt", "missing.txt", "b.txt", "c.txt"}

	tests := []struct {
		name   string
		reject string // path the server rejects, if any
		want   []UploadProgress
	}{
		{
			name: "every acknowledgement is counted",
			want: []UploadProgress{
				{FilesDone: 1, FilesTotal: 3, BytesDone: 10, BytesTotal: 60},
				{FilesDone: 2, FilesTotal: 3, BytesDone: 30, BytesTotal: 60},
				{FilesDone: 3, FilesTotal: 3, BytesDone: 60, BytesTotal: 60},
			},
		},
		{
			name:   "a rejected upload is not counted",
			reject: "b.txt",
			want: []UploadProgress{
				{FilesDone: 1, FilesTotal: 3, BytesDone: 10, BytesTotal: 60},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
				if uploadedPath(msg) == tt.reject {
					writeReply(conn, msg, "error", map[string]any{"message": "Invalid binary format"})
				} else {
					writeReply(conn, msg, "ok", map[string]any{})
				}
				return true
			}))
			defer server.Close()
			client := joinedClient(t, server, 10*time.Second)
			// One at a time, so the files are acknowledged in order.
			client.uploadConcurrency = 1

			var got []UploadProgress
			err := client.SendFiles(t.Context(), files, needed, func(p UploadProgress) {
				got = append(got, p)
			})
			if (err != nil) != (tt.reject != "") {
				t.Fatalf("SendFiles() error = %v, want an error: %v", err, tt.reject != "")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("progress = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// With uploads running in parallel, progress calls overlap and can arrive out
// of order, but every acknowledgement is reported once and the largest
// snapshot carries the totals.
func TestClient_SendFiles_ReportsProgressConcurrently(t *testing.T) {
	files, paths := uploadFiles(200)
	var totalBytes int64
	for _, fi := range files {
		totalBytes += fi.Size
	}

	server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
		writeReply(conn, msg, "ok", map[string]any{})
		return true
	}))
	defer server.Close()
	client := joinedClient(t, server, 10*time.Second)

	var (
		mu    sync.Mutex
		seen  = map[int]bool{}
		final UploadProgress
	)
	err := client.SendFiles(t.Context(), files, paths, func(p UploadProgress) {
		mu.Lock()
		defer mu.Unlock()
		if seen[p.FilesDone] {
			t.Errorf("FilesDone %d reported twice", p.FilesDone)
		}
		seen[p.FilesDone] = true
		if p.FilesDone > final.FilesDone {
			final = p
		}
	})
	if err != nil {
		t.Fatalf("SendFiles() error = %v", err)
	}

	if len(seen) != len(paths) {
		t.Errorf("got %d progress calls, want %d", len(seen), len(paths))
	}
	want := UploadProgress{FilesDone: len(paths), FilesTotal: len(paths), BytesDone: totalBytes, BytesTotal: totalBytes}
	if final != want {
		t.Errorf("final progress = %+v, want %+v", final, want)
	}
}

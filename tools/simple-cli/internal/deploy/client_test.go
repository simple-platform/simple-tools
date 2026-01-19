package deploy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	if client.timeout != 30*time.Second {
		t.Errorf("NewClient() default timeout = %v, want %v", client.timeout, 30*time.Second)
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
	if !containsString(err.Error(), "not connected") {
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
	if !containsString(err.Error(), "not joined") {
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
	if !containsString(err.Error(), "not joined") {
		t.Errorf("SendFiles() error = %v, want containing 'not joined'", err)
	}
}

func TestClient_SendFiles_EmptyList(t *testing.T) {
	client := &Client{
		timeout: 5 * time.Second,
	}

	err := client.SendFiles(nil, []string{})
	if err != nil && !containsString(err.Error(), "not joined") {
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
	if !containsString(err.Error(), "not joined") {
		t.Errorf("Deploy() error = %v, want containing 'not joined'", err)
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
		defer conn.Close()

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
					conn.WriteMessage(websocket.TextMessage, reply)
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
					conn.WriteMessage(websocket.TextMessage, reply)
				case "manifest":
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{"need_files": []string{"file1.txt"}}})
					conn.WriteMessage(websocket.TextMessage, reply)
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
					conn.WriteMessage(websocket.TextMessage, reply)
				}
			} else if msgType == websocket.BinaryMessage {
				// Binary file received
				receivedFiles["binary"] = true

				// Decode to get ref and send reply
				msg := decodeBinaryMessageFast(data)
				if msg != nil {
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					conn.WriteMessage(websocket.TextMessage, reply)
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
					conn.WriteMessage(websocket.TextMessage, reply)
				case "deploy":
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{"version": "1.0.0", "file_count": float64(5)}})
					conn.WriteMessage(websocket.TextMessage, reply)
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
					conn.WriteMessage(websocket.TextMessage, reply)
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

func TestPushBinaryFile(t *testing.T) {
	receivedPayload := make(chan []byte, 1)

	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			switch msgType {
			case websocket.TextMessage:
				msg := decodeJSONMessageFast(data)
				if msg != nil && msg.Event == "phx_join" {
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					conn.WriteMessage(websocket.TextMessage, reply)
				}
			case websocket.BinaryMessage:
				// Decode Phoenix binary message
				msg := decodeBinaryMessageFast(data)
				if msg != nil && msg.Event == "file" {
					if payload, ok := msg.Payload.([]byte); ok {
						receivedPayload <- payload
						// Send reply
						reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
							map[string]any{"status": "ok", "response": map[string]any{}})
						conn.WriteMessage(websocket.TextMessage, reply)
					}
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

	metadata := map[string]string{
		"path": "test.txt",
		"hash": "abc123",
	}
	content := []byte("file content")

	ref, err := channel.PushBinaryFile(metadata, content)
	if err != nil {
		t.Fatalf("PushBinaryFile error: %v", err)
	}
	if ref == 0 {
		t.Error("PushBinaryFile returned ref = 0")
	}

	// Verify payload format
	select {
	case payload := <-receivedPayload:
		// First 4 bytes should be metadata length
		if len(payload) < 4 {
			t.Fatalf("payload too short: %d", len(payload))
		}
		metaLen := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
		if metaLen <= 0 || metaLen > len(payload)-4 {
			t.Fatalf("invalid metadata length: %d", metaLen)
		}

		// Parse metadata JSON
		var meta map[string]string
		if err := json.Unmarshal(payload[4:4+metaLen], &meta); err != nil {
			t.Fatalf("failed to parse metadata: %v", err)
		}

		if meta["path"] != "test.txt" {
			t.Errorf("metadata path = %q, want %q", meta["path"], "test.txt")
		}
		if meta["hash"] != "abc123" {
			t.Errorf("metadata hash = %q, want %q", meta["hash"], "abc123")
		}

		// Verify content
		fileContent := payload[4+metaLen:]
		if string(fileContent) != "file content" {
			t.Errorf("file content = %q, want %q", string(fileContent), "file content")
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for payload")
	}
}

// Helper to parse endpoint URL
func parseEndpointURL(rawURL string) (*url.URL, error) {
	return url.Parse(rawURL)
}

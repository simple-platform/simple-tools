package deploy

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestEncodeJSONMessage(t *testing.T) {
	tests := []struct {
		name     string
		joinRef  uint64
		ref      uint64
		topic    string
		event    string
		payload  any
		expected string
	}{
		{
			name:     "simple message",
			joinRef:  0,
			ref:      1,
			topic:    "room:lobby",
			event:    "new_msg",
			payload:  map[string]any{"body": "hello"},
			expected: `[null,"1","room:lobby","new_msg",{"body":"hello"}]`,
		},
		{
			name:     "with join ref",
			joinRef:  5,
			ref:      10,
			topic:    "deploy:app",
			event:    "manifest",
			payload:  map[string]any{"version": "1.0.0"},
			expected: `["5","10","deploy:app","manifest",{"version":"1.0.0"}]`,
		},
		{
			name:     "heartbeat with nil payload",
			joinRef:  0,
			ref:      42,
			topic:    "phoenix",
			event:    "heartbeat",
			payload:  nil,
			expected: `[null,"42","phoenix","heartbeat",{}]`,
		},
		{
			name:     "empty payload map",
			joinRef:  1,
			ref:      2,
			topic:    "test",
			event:    "test",
			payload:  map[string]any{},
			expected: `["1","2","test","test",{}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeJSONMessageFast(tt.joinRef, tt.ref, tt.topic, tt.event, tt.payload)
			if string(result) != tt.expected {
				t.Errorf("encodeJSONMessageFast() = %s, want %s", string(result), tt.expected)
			}
		})
	}
}

func TestDecodeJSONMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantJoinRef uint64
		wantRef     uint64
		wantTopic   string
		wantEvent   string
		wantErr     bool
	}{
		{
			name:        "simple reply",
			input:       `["1","2","room:lobby","phx_reply",{"status":"ok"}]`,
			wantJoinRef: 1,
			wantRef:     2,
			wantTopic:   "room:lobby",
			wantEvent:   "phx_reply",
			wantErr:     false,
		},
		{
			name:        "null join ref",
			input:       `[null,"5","phoenix","heartbeat",{}]`,
			wantJoinRef: 0,
			wantRef:     5,
			wantTopic:   "phoenix",
			wantEvent:   "heartbeat",
			wantErr:     false,
		},
		{
			name:    "invalid format - too few elements",
			input:   `["1","2","topic"]`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "empty array",
			input:   `[]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := decodeJSONMessage([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Errorf("decodeJSONMessage() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("decodeJSONMessage() unexpected error = %v", err)
				return
			}

			if msg.JoinRef != tt.wantJoinRef {
				t.Errorf("JoinRef = %d, want %d", msg.JoinRef, tt.wantJoinRef)
			}
			if msg.Ref != tt.wantRef {
				t.Errorf("Ref = %d, want %d", msg.Ref, tt.wantRef)
			}
			if msg.Topic != tt.wantTopic {
				t.Errorf("Topic = %q, want %q", msg.Topic, tt.wantTopic)
			}
			if msg.Event != tt.wantEvent {
				t.Errorf("Event = %q, want %q", msg.Event, tt.wantEvent)
			}
		})
	}
}

func TestEncodeBinaryMessage(t *testing.T) {
	tests := []struct {
		name    string
		joinRef uint64
		ref     uint64
		topic   string
		event   string
		payload []byte
	}{
		{
			name:    "file upload",
			joinRef: 1,
			ref:     5,
			topic:   "deploy:app",
			event:   "file",
			payload: []byte(`{"path":"test.txt"}`),
		},
		{
			name:    "no join ref",
			joinRef: 0,
			ref:     10,
			topic:   "room",
			event:   "msg",
			payload: []byte("hello"),
		},
		{
			name:    "large payload",
			joinRef: 100,
			ref:     200,
			topic:   "deploy:com.acme.app",
			event:   "file",
			payload: make([]byte, 10000), // 10KB payload
		},
		{
			name:    "empty payload",
			joinRef: 1,
			ref:     1,
			topic:   "test",
			event:   "ping",
			payload: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeBinaryMessageFast(tt.joinRef, tt.ref, tt.topic, tt.event, tt.payload)

			// Verify header
			if result[0] != phxPush {
				t.Errorf("push type = %d, want %d", result[0], phxPush)
			}

			// Decode and verify
			decoded, err := decodeBinaryMessage(result)
			if err != nil {
				t.Fatalf("failed to decode: %v", err)
			}

			if decoded.JoinRef != tt.joinRef {
				t.Errorf("JoinRef = %d, want %d", decoded.JoinRef, tt.joinRef)
			}
			if decoded.Ref != tt.ref {
				t.Errorf("Ref = %d, want %d", decoded.Ref, tt.ref)
			}
			if decoded.Topic != tt.topic {
				t.Errorf("Topic = %q, want %q", decoded.Topic, tt.topic)
			}
			if decoded.Event != tt.event {
				t.Errorf("Event = %q, want %q", decoded.Event, tt.event)
			}
		})
	}
}

func TestDecodeBinaryMessage(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "too short",
			data:    []byte{0, 1, 1},
			wantErr: true,
		},
		{
			name:    "truncated",
			data:    []byte{0, 5, 5, 5, 5, 'a'},
			wantErr: true,
		},
		{
			name:    "minimum valid",
			data:    []byte{0, 0, 0, 0, 0},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeBinaryMessage(tt.data)
			if tt.wantErr && err == nil {
				t.Errorf("decodeBinaryMessage() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("decodeBinaryMessage() unexpected error = %v", err)
			}
		})
	}
}

func TestDecodeBinaryReply(t *testing.T) {
	// Build a reply message
	joinRefStr := "1"
	refStr := "5"
	topic := "deploy:app"
	event := "phx_reply"

	// Reply payload: [status_size(1)] [status] [json]
	status := "ok"
	jsonPayload := []byte(`{"response":"done"}`)
	replyPayload := make([]byte, 1+len(status)+len(jsonPayload))
	replyPayload[0] = byte(len(status))
	copy(replyPayload[1:], status)
	copy(replyPayload[1+len(status):], jsonPayload)

	// Build full message
	totalSize := 5 + len(joinRefStr) + len(refStr) + len(topic) + len(event) + len(replyPayload)
	data := make([]byte, totalSize)
	data[0] = phxReply
	data[1] = byte(len(joinRefStr))
	data[2] = byte(len(refStr))
	data[3] = byte(len(topic))
	data[4] = byte(len(event))

	offset := 5
	copy(data[offset:], joinRefStr)
	offset += len(joinRefStr)
	copy(data[offset:], refStr)
	offset += len(refStr)
	copy(data[offset:], topic)
	offset += len(topic)
	copy(data[offset:], event)
	offset += len(event)
	copy(data[offset:], replyPayload)

	msg, err := decodeBinaryMessage(data)
	if err != nil {
		t.Fatalf("decodeBinaryMessage() error = %v", err)
	}

	if msg.Status != "ok" {
		t.Errorf("Status = %q, want %q", msg.Status, "ok")
	}
	if msg.JoinRef != 1 {
		t.Errorf("JoinRef = %d, want 1", msg.JoinRef)
	}
	if msg.Ref != 5 {
		t.Errorf("Ref = %d, want 5", msg.Ref)
	}
}

func TestBinaryFilePayloadFormat(t *testing.T) {
	metadata := map[string]string{
		"path": "test.scl",
		"hash": "abc123",
	}
	content := []byte("file content here")

	metaJSON, _ := json.Marshal(metadata)

	payload := make([]byte, 4+len(metaJSON)+len(content))
	binary.BigEndian.PutUint32(payload[0:4], uint32(len(metaJSON)))
	copy(payload[4:4+len(metaJSON)], metaJSON)
	copy(payload[4+len(metaJSON):], content)

	metaLen := binary.BigEndian.Uint32(payload[0:4])
	if metaLen != uint32(len(metaJSON)) {
		t.Errorf("metadata length = %d, want %d", metaLen, len(metaJSON))
	}

	parsedMeta := payload[4 : 4+metaLen]
	if string(parsedMeta) != string(metaJSON) {
		t.Errorf("metadata = %s, want %s", parsedMeta, metaJSON)
	}

	parsedContent := payload[4+metaLen:]
	if string(parsedContent) != string(content) {
		t.Errorf("content = %s, want %s", parsedContent, content)
	}
}

func TestNullableRef(t *testing.T) {
	tests := []struct {
		input uint64
		want  any
	}{
		{0, nil},
		{1, "1"},
		{42, "42"},
		{12345, "12345"},
	}

	for _, tt := range tests {
		result := nullableRef(tt.input)
		switch v := result.(type) {
		case nil:
			if tt.want != nil {
				t.Errorf("nullableRef(%d) = nil, want %v", tt.input, tt.want)
			}
		case string:
			if v != tt.want {
				t.Errorf("nullableRef(%d) = %q, want %q", tt.input, v, tt.want)
			}
		}
	}
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{`""`, 0},
		{`null`, 0},
		{`"1"`, 1},
		{`"42"`, 42},
		{`"12345"`, 12345},
	}

	for _, tt := range tests {
		result := parseRef([]byte(tt.input))
		if result != tt.want {
			t.Errorf("parseRef(%s) = %d, want %d", tt.input, result, tt.want)
		}
	}
}

func TestBufferPool(t *testing.T) {
	// Get a buffer
	buf1 := getBuffer()
	if buf1 == nil {
		t.Fatal("getBuffer() returned nil")
	}

	// Write some data
	buf1.WriteString("test data")
	if buf1.Len() != 9 {
		t.Errorf("buffer length = %d, want 9", buf1.Len())
	}

	// Return to pool
	putBuffer(buf1)

	// Get another buffer - should be reset
	buf2 := getBuffer()
	if buf2.Len() != 0 {
		t.Errorf("buffer should be empty after pool reset, got len %d", buf2.Len())
	}
	putBuffer(buf2)
}

func TestBufferPoolLargeBuffer(t *testing.T) {
	// Create a buffer larger than the pool limit
	buf := getBuffer()
	largeData := make([]byte, 100000) // 100KB
	buf.Write(largeData)

	// This should not be returned to pool (> 65536)
	putBuffer(buf)

	// Get another buffer - should be a new one
	buf2 := getBuffer()
	if buf2.Cap() > 65536 {
		t.Errorf("pool should not return very large buffers")
	}
	putBuffer(buf2)
}

func TestNewPhoenixSocket(t *testing.T) {
	u, _ := url.Parse("wss://example.com/socket")
	socket := NewPhoenixSocket(u)

	if socket == nil {
		t.Fatal("NewPhoenixSocket returned nil")
	}
	if socket.endpoint != u {
		t.Error("endpoint not set correctly")
	}
	if socket.done == nil {
		t.Error("done channel not initialized")
	}
	if socket.sendCh == nil {
		t.Error("sendCh not initialized")
	}
	if cap(socket.sendCh) != messageQueueLength {
		t.Errorf("sendCh capacity = %d, want %d", cap(socket.sendCh), messageQueueLength)
	}
}

func TestPhoenixSocketChannel(t *testing.T) {
	u, _ := url.Parse("wss://example.com/socket")
	socket := NewPhoenixSocket(u)

	// Get a channel
	ch1 := socket.Channel("test:topic")
	if ch1 == nil {
		t.Fatal("Channel returned nil")
	}
	if ch1.topic != "test:topic" {
		t.Errorf("topic = %q, want %q", ch1.topic, "test:topic")
	}
	if ch1.socket != socket {
		t.Error("channel socket not set correctly")
	}

	// Get the same channel again - should return same instance
	ch2 := socket.Channel("test:topic")
	if ch1 != ch2 {
		t.Error("Channel should return same instance for same topic")
	}

	// Different topic - should return different instance
	ch3 := socket.Channel("other:topic")
	if ch1 == ch3 {
		t.Error("Channel should return different instance for different topic")
	}
}

func TestPhoenixSocketNextRef(t *testing.T) {
	u, _ := url.Parse("wss://example.com/socket")
	socket := NewPhoenixSocket(u)

	ref1 := socket.nextRef()
	ref2 := socket.nextRef()
	ref3 := socket.nextRef()

	if ref1 != 1 {
		t.Errorf("first ref = %d, want 1", ref1)
	}
	if ref2 != 2 {
		t.Errorf("second ref = %d, want 2", ref2)
	}
	if ref3 != 3 {
		t.Errorf("third ref = %d, want 3", ref3)
	}
}

func TestPhoenixSocketNextRefConcurrent(t *testing.T) {
	u, _ := url.Parse("wss://example.com/socket")
	socket := NewPhoenixSocket(u)

	const numGoroutines = 100
	refs := make(chan uint64, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refs <- socket.nextRef()
		}()
	}

	wg.Wait()
	close(refs)

	// Collect all refs
	seen := make(map[uint64]bool)
	for ref := range refs {
		if seen[ref] {
			t.Errorf("duplicate ref: %d", ref)
		}
		seen[ref] = true
	}

	if len(seen) != numGoroutines {
		t.Errorf("got %d unique refs, want %d", len(seen), numGoroutines)
	}
}

func TestPhoenixSocketDisconnectIdempotent(t *testing.T) {
	u, _ := url.Parse("wss://example.com/socket")
	socket := NewPhoenixSocket(u)

	// Disconnect multiple times should not panic
	socket.Disconnect()
	socket.Disconnect()
	socket.Disconnect()
}

// Mock WebSocket server for integration tests
func startMockPhoenixServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
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
			t.Logf("upgrade error: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		handler(conn)
	}))

	return server
}

func TestPhoenixSocketConnectAndJoin(t *testing.T) {
	// Start mock server
	server := startMockPhoenixServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				msg := decodeJSONMessageFast(data)
				if msg != nil && msg.Event == "phx_join" {
					// Send join reply
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	// Parse server URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := url.Parse(wsURL)

	// Connect
	socket := NewPhoenixSocket(u)
	err := socket.Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer socket.Disconnect()

	// Join channel
	ch := socket.Channel("test:room")
	err = ch.Join(5 * time.Second)
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
}

func TestPhoenixSocketConnectAuthFailure(t *testing.T) {
	// Start mock server that returns 401
	server := startMockPhoenixServer(t, func(conn *websocket.Conn) {
		// This handler won't be called because handshake fails
	})
	// Force the mock server wrapper to handle the handshake failure check?
	// The standard httptest.NewServer handles the connection.
	// We need a specific handler that rejects the upgrade request with 401.
	server.Close() // Close the specific websocket server

	// create a custom server for this test
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Unauthorized"))
	}))
	defer authServer.Close()

	// Parse server URL
	wsURL := "ws" + strings.TrimPrefix(authServer.URL, "http") + "/socket"
	u, err := url.Parse(wsURL)
	if err != nil {
		t.Fatalf("failed to parse WebSocket URL: %v", err)
	}

	// Connect
	socket := NewPhoenixSocket(u)
	err = socket.Connect()
	if err == nil {
		t.Fatal("Connect() should have failed")
	}

	var authErr *AuthFailedError
	if !errors.As(err, &authErr) {
		t.Errorf("error should be AuthFailedError, got type %T: %v", err, err)
	} else if authErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", authErr.StatusCode)
	}
}

func TestPhoenixSocketPush(t *testing.T) {
	receivedMsg := make(chan *phoenixMessage, 1)

	server := startMockPhoenixServer(t, func(conn *websocket.Conn) {
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
				case "test_event":
					receivedMsg <- msg
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{"received": true}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := url.Parse(wsURL)

	socket := NewPhoenixSocket(u)
	if err := socket.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer socket.Disconnect()

	ch := socket.Channel("test:room")
	if err := ch.Join(5 * time.Second); err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	// Push a message
	ref, err := ch.Push("test_event", map[string]any{"data": "hello"})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if ref == 0 {
		t.Error("Push() returned ref = 0")
	}

	// Verify server received the message
	select {
	case msg := <-receivedMsg:
		if msg.Event != "test_event" {
			t.Errorf("received event = %q, want %q", msg.Event, "test_event")
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestPhoenixChannelLeave(t *testing.T) {
	server := startMockPhoenixServer(t, func(conn *websocket.Conn) {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				msg := decodeJSONMessageFast(data)
				if msg != nil && (msg.Event == "phx_join" || msg.Event == "phx_leave") {
					reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
						map[string]any{"status": "ok", "response": map[string]any{}})
					_ = conn.WriteMessage(websocket.TextMessage, reply)
				}
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := url.Parse(wsURL)

	socket := NewPhoenixSocket(u)
	if err := socket.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer socket.Disconnect()

	ch := socket.Channel("test:room")
	if err := ch.Join(5 * time.Second); err != nil {
		t.Fatalf("Join() error = %v", err)
	}

	// Leave channel
	err := ch.Leave()
	if err != nil {
		t.Errorf("Leave() error = %v", err)
	}
}

func TestPhoenixChannelJoinTimeout(t *testing.T) {
	server := startMockPhoenixServer(t, func(conn *websocket.Conn) {
		// Don't respond to anything - cause timeout
		time.Sleep(10 * time.Second)
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	u, _ := url.Parse(wsURL)

	socket := NewPhoenixSocket(u)
	if err := socket.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer socket.Disconnect()

	ch := socket.Channel("test:room")
	err := ch.Join(100 * time.Millisecond)
	if err == nil {
		t.Error("Join() should have timed out")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error should contain 'timeout', got: %v", err)
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	tests := []struct {
		joinRef uint64
		ref     uint64
		topic   string
		event   string
		payload any
	}{
		{0, 1, "room:lobby", "msg", map[string]any{"text": "hello"}},
		{5, 10, "deploy:app", "manifest", map[string]any{"version": "1.0"}},
		{0, 0, "phoenix", "heartbeat", nil},
	}

	for _, tt := range tests {
		t.Run(tt.topic+":"+tt.event, func(t *testing.T) {
			encoded := encodeJSONMessageFast(tt.joinRef, tt.ref, tt.topic, tt.event, tt.payload)
			decoded, err := decodeJSONMessage(encoded)
			if err != nil {
				t.Fatalf("decode error: %v", err)
			}

			if decoded.JoinRef != tt.joinRef {
				t.Errorf("JoinRef = %d, want %d", decoded.JoinRef, tt.joinRef)
			}
			if decoded.Ref != tt.ref {
				t.Errorf("Ref = %d, want %d", decoded.Ref, tt.ref)
			}
			if decoded.Topic != tt.topic {
				t.Errorf("Topic = %q, want %q", decoded.Topic, tt.topic)
			}
			if decoded.Event != tt.event {
				t.Errorf("Event = %q, want %q", decoded.Event, tt.event)
			}
		})
	}
}

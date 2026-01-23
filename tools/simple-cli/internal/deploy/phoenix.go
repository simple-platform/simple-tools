package deploy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Performance tuning constants (from phx library patterns)
const (
	// messageQueueLength is the number of messages to queue before blocking
	messageQueueLength = 1000

	// defaultHeartbeatInterval is the time between heartbeats
	defaultHeartbeatInterval = 30 * time.Second

	// defaultConnectTimeout is the handshake timeout
	defaultConnectTimeout = 10 * time.Second
)

// Phoenix V2 binary protocol message types
const (
	phxPush      = 0 // Client push
	phxReply     = 1 // Server reply
	phxBroadcast = 2 // Server broadcast
)

// Buffer pool for reducing allocations
var bufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

func getBuffer() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putBuffer(buf *bytes.Buffer) {
	if buf.Cap() <= 65536 { // Don't pool very large buffers
		bufferPool.Put(buf)
	}
}

// PhoenixSocket represents a WebSocket connection to a Phoenix server.
type PhoenixSocket struct {
	conn       *websocket.Conn
	endpoint   *url.URL
	refCounter uint64
	channels   sync.Map // map[string]*PhoenixChannel - concurrent safe
	done       chan struct{}
	sendCh     chan outgoingMsg
	connMu     sync.RWMutex
}

type outgoingMsg struct {
	msgType int
	data    []byte
}

// PhoenixChannel represents a joined channel on the socket.
type PhoenixChannel struct {
	socket   *PhoenixSocket
	topic    string
	joinRef  uint64
	bindings sync.Map // map[uint64]func(any) - concurrent safe
}

// phoenixMessage is the decoded Phoenix channel message.
type phoenixMessage struct {
	JoinRef uint64
	Ref     uint64
	Topic   string
	Event   string
	Payload any
	Status  string
}

// NewPhoenixSocket creates a new Phoenix socket connection.
func NewPhoenixSocket(endpointURL *url.URL) *PhoenixSocket {
	return &PhoenixSocket{
		endpoint: endpointURL,
		done:     make(chan struct{}),
		sendCh:   make(chan outgoingMsg, messageQueueLength),
	}
}

// Connect establishes the WebSocket connection.
func (s *PhoenixSocket) Connect() error {
	wsURL := *s.endpoint
	wsURL.Path = path.Join(wsURL.Path, "websocket")
	q := wsURL.Query()
	q.Set("vsn", "2.0.0")
	wsURL.RawQuery = q.Encode()

	switch wsURL.Scheme {
	case "https":
		wsURL.Scheme = "wss"
	case "http":
		wsURL.Scheme = "ws"
	}

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = defaultConnectTimeout
	dialer.ReadBufferSize = 16384
	dialer.WriteBufferSize = 16384

	conn, resp, err := dialer.Dial(wsURL.String(), http.Header{})
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("websocket auth failed: %d", resp.StatusCode)
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()

	go s.readLoop()
	go s.writeLoop()
	go s.heartbeatLoop()

	return nil
}

// Disconnect closes the WebSocket connection.
func (s *PhoenixSocket) Disconnect() {
	select {
	case <-s.done:
		return // Already closed
	default:
		close(s.done)
	}

	s.connMu.Lock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	s.connMu.Unlock()
}

// IsConnected returns true if the socket is connected.
func (s *PhoenixSocket) IsConnected() bool {
	select {
	case <-s.done:
		return false
	default:
	}
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.conn != nil
}

// Channel returns a channel for the given topic.
func (s *PhoenixSocket) Channel(topic string) *PhoenixChannel {
	if ch, ok := s.channels.Load(topic); ok {
		return ch.(*PhoenixChannel)
	}

	ch := &PhoenixChannel{
		socket: s,
		topic:  topic,
	}
	actual, _ := s.channels.LoadOrStore(topic, ch)
	return actual.(*PhoenixChannel)
}

// nextRef returns the next unique reference number.
func (s *PhoenixSocket) nextRef() uint64 {
	return atomic.AddUint64(&s.refCounter, 1)
}

// send queues a message for sending (non-blocking with large buffer).
func (s *PhoenixSocket) send(msgType int, data []byte) error {
	select {
	case s.sendCh <- outgoingMsg{msgType: msgType, data: data}:
		return nil
	case <-s.done:
		return fmt.Errorf("socket closed")
	default:
		// Queue full, block briefly then try again
		select {
		case s.sendCh <- outgoingMsg{msgType: msgType, data: data}:
			return nil
		case <-s.done:
			return fmt.Errorf("socket closed")
		case <-time.After(100 * time.Millisecond):
			return fmt.Errorf("send queue full")
		}
	}
}

func (s *PhoenixSocket) writeLoop() {
	for {
		select {
		case <-s.done:
			return
		case msg := <-s.sendCh:
			s.connMu.RLock()
			conn := s.conn
			s.connMu.RUnlock()
			if conn != nil {
				_ = conn.WriteMessage(msg.msgType, msg.data)
			}
		}
	}
}

func (s *PhoenixSocket) readLoop() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		s.connMu.RLock()
		conn := s.conn
		s.connMu.RUnlock()

		if conn == nil {
			return
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg *phoenixMessage
		switch msgType {
		case websocket.TextMessage:
			msg = decodeJSONMessageFast(data)
		case websocket.BinaryMessage:
			msg = decodeBinaryMessageFast(data)
		}

		if msg == nil {
			continue
		}

		if ch, ok := s.channels.Load(msg.Topic); ok {
			ch.(*PhoenixChannel).handleMessage(msg)
		}
	}
}

func (s *PhoenixSocket) heartbeatLoop() {
	ticker := time.NewTicker(defaultHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			ref := s.nextRef()
			data := encodeJSONMessageFast(0, ref, "phoenix", "heartbeat", nil)
			_ = s.send(websocket.TextMessage, data)
		}
	}
}

// encodeJSONMessageFast encodes a Phoenix V2 JSON message with minimal allocations
func encodeJSONMessageFast(joinRef, ref uint64, topic, event string, payload any) []byte {
	buf := getBuffer()
	defer putBuffer(buf)

	buf.WriteByte('[')

	// JoinRef
	if joinRef == 0 {
		buf.WriteString("null")
	} else {
		buf.WriteByte('"')
		buf.WriteString(strconv.FormatUint(joinRef, 10))
		buf.WriteByte('"')
	}
	buf.WriteByte(',')

	// Ref
	buf.WriteByte('"')
	buf.WriteString(strconv.FormatUint(ref, 10))
	buf.WriteByte('"')
	buf.WriteByte(',')

	// Topic
	buf.WriteByte('"')
	buf.WriteString(topic)
	buf.WriteByte('"')
	buf.WriteByte(',')

	// Event
	buf.WriteByte('"')
	buf.WriteString(event)
	buf.WriteByte('"')
	buf.WriteByte(',')

	// Payload
	if payload == nil {
		buf.WriteString("{}")
	} else {
		payloadBytes, _ := json.Marshal(payload)
		buf.Write(payloadBytes)
	}

	buf.WriteByte(']')

	// Make a copy since we're returning the buffer to the pool
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result
}

// decodeJSONMessageFast decodes a Phoenix V2 JSON message efficiently
func decodeJSONMessageFast(data []byte) *phoenixMessage {
	var arr []json.RawMessage
	if json.Unmarshal(data, &arr) != nil || len(arr) != 5 {
		return nil
	}

	msg := &phoenixMessage{}
	msg.JoinRef = parseRefFast(arr[0])
	msg.Ref = parseRefFast(arr[1])
	json.Unmarshal(arr[2], &msg.Topic)
	json.Unmarshal(arr[3], &msg.Event)
	json.Unmarshal(arr[4], &msg.Payload)

	// For phx_reply, extract status and response from payload
	if msg.Event == "phx_reply" {
		if payloadMap, ok := msg.Payload.(map[string]any); ok {
			if status, ok := payloadMap["status"].(string); ok {
				msg.Status = status
			}
			if response, ok := payloadMap["response"]; ok {
				msg.Payload = response
			}
		}
	}

	return msg
}

// encodeBinaryMessageFast encodes a Phoenix V2 binary message efficiently
func encodeBinaryMessageFast(joinRef, ref uint64, topic, event string, payload []byte) []byte {
	var joinRefStr string
	if joinRef != 0 {
		joinRefStr = strconv.FormatUint(joinRef, 10)
	}
	refStr := strconv.FormatUint(ref, 10)

	joinRefSize := len(joinRefStr)
	refSize := len(refStr)
	topicSize := len(topic)
	eventSize := len(event)

	totalSize := 5 + joinRefSize + refSize + topicSize + eventSize + len(payload)
	buf := make([]byte, totalSize)

	buf[0] = phxPush
	buf[1] = byte(joinRefSize)
	buf[2] = byte(refSize)
	buf[3] = byte(topicSize)
	buf[4] = byte(eventSize)

	offset := 5
	copy(buf[offset:], joinRefStr)
	offset += joinRefSize
	copy(buf[offset:], refStr)
	offset += refSize
	copy(buf[offset:], topic)
	offset += topicSize
	copy(buf[offset:], event)
	offset += eventSize
	copy(buf[offset:], payload)

	return buf
}

// decodeBinaryMessageFast decodes a Phoenix V2 binary message efficiently
func decodeBinaryMessageFast(data []byte) *phoenixMessage {
	if len(data) < 5 {
		return nil
	}

	msgType := data[0]
	joinRefSize := int(data[1])
	refSize := int(data[2])
	topicSize := int(data[3])
	eventSize := int(data[4])

	minLen := 5 + joinRefSize + refSize + topicSize + eventSize
	if len(data) < minLen {
		return nil
	}

	offset := 5
	joinRefStr := string(data[offset : offset+joinRefSize])
	offset += joinRefSize
	refStr := string(data[offset : offset+refSize])
	offset += refSize
	topic := string(data[offset : offset+topicSize])
	offset += topicSize
	event := string(data[offset : offset+eventSize])
	offset += eventSize
	payload := data[offset:]

	msg := &phoenixMessage{
		Topic: topic,
		Event: event,
	}

	if joinRefStr != "" {
		msg.JoinRef, _ = strconv.ParseUint(joinRefStr, 10, 64)
	}
	if refStr != "" {
		msg.Ref, _ = strconv.ParseUint(refStr, 10, 64)
	}

	// For replies, payload is: status_size(1) + status + json_payload
	if msgType == phxReply && len(payload) > 0 {
		statusSize := int(payload[0])
		if len(payload) >= 1+statusSize {
			msg.Status = string(payload[1 : 1+statusSize])
			jsonPayload := payload[1+statusSize:]
			if len(jsonPayload) > 0 {
				var p any
				json.Unmarshal(jsonPayload, &p)
				msg.Payload = p
			}
		}
	} else {
		var p any
		if json.Unmarshal(payload, &p) == nil {
			msg.Payload = p
		} else {
			msg.Payload = payload
		}
	}

	return msg
}

func parseRefFast(raw json.RawMessage) uint64 {
	// Fast path for null
	if len(raw) == 4 && raw[0] == 'n' {
		return 0
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		ref, _ := strconv.ParseUint(s, 10, 64)
		return ref
	}
	return 0
}

// Join sends a join message and waits for response.
func (c *PhoenixChannel) Join(timeout time.Duration) error {
	ref := c.socket.nextRef()
	c.joinRef = ref

	data := encodeJSONMessageFast(ref, ref, c.topic, "phx_join", nil)

	done := make(chan error, 1)
	c.bindings.Store(ref, func(payload any) {
		resp, ok := payload.(map[string]any)
		if !ok {
			done <- nil
			return
		}
		if status, ok := resp["status"].(string); ok && status == "error" {
			done <- fmt.Errorf("join error: %v", resp["response"])
			return
		}
		done <- nil
	})

	if err := c.socket.send(websocket.TextMessage, data); err != nil {
		c.bindings.Delete(ref)
		return err
	}

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		c.bindings.Delete(ref)
		return fmt.Errorf("join timeout")
	}
}

// Push sends a JSON message and returns the ref for tracking replies.
func (c *PhoenixChannel) Push(event string, payload any) (uint64, error) {
	ref := c.socket.nextRef()
	data := encodeJSONMessageFast(c.joinRef, ref, c.topic, event, payload)

	if err := c.socket.send(websocket.TextMessage, data); err != nil {
		return 0, err
	}
	return ref, nil
}

// PushBinary sends a binary message with proper Phoenix V2 format.
func (c *PhoenixChannel) PushBinary(event string, payload []byte) (uint64, error) {
	ref := c.socket.nextRef()
	data := encodeBinaryMessageFast(c.joinRef, ref, c.topic, event, payload)

	if err := c.socket.send(websocket.BinaryMessage, data); err != nil {
		return 0, err
	}
	return ref, nil
}

// PushBinaryFile sends a file with metadata in our custom format.
// Returns an error if the combined payload size would exceed safe limits.
func (c *PhoenixChannel) PushBinaryFile(metadata map[string]string, content []byte) (uint64, error) {
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return 0, err
	}

	// Validate metadata length fits in uint32 (4 bytes header)
	if len(metaJSON) > 0xFFFFFFFF {
		return 0, fmt.Errorf("metadata too large: %d bytes exceeds maximum", len(metaJSON))
	}

	// Calculate total size using int64 to prevent overflow
	// Build payload: [metadata_len (4 bytes)] [metadata_json] [file_content]
	const maxPayloadSize = 100 * 1024 * 1024 // 100MB limit
	totalSize := int64(4) + int64(len(metaJSON)) + int64(len(content))
	if totalSize > maxPayloadSize {
		return 0, fmt.Errorf("payload too large: %d bytes exceeds maximum %d", totalSize, maxPayloadSize)
	}

	payload := make([]byte, int(totalSize))
	binary.BigEndian.PutUint32(payload[0:4], uint32(len(metaJSON)))
	copy(payload[4:4+len(metaJSON)], metaJSON)
	copy(payload[4+len(metaJSON):], content)

	return c.PushBinary("file", payload)
}

// onRef registers a one-time callback for a specific ref.
func (c *PhoenixChannel) onRef(ref uint64, callback func(any)) {
	c.bindings.Store(ref, callback)
}

func (c *PhoenixChannel) handleMessage(msg *phoenixMessage) {
	if callback, ok := c.bindings.Load(msg.Ref); ok {
		fn := callback.(func(any))

		if msg.Event == "phx_reply" {
			response := map[string]any{
				"status":   msg.Status,
				"response": msg.Payload,
			}
			fn(response)
		} else {
			fn(msg.Payload)
		}

		c.bindings.Delete(msg.Ref)
	}
}

// Leave sends a leave message.
func (c *PhoenixChannel) Leave() error {
	ref := c.socket.nextRef()
	data := encodeJSONMessageFast(c.joinRef, ref, c.topic, "phx_leave", nil)
	return c.socket.send(websocket.TextMessage, data)
}

// Legacy compatibility functions
func encodeJSONMessage(joinRef, ref uint64, topic, event string, payload any) []byte {
	return encodeJSONMessageFast(joinRef, ref, topic, event, payload)
}

func decodeJSONMessage(data []byte) (*phoenixMessage, error) {
	msg := decodeJSONMessageFast(data)
	if msg == nil {
		return nil, fmt.Errorf("failed to decode message")
	}
	return msg, nil
}

func encodeBinaryMessage(joinRef, ref uint64, topic, event string, payload []byte) []byte {
	return encodeBinaryMessageFast(joinRef, ref, topic, event, payload)
}

func decodeBinaryMessage(data []byte) (*phoenixMessage, error) {
	msg := decodeBinaryMessageFast(data)
	if msg == nil {
		return nil, fmt.Errorf("failed to decode message")
	}
	return msg, nil
}

func nullableRef(ref uint64) any {
	if ref == 0 {
		return nil
	}
	return strconv.FormatUint(ref, 10)
}

func parseRef(raw json.RawMessage) uint64 {
	return parseRefFast(raw)
}

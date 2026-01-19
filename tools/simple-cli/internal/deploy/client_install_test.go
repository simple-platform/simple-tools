package deploy

import (
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClient_Install(t *testing.T) {
	appID := "test.app"
	expectedVersion := "1.2.3"

	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			msg := decodeJSONMessageFast(data)
			if msg == nil {
				continue
			}

			switch msg.Event {
			case "phx_join":
				reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
					map[string]any{"status": "ok", "response": map[string]any{}})
				conn.WriteMessage(websocket.TextMessage, reply)

			case "install":
				reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
					map[string]any{
						"status": "ok",
						"response": map[string]any{
							"version": expectedVersion,
						},
					})
				conn.WriteMessage(websocket.TextMessage, reply)
			}
		}
	})
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		JWT:      "test-token",
		Timeout:  time.Second,
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	if err := client.JoinChannel(appID); err != nil {
		t.Fatalf("JoinChannel() error = %v", err)
	}

	result, err := client.Install()
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if result.Version != expectedVersion {
		t.Errorf("Install() version = %v, want %v", result.Version, expectedVersion)
	}
	if !result.Success {
		t.Errorf("Install() success = false, want true")
	}
}

func TestClient_Install_Error(t *testing.T) {
	appID := "test.app"
	errorMsg := "install blocked"

	server := startClientMockServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			msg := decodeJSONMessageFast(data)
			if msg == nil {
				continue
			}

			switch msg.Event {
			case "phx_join":
				reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
					map[string]any{"status": "ok", "response": map[string]any{}})
				conn.WriteMessage(websocket.TextMessage, reply)

			case "install":
				reply := encodeJSONMessageFast(msg.JoinRef, msg.Ref, msg.Topic, "phx_reply",
					map[string]any{
						"status": "error",
						"response": map[string]any{
							"message": errorMsg,
						},
					})
				conn.WriteMessage(websocket.TextMessage, reply)
			}
		}
	})
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		JWT:      "test-token",
		Timeout:  time.Second,
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	if err := client.JoinChannel(appID); err != nil {
		t.Fatalf("JoinChannel() error = %v", err)
	}

	_, err := client.Install()
	if err == nil {
		t.Fatal("Install() expected error, got nil")
	}

	if err.Error() != errorMsg {
		t.Errorf("Install() error = %v, want %v", err.Error(), errorMsg)
	}
}

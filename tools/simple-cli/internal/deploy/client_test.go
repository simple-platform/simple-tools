package deploy

import (
	"testing"
	"time"
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
	// This shouldn't error even without connection for empty list
	client := &Client{
		timeout: 5 * time.Second,
	}

	// Need channel to be set for this test
	err := client.SendFiles(nil, []string{})
	// Empty list should check channel first
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

// Note: Integration tests with real Phoenix server would be added separately.
// These unit tests cover the error paths and basic functionality.
// The actual Phoenix channel communication is tested via integration tests.

package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client handles deployment via Phoenix Channel.
type Client struct {
	endpoint string
	jwt      string
	appID    string
	socket   *PhoenixSocket
	channel  *PhoenixChannel
	timeout  time.Duration
}

// ClientConfig holds configuration for creating a Client.
type ClientConfig struct {
	Endpoint string
	JWT      string
	Timeout  time.Duration
}

// DefaultTimeout is the fallback wait for a channel reply when a caller does
// not set one. Installs run synchronously server-side and scale with app size
// (record-heavy apps routinely exceed a minute), so this must stay generous:
// a short default silently reports a false failure while the install is still
// running to completion on the server.
const DefaultTimeout = 15 * time.Minute

// NewClient creates a deployment client.
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		endpoint: cfg.Endpoint,
		jwt:      cfg.JWT,
		timeout:  timeout,
	}
}

// Connect establishes WebSocket connection to the Phoenix server. ctx bounds
// the dial and handshake only.
func (c *Client) Connect(ctx context.Context) error {
	endpoint := c.endpoint
	if !strings.Contains(endpoint, "://") {
		endpoint = fmt.Sprintf("wss://%s", endpoint)
	}

	endpointURL, err := url.Parse(fmt.Sprintf("%s/socket?auth_token=%s", endpoint, c.jwt))
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}

	socket := NewPhoenixSocket(endpointURL)
	if err := socket.Connect(ctx); err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}

	c.socket = socket
	return nil
}

// JoinChannel joins the deploy channel for the app.
func (c *Client) JoinChannel(ctx context.Context, appID string) error {
	if c.socket == nil {
		return fmt.Errorf("not connected to socket")
	}

	c.appID = appID
	channel := c.socket.Channel(fmt.Sprintf("deploy:%s", appID))

	if err := channel.Join(ctx, c.timeout); err != nil {
		return fmt.Errorf("failed to join channel: %w", err)
	}

	c.channel = channel
	return nil
}

// SendManifest sends file manifest and returns paths of needed files.
func (c *Client) SendManifest(ctx context.Context, files map[string]FileInfo, version string) ([]string, error) {
	if c.channel == nil {
		return nil, fmt.Errorf("not joined to channel")
	}

	// Convert to format expected by server
	fileList := make([]map[string]any, 0, len(files))
	for path, info := range files {
		fileList = append(fileList, map[string]any{
			"path": path,
			"hash": info.Hash,
			"size": info.Size,
		})
	}

	reply, err := c.channel.Request(ctx, "manifest", map[string]any{
		"files":   fileList,
		"version": version,
	}, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if reply.Status == "error" {
		return nil, fmt.Errorf("manifest rejected: %v", reply.Response)
	}

	response, _ := reply.Response.(map[string]any)
	needFiles, ok := response["need_files"].([]any)
	if !ok {
		return []string{}, nil
	}

	result := make([]string, len(needFiles))
	for i, f := range needFiles {
		if s, ok := f.(string); ok {
			result[i] = s
		}
	}
	return result, nil
}

// SendFiles uploads multiple files in parallel.
func (c *Client) SendFiles(ctx context.Context, files map[string]FileInfo, neededPaths []string) error {
	if c.channel == nil {
		return fmt.Errorf("not joined to channel")
	}

	if len(neededPaths) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(neededPaths))

	for _, path := range neededPaths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if fi, ok := files[p]; ok {
				if err := c.sendFile(ctx, p, fi); err != nil {
					errChan <- err
				}
			}
		}(path)
	}

	wg.Wait()
	close(errChan)

	// Return first error if any
	for err := range errChan {
		return err
	}

	return nil
}

// sendFile sends a single file using Phoenix V2 binary protocol.
// Format: [metadata_len (4 bytes)] [metadata_json] [file_content]
func (c *Client) sendFile(ctx context.Context, path string, fi FileInfo) error {
	metadata := map[string]string{
		"path": path,
		"hash": fi.Hash,
	}

	reply, err := c.channel.RequestBinaryFile(ctx, metadata, fi.Content, c.timeout)
	if err != nil {
		return fmt.Errorf("upload %s: %w", path, err)
	}
	if reply.Status == "error" {
		return fmt.Errorf("file rejected for %s: %v", path, reply.Response)
	}
	return nil
}

// Deploy triggers the actual deployment.
func (c *Client) Deploy(ctx context.Context) (*DeployResult, error) {
	if c.channel == nil {
		return nil, fmt.Errorf("not joined to channel")
	}

	reply, err := c.channel.Request(ctx, "deploy", map[string]any{}, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}

	if reply.Status == "error" {
		errResp, _ := reply.Response.(map[string]any)
		errMsg := "unknown error"
		if msg, ok := errResp["message"].(string); ok {
			errMsg = msg
		}
		return nil, fmt.Errorf("deploy failed: %s", errMsg)
	}

	response, _ := reply.Response.(map[string]any)
	version, _ := response["version"].(string)
	fileCount := 0
	if fc, ok := response["file_count"].(float64); ok {
		fileCount = int(fc)
	}

	return &DeployResult{
		AppID:     c.appID,
		Version:   version,
		FileCount: fileCount,
	}, nil
}

// InstallResult represents the result of a successful installation.
type InstallResult struct {
	AppID   string `json:"app_id"`
	Version string `json:"version"`
	Success bool   `json:"success"`
}

// Install triggers the installation of the app version.
func (c *Client) Install(ctx context.Context) (*InstallResult, error) {
	if c.channel == nil {
		return nil, fmt.Errorf("not joined to channel")
	}

	// Send install event with empty payload
	reply, err := c.channel.Request(ctx, "install", map[string]any{}, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}

	// The server's own message is returned unchanged: callers match on it to
	// recognise an install that is already done.
	if reply.Status != "ok" {
		response, _ := reply.Response.(map[string]any)
		msg := "install failed"
		if m, ok := response["message"].(string); ok {
			msg = m
		}
		return nil, errors.New(msg)
	}

	response, _ := reply.Response.(map[string]any)
	version, _ := response["version"].(string)

	return &InstallResult{
		AppID:   c.appID,
		Version: version,
		Success: true,
	}, nil
}

// Close disconnects from the socket.
func (c *Client) Close() {
	if c.channel != nil {
		_ = c.channel.Leave()
	}
	if c.socket != nil {
		c.socket.Disconnect()
	}
}

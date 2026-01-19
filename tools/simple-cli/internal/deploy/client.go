package deploy

import (
	"encoding/base64"
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

// NewClient creates a deployment client.
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		endpoint: cfg.Endpoint,
		jwt:      cfg.JWT,
		timeout:  timeout,
	}
}

// Connect establishes WebSocket connection to the Phoenix server.
func (c *Client) Connect() error {
	endpoint := c.endpoint
	if !strings.Contains(endpoint, "://") {
		endpoint = fmt.Sprintf("wss://%s", endpoint)
	}

	endpointURL, err := url.Parse(fmt.Sprintf("%s/socket?auth_token=%s", endpoint, c.jwt))
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}

	socket := NewPhoenixSocket(endpointURL)
	if err := socket.Connect(); err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}

	c.socket = socket
	return nil
}

// JoinChannel joins the deploy channel for the app.
func (c *Client) JoinChannel(appID string) error {
	if c.socket == nil {
		return fmt.Errorf("not connected to socket")
	}

	c.appID = appID
	channel := c.socket.Channel(fmt.Sprintf("deploy:%s", appID))

	if err := channel.Join(c.timeout); err != nil {
		return fmt.Errorf("failed to join channel: %w", err)
	}

	c.channel = channel
	return nil
}

// SendManifest sends file manifest and returns paths of needed files.
func (c *Client) SendManifest(files map[string]FileInfo, version string) ([]string, error) {
	if c.channel == nil {
		return nil, fmt.Errorf("not joined to channel")
	}

	// Convert to format expected by server
	fileList := make([]map[string]interface{}, 0, len(files))
	for path, info := range files {
		fileList = append(fileList, map[string]interface{}{
			"path": path,
			"hash": info.Hash,
			"size": info.Size,
		})
	}

	ref, err := c.channel.Push("manifest", map[string]interface{}{
		"files":   fileList,
		"version": version,
	})
	if err != nil {
		return nil, fmt.Errorf("manifest push failed: %w", err)
	}

	done := make(chan struct {
		files []string
		err   error
	}, 1)

	c.channel.onRef(ref, func(payload any) {
		resp, ok := payload.(map[string]any)
		if !ok {
			done <- struct {
				files []string
				err   error
			}{nil, fmt.Errorf("invalid response format")}
			return
		}

		// Check for error status
		if status, ok := resp["status"].(string); ok && status == "error" {
			done <- struct {
				files []string
				err   error
			}{nil, fmt.Errorf("manifest rejected: %v", resp["response"])}
			return
		}

		// Extract response - for phx_reply, data is in "response" field
		response, _ := resp["response"].(map[string]any)
		needFiles, ok := response["need_files"].([]interface{})
		if !ok {
			done <- struct {
				files []string
				err   error
			}{[]string{}, nil}
			return
		}

		result := make([]string, len(needFiles))
		for i, f := range needFiles {
			if s, ok := f.(string); ok {
				result[i] = s
			}
		}
		done <- struct {
			files []string
			err   error
		}{result, nil}
	})

	select {
	case result := <-done:
		return result.files, result.err
	case <-time.After(c.timeout):
		return nil, fmt.Errorf("manifest response timeout")
	}
}

// SendFiles uploads multiple files in parallel.
func (c *Client) SendFiles(files map[string]FileInfo, neededPaths []string) error {
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
				if err := c.sendFile(p, fi); err != nil {
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
func (c *Client) sendFile(path string, fi FileInfo) error {
	metadata := map[string]string{
		"path": path,
		"hash": fi.Hash,
	}

	ref, err := c.channel.PushBinaryFile(metadata, fi.Content)
	if err != nil {
		return fmt.Errorf("file push failed for %s: %w", path, err)
	}

	done := make(chan error, 1)
	c.channel.onRef(ref, func(payload any) {
		resp, ok := payload.(map[string]any)
		if !ok {
			done <- nil // Binary file pushes may not reply, consider success
			return
		}
		if status, ok := resp["status"].(string); ok && status == "error" {
			done <- fmt.Errorf("file rejected for %s: %v", path, resp["response"])
			return
		}
		done <- nil
	})

	select {
	case err := <-done:
		return err
	case <-time.After(c.timeout):
		return fmt.Errorf("timeout waiting for file response")
	}
}

// sendFileBase64 sends a file using JSON with base64 encoding (fallback).
func (c *Client) sendFileBase64(path string, fi FileInfo) error {
	done := make(chan error, 1)

	ref, err := c.channel.Push("file", map[string]interface{}{
		"path": path,
		"hash": fi.Hash,
		"data": base64.StdEncoding.EncodeToString(fi.Content),
	})
	if err != nil {
		return fmt.Errorf("file push failed for %s: %w", path, err)
	}

	c.channel.onRef(ref, func(payload any) {
		resp, ok := payload.(map[string]any)
		if !ok {
			done <- nil // File messages may not reply
			return
		}
		if status, ok := resp["status"].(string); ok && status == "error" {
			done <- fmt.Errorf("file rejected for %s: %v", path, resp["response"])
			return
		}
		done <- nil
	})

	select {
	case err := <-done:
		return err
	case <-time.After(c.timeout):
		return fmt.Errorf("file upload timeout for %s", path)
	}
}

// Deploy triggers the actual deployment.
func (c *Client) Deploy() (*DeployResult, error) {
	if c.channel == nil {
		return nil, fmt.Errorf("not joined to channel")
	}

	ref, err := c.channel.Push("deploy", map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("deploy push failed: %w", err)
	}

	done := make(chan struct {
		result *DeployResult
		err    error
	}, 1)

	c.channel.onRef(ref, func(payload any) {
		resp, ok := payload.(map[string]any)
		if !ok {
			done <- struct {
				result *DeployResult
				err    error
			}{nil, fmt.Errorf("invalid response format")}
			return
		}

		if status, ok := resp["status"].(string); ok && status == "error" {
			errResp, _ := resp["response"].(map[string]any)
			errMsg := "unknown error"
			if msg, ok := errResp["message"].(string); ok {
				errMsg = msg
			}
			done <- struct {
				result *DeployResult
				err    error
			}{nil, fmt.Errorf("deploy failed: %s", errMsg)}
			return
		}

		response, _ := resp["response"].(map[string]any)
		version, _ := response["version"].(string)
		fileCount := 0
		if fc, ok := response["file_count"].(float64); ok {
			fileCount = int(fc)
		}

		done <- struct {
			result *DeployResult
			err    error
		}{&DeployResult{
			AppID:     c.appID,
			Version:   version,
			FileCount: fileCount,
		}, nil}
	})

	select {
	case result := <-done:
		return result.result, result.err
	case <-time.After(c.timeout):
		return nil, fmt.Errorf("deploy response timeout")
	}
}

// InstallResult represents the result of a successful installation.
type InstallResult struct {
	AppID   string `json:"app_id"`
	Version string `json:"version"`
	Success bool   `json:"success"`
}

// Install triggers the installation of the app version.
func (c *Client) Install() (*InstallResult, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("client not connected")
	}

	done := make(chan struct {
		result *InstallResult
		err    error
	}, 1)

	// Send install event with empty payload
	ref, err := c.channel.Push("install", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("failed to send install command: %w", err)
	}

	c.channel.onRef(ref, func(payload any) {
		resp, ok := payload.(map[string]any)
		if !ok {
			done <- struct {
				result *InstallResult
				err    error
			}{nil, fmt.Errorf("invalid response format")}
			return
		}

		if status, _ := resp["status"].(string); status != "ok" {
			response, _ := resp["response"].(map[string]any)
			msg := "install failed"
			if response != nil {
				if m, ok := response["message"].(string); ok {
					msg = m
				}
			}
			done <- struct {
				result *InstallResult
				err    error
			}{nil, fmt.Errorf("%s", msg)}
			return
		}

		response, _ := resp["response"].(map[string]any)
		version := ""
		if v, ok := response["version"].(string); ok {
			version = v
		}

		done <- struct {
			result *InstallResult
			err    error
		}{&InstallResult{
			AppID:   c.appID,
			Version: version,
			Success: true,
		}, nil}
	})

	select {
	case result := <-done:
		return result.result, result.err
	case <-time.After(c.timeout):
		return nil, fmt.Errorf("install response timeout")
	}
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

// IsConnected returns true if the client is connected to the socket.
func (c *Client) IsConnected() bool {
	return c.socket != nil && c.socket.IsConnected()
}

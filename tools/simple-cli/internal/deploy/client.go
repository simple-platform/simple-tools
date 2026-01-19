package deploy

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/nshafer/phx"
)

// Client handles deployment via Phoenix Channel.
type Client struct {
	endpoint string
	jwt      string
	appID    string
	socket   *phx.Socket
	channel  *phx.Channel
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
	endpointURL, err := url.Parse(fmt.Sprintf("wss://%s/socket/websocket?auth_token=%s", c.endpoint, c.jwt))
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}

	socket := phx.NewSocket(endpointURL)
	socket.ReconnectAfterFunc = func(attempts int) time.Duration {
		// Don't auto-reconnect for CLI
		return 0
	}

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
	channel := c.socket.Channel(fmt.Sprintf("deploy:%s", appID), nil)

	// Use a channel to wait for join response
	done := make(chan error, 1)

	join, err := channel.Join()
	if err != nil {
		return fmt.Errorf("failed to join channel: %w", err)
	}

	join.Receive("ok", func(response any) {
		done <- nil
	})
	join.Receive("error", func(response any) {
		done <- fmt.Errorf("join rejected: %v", response)
	})

	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-time.After(c.timeout):
		return fmt.Errorf("join channel timeout")
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

	done := make(chan struct {
		files []string
		err   error
	}, 1)

	push, err := c.channel.Push("manifest", map[string]interface{}{
		"files":   fileList,
		"version": version,
	})
	if err != nil {
		return nil, fmt.Errorf("manifest push failed: %w", err)
	}

	push.Receive("ok", func(response any) {
		resp, ok := response.(map[string]interface{})
		if !ok {
			done <- struct {
				files []string
				err   error
			}{nil, fmt.Errorf("invalid response format")}
			return
		}

		needFiles, ok := resp["need_files"].([]interface{})
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

	push.Receive("error", func(response any) {
		done <- struct {
			files []string
			err   error
		}{nil, fmt.Errorf("manifest rejected: %v", response)}
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

// sendFile sends a single file (base64 encoded).
func (c *Client) sendFile(path string, fi FileInfo) error {
	done := make(chan error, 1)

	push, err := c.channel.Push("file", map[string]interface{}{
		"path": path,
		"hash": fi.Hash,
		"data": base64.StdEncoding.EncodeToString(fi.Content),
	})
	if err != nil {
		return fmt.Errorf("file push failed for %s: %w", path, err)
	}

	push.Receive("ok", func(response any) {
		done <- nil
	})

	push.Receive("error", func(response any) {
		done <- fmt.Errorf("file rejected for %s: %v", path, response)
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

	done := make(chan struct {
		result *DeployResult
		err    error
	}, 1)

	push, err := c.channel.Push("deploy", map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("deploy push failed: %w", err)
	}

	push.Receive("ok", func(response any) {
		resp, ok := response.(map[string]interface{})
		if !ok {
			done <- struct {
				result *DeployResult
				err    error
			}{nil, fmt.Errorf("invalid response format")}
			return
		}

		version, _ := resp["version"].(string)
		fileCount := 0
		if fc, ok := resp["file_count"].(float64); ok {
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

	push.Receive("error", func(response any) {
		errMsg := "unknown error"
		if resp, ok := response.(map[string]interface{}); ok {
			if msg, ok := resp["error"].(string); ok {
				errMsg = msg
			}
		}
		done <- struct {
			result *DeployResult
			err    error
		}{nil, fmt.Errorf("deploy failed: %s", errMsg)}
	})

	select {
	case result := <-done:
		return result.result, result.err
	case <-time.After(c.timeout):
		return nil, fmt.Errorf("deploy response timeout")
	}
}

// Close disconnects from the socket.
func (c *Client) Close() {
	if c.channel != nil {
		_, _ = c.channel.Leave()
	}
	if c.socket != nil {
		c.socket.Disconnect()
	}
}

// IsConnected returns true if the client is connected to the socket.
func (c *Client) IsConnected() bool {
	return c.socket != nil && c.socket.IsConnected()
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

const (
	devStudioLoopbackHost    = "127.0.0.1"
	devStudioDefaultPort     = "47831"
	devStudioHealthPath      = "/health"
	devStudioWebSocketPath   = "/v1/devstudio"
	devStudioProtocolVersion = 1
	devStudioMaxMessageSize  = 16 * 1024
	devStudioAppsDirectory   = "apps"
)

var (
	devStudioWorkspaceRoot       = defaultDevStudioWorkspaceRoot()
	devStudioProductionRoot      = devStudioWorkspaceRoot
	devStudioInstanceIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	devStudioInstanceHostPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

func defaultDevStudioWorkspaceRoot() string {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDirectory, "workspace", "simple-devstudio")
}

func isValidDevStudioInstanceID(instanceID string) bool {
	if instanceID == "." || instanceID == ".." {
		return false
	}
	return devStudioInstanceIDPattern.MatchString(instanceID)
}

func isValidDevStudioInstanceHost(instanceID, instanceHost string) bool {
	host := instanceHost
	if h, _, err := net.SplitHostPort(instanceHost); err == nil {
		host = h
	}
	if !devStudioInstanceHostPattern.MatchString(host) {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 || labels[0] != instanceID {
		return false
	}
	for _, label := range labels {
		if !isValidDevStudioInstanceID(label) {
			return false
		}
	}
	return true
}

func isValidDevStudioOrigin(origin string) bool {
	trimmed := strings.TrimSpace(origin)
	if trimmed == "" || trimmed != origin {
		return false
	}
	parsedURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if parsedURL.Opaque != "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return false
	}
	if parsedURL.Path != "" && parsedURL.Path != "/" {
		return false
	}
	if parsedURL.Host == "" {
		return false
	}
	if port := parsedURL.Port(); port != "" {
		for _, ch := range port {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	hostname := strings.ToLower(parsedURL.Hostname())
	if hostname == "" {
		return false
	}
	if !devStudioInstanceHostPattern.MatchString(hostname) {
		return false
	}
	if !strings.HasSuffix(hostname, ".simple.lcl") && !strings.HasSuffix(hostname, ".simple.dev") {
		return false
	}
	labels := strings.Split(hostname, ".")
	if len(labels) < 3 {
		return false
	}
	for _, label := range labels {
		if !isValidDevStudioInstanceID(label) {
			return false
		}
	}
	return true
}

func isValidDevStudioOriginRequest(request *http.Request) bool {
	if request == nil || request.Header == nil {
		return false
	}
	origins := request.Header["Origin"]
	if len(origins) != 1 {
		return false
	}
	return isValidDevStudioOrigin(origins[0])
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func devStudioDefaultCandidateRoots(instanceID string) []string {
	var roots []string
	if devStudioProductionRoot != "" {
		roots = append(roots, filepath.Join(devStudioProductionRoot, instanceID))
	}
	homeDir, err := os.UserHomeDir()
	if err == nil && homeDir != "" {
		candidates := []string{
			filepath.Join(homeDir, "workspace", "simple-devstudio", instanceID),
			filepath.Join(homeDir, "workspace", "simple", instanceID),
			filepath.Join(homeDir, "simple", instanceID),
			filepath.Join(homeDir, "simple-devstudio", instanceID),
			filepath.Join(homeDir, "workspace", instanceID),
		}
		for _, c := range candidates {
			found := false
			for _, r := range roots {
				if r == c {
					found = true
					break
				}
			}
			if !found {
				roots = append(roots, c)
			}
		}
	}
	return roots
}

func findExistingDevStudioInstanceRoot(instanceID string) (string, bool) {
	for _, candidate := range devStudioDefaultCandidateRoots(instanceID) {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

type devStudioWorkspaceResolver func(instanceID string) (*devStudioWorkspace, error)

// ensureDevStudioInstanceRootAtPath ensures an instance workspace exists at the given path,
// scaffolding it via scaffold.CreateMonorepoStructure if it does not already exist.
func ensureDevStudioInstanceRootAtPath(instanceID, targetPath string) (string, error) {
	if !isValidDevStudioInstanceID(instanceID) {
		return "", fmt.Errorf("invalid instance ID: %q", instanceID)
	}
	resolvedPath := expandHome(targetPath)
	if !filepath.IsAbs(resolvedPath) {
		abs, err := filepath.Abs(resolvedPath)
		if err != nil {
			return "", fmt.Errorf("invalid instance path: %w", err)
		}
		resolvedPath = abs
	}
	if info, err := os.Stat(resolvedPath); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("instance path is not a directory: %s", resolvedPath)
		}
		if err := os.MkdirAll(filepath.Join(resolvedPath, devStudioAppsDirectory), 0755); err != nil {
			return "", fmt.Errorf("failed to create instance apps directory: %w", err)
		}
		return resolvedPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to inspect instance workspace root: %w", err)
	}

	cfg := scaffold.MonorepoConfig{
		ProjectName: instanceID,
		TenantName:  instanceID,
	}
	if err := scaffold.CreateMonorepoStructure(fsx.OSFileSystem{}, scaffold.TemplatesFS, resolvedPath, cfg); err != nil {
		return "", fmt.Errorf("failed to initialize instance workspace: %w", err)
	}
	if err := exec.Command("git", "-C", resolvedPath, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		_ = exec.Command("git", "init", resolvedPath).Run()
	}
	return resolvedPath, nil
}

// ensureDevStudioInstanceRoot creates the monorepo root structure using the platform's scaffold.
func ensureDevStudioInstanceRoot(instanceID string) (bool, error) {
	if !isValidDevStudioInstanceID(instanceID) {
		return false, fmt.Errorf("invalid instance ID: %q", instanceID)
	}
	if foundPath, ok := findExistingDevStudioInstanceRoot(instanceID); ok {
		_, err := ensureDevStudioInstanceRootAtPath(instanceID, foundPath)
		return false, err
	}
	target := filepath.Join(devStudioProductionRoot, instanceID)
	_, err := ensureDevStudioInstanceRootAtPath(instanceID, target)
	if err != nil {
		return false, err
	}
	return true, nil
}

var devStudioCmd = &cobra.Command{
	Use:   "devstudio",
	Short: "Run the local DevStudio bridge",
}

var devStudioServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the local DevStudio browser bridge",
	Args:  cobra.NoArgs,
	RunE:  runDevStudioServeCommand,
}

func init() {
	RootCmd.AddCommand(devStudioCmd)
	devStudioCmd.AddCommand(devStudioServeCmd)
}

func runDevStudioServeCommand(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", net.JoinHostPort(devStudioLoopbackHost, devStudioDefaultPort))
	if err != nil {
		return fmt.Errorf("failed to bind DevStudio bridge to %s:%s: %w", devStudioLoopbackHost, devStudioDefaultPort, err)
	}

	return serveDevStudio(ctx, listener, cmd.OutOrStdout())
}

func serveDevStudio(ctx context.Context, listener net.Listener, output io.Writer, resolvers ...devStudioWorkspaceResolver) error {
	return serveDevStudioWithBridge(ctx, listener, output, newDevStudioBridge(resolvers...))
}

func serveDevStudioWithBridge(ctx context.Context, listener net.Listener, output io.Writer, bridge *devStudioBridge) error {
	defer func() {
		bridge.mu.Lock()
		session := bridge.session
		bridge.session = nil
		bridge.mu.Unlock()
		if session != nil {
			session.close()
		}
	}()

	server := &http.Server{Handler: bridge.handler()}
	address := listener.Addr().String()

	fmt.Fprintf(output, "Bound address: %s\n", address)
	fmt.Fprintf(output, "Health: http://%s%s\n", address, devStudioHealthPath)
	fmt.Fprintf(output, "WebSocket: ws://%s%s\n", address, devStudioWebSocketPath)
	fmt.Fprintln(output, "Press Ctrl+C to stop.")

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("DevStudio bridge stopped: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to stop DevStudio bridge: %w", err)
		}
		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("DevStudio bridge stopped unexpectedly: %w", err)
		}
		return nil
	}
}

type devStudioWorkspace struct {
	instanceID   string
	instanceRoot string
	appDirs      []string
}

func (workspace *devStudioWorkspace) appPath(appID string) string {
	return filepath.Join(workspace.instanceRoot, devStudioAppsDirectory, appID)
}

func resolveDevStudioWorkspace(instanceID string) (*devStudioWorkspace, error) {
	return resolveDevStudioWorkspaceAtPath(instanceID, "")
}

func resolveDevStudioWorkspaceAtPath(instanceID, customPath string) (*devStudioWorkspace, error) {
	if !isValidDevStudioInstanceID(instanceID) {
		return nil, fmt.Errorf("invalid instance ID: %q", instanceID)
	}

	var targetPath string
	if customPath != "" {
		targetPath = expandHome(customPath)
	} else {
		foundPath, ok := findExistingDevStudioInstanceRoot(instanceID)
		if !ok {
			return nil, fmt.Errorf("instance root directory does not exist: %q", instanceID)
		}
		targetPath = foundPath
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, fmt.Errorf("instance root directory does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("instance root path is not a directory")
	}

	canonicalTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize instance root path: %w", err)
	}
	canonicalTargetInfo, err := os.Stat(canonicalTarget)
	if err != nil || !canonicalTargetInfo.IsDir() {
		return nil, fmt.Errorf("canonical instance root path is not a directory")
	}

	appsRoot := filepath.Join(canonicalTarget, devStudioAppsDirectory)
	appsInfo, err := os.Stat(appsRoot)
	if err != nil {
		return nil, fmt.Errorf("apps directory does not exist: %w", err)
	}
	if !appsInfo.IsDir() {
		return nil, fmt.Errorf("apps path is not a directory")
	}
	canonicalAppsRoot, err := filepath.EvalSymlinks(appsRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize apps directory: %w", err)
	}
	appsRel, err := filepath.Rel(canonicalTarget, canonicalAppsRoot)
	if err != nil || appsRel != devStudioAppsDirectory {
		return nil, fmt.Errorf("apps directory path escapes instance root")
	}

	entries, err := os.ReadDir(canonicalAppsRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read instance root directory: %w", err)
	}

	var appDirs []string
	for _, entry := range entries {
		entryPath := filepath.Join(canonicalAppsRoot, entry.Name())
		canonicalAppPath, err := filepath.EvalSymlinks(entryPath)
		if err != nil {
			continue
		}
		appInfo, err := os.Stat(canonicalAppPath)
		if err != nil || !appInfo.IsDir() {
			continue
		}
		appRel, err := filepath.Rel(canonicalAppsRoot, canonicalAppPath)
		if err != nil || appRel == "." || appRel == ".." || strings.HasPrefix(appRel, ".."+string(filepath.Separator)) || filepath.IsAbs(appRel) {
			continue
		}
		appConfigPath := filepath.Join(canonicalAppPath, "app.scl")
		if appConfigInfo, err := os.Stat(appConfigPath); err == nil && !appConfigInfo.IsDir() {
			appDirs = append(appDirs, entry.Name())
		}
	}

	return &devStudioWorkspace{
		instanceID:   instanceID,
		instanceRoot: canonicalTarget,
		appDirs:      appDirs,
	}, nil
}

type devStudioBridge struct {
	mu           sync.Mutex
	session      *devStudioSession
	upgrader     websocket.Upgrader
	resolver     devStudioWorkspaceResolver
	codexStarter devStudioCodexStarter
}

type devStudioSession struct {
	stateMu    sync.Mutex
	instanceID string
	deployKey  string
	workspace  *devStudioWorkspace
	writerMu   sync.Mutex
	send       func(any) error
	codex      *devStudioCodexAdapter
	closed     bool
}

func (session *devStudioSession) setCodex(adapter *devStudioCodexAdapter) bool {
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	if session.closed {
		return false
	}
	session.codex = adapter
	return true
}

func (session *devStudioSession) codexAdapter() *devStudioCodexAdapter {
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	if session.closed {
		return nil
	}
	return session.codex
}

func (session *devStudioSession) deployKeyForCodex() (string, bool) {
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	if session.closed || session.deployKey == "" {
		return "", false
	}
	return session.deployKey, true
}

func (session *devStudioSession) close() {
	session.stateMu.Lock()
	if session.closed {
		session.stateMu.Unlock()
		return
	}
	session.closed = true
	codex := session.codex
	session.codex = nil
	session.deployKey = ""
	session.stateMu.Unlock()
	if codex != nil {
		codex.close()
	}
}

type devStudioConnectRequest struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocolVersion"`
	InstanceID      string `json:"instanceId"`
	InstanceHost    string `json:"instanceHost"`
	DeployKey       string `json:"deployKey"`
	InstancePath    string `json:"instancePath,omitempty"`
}

type devStudioSessionReady struct {
	Type              string `json:"type"`
	ProtocolVersion   int    `json:"protocolVersion"`
	InstanceID        string `json:"instanceId"`
	InstanceRootReady bool   `json:"instanceRootReady"`
}

type devStudioProtocolError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newDevStudioBridge(resolvers ...devStudioWorkspaceResolver) *devStudioBridge {
	return newDevStudioBridgeWithCodexStarter(newExecDevStudioCodexStarter(), resolvers...)
}

func newDevStudioBridgeWithCodexStarter(starter devStudioCodexStarter, resolvers ...devStudioWorkspaceResolver) *devStudioBridge {
	var resolver devStudioWorkspaceResolver = resolveDevStudioWorkspace
	if len(resolvers) > 0 && resolvers[0] != nil {
		resolver = resolvers[0]
	}
	if starter == nil {
		starter = newExecDevStudioCodexStarter()
	}
	return &devStudioBridge{
		resolver:     resolver,
		codexStarter: starter,
		upgrader: websocket.Upgrader{
			CheckOrigin: isValidDevStudioOriginRequest,
		},
	}
}

func (bridge *devStudioBridge) handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case devStudioHealthPath:
			bridge.handleHealth(writer, request)
		case devStudioWebSocketPath:
			bridge.handleWebSocket(writer, request)
		default:
			http.NotFound(writer, request)
		}
	})
}

func (bridge *devStudioBridge) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]string{"status": "ok"})
}

func (bridge *devStudioBridge) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	connection, err := bridge.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	connection.SetReadLimit(devStudioMaxMessageSize)

	requestMessage, protocolError := readDevStudioConnectRequest(connection)
	if protocolError != nil {
		log.Printf("[DevStudio Bridge] Session connect error: code=%s, message=%s", protocolError.Code, protocolError.Message)
		bridge.closeWithError(connection, *protocolError)
		return
	}

	log.Printf("[DevStudio Bridge] Session connect received: instanceID=%q, instanceHost=%q, hasDeployKey=%t, hasInstancePath=%t", requestMessage.InstanceID, requestMessage.InstanceHost, requestMessage.DeployKey != "", requestMessage.InstancePath != "")

	var ws *devStudioWorkspace
	if requestMessage.InstancePath != "" {
		instanceRoot, err := ensureDevStudioInstanceRootAtPath(requestMessage.InstanceID, requestMessage.InstancePath)
		if err != nil {
			log.Printf("[DevStudio Bridge] Workspace initialization failed for path %q: %v", requestMessage.InstancePath, err)
			bridge.closeWithError(connection, devStudioProtocolError{
				Type:    "error",
				Code:    "workspace_initialization_failed",
				Message: fmt.Sprintf("Failed to initialize workspace at %s: %v", requestMessage.InstancePath, err),
			})
			return
		}
		ws, err = resolveDevStudioWorkspaceAtPath(requestMessage.InstanceID, instanceRoot)
		if err != nil {
			bridge.closeWithError(connection, devStudioProtocolError{
				Type:    "error",
				Code:    "workspace_initialization_failed",
				Message: "The workspace path is invalid or missing.",
			})
			return
		}
	} else {
		foundPath, found := findExistingDevStudioInstanceRoot(requestMessage.InstanceID)
		if !found {
			log.Printf("[DevStudio Bridge] Instance %q not found in default locations", requestMessage.InstanceID)
			bridge.closeWithError(connection, devStudioProtocolError{
				Type:    "error",
				Code:    "instance_path_required",
				Message: "Instance workspace was not found in default locations. Please provide the instance workspace path.",
			})
			return
		}

		instanceRoot, err := ensureDevStudioInstanceRootAtPath(requestMessage.InstanceID, foundPath)
		if err != nil {
			bridge.closeWithError(connection, devStudioProtocolError{
				Type:    "error",
				Code:    "workspace_initialization_failed",
				Message: "The workspace root for the instance could not be initialized.",
			})
			return
		}

		resolver := bridge.resolver
		if resolver == nil {
			resolver = resolveDevStudioWorkspace
		}
		var resolveErr error
		ws, resolveErr = resolver(requestMessage.InstanceID)
		if resolveErr != nil {
			ws, resolveErr = resolveDevStudioWorkspaceAtPath(requestMessage.InstanceID, instanceRoot)
			if resolveErr != nil {
				bridge.closeWithError(connection, devStudioProtocolError{
					Type:    "error",
					Code:    "invalid_instance_id",
					Message: "The workspace root for the instance ID is invalid or missing.",
				})
				return
			}
		}
	}

	if err := ensureDevStudioSCLConfiguration(ws, requestMessage.InstanceHost, requestMessage.DeployKey); err != nil {
		bridge.closeWithError(connection, devStudioProtocolError{
			Type:    "error",
			Code:    "workspace_configuration_failed",
			Message: "The instance workspace could not be prepared for DevStudio.",
		})
		return
	}

	session, protocolError := bridge.beginSession(requestMessage.InstanceID, requestMessage.DeployKey, ws)
	if protocolError != nil {
		bridge.closeWithError(connection, *protocolError)
		return
	}
	defer bridge.endSession(session)
	session.send = func(event any) error {
		session.writerMu.Lock()
		defer session.writerMu.Unlock()
		return connection.WriteJSON(event)
	}
	deployKey, ok := session.deployKeyForCodex()
	if !ok {
		return
	}
	codex := newDevStudioCodexAdapter(
		bridge.codexStarter,
		session.instanceID,
		session.workspace,
		deployKey,
		session.send,
	)
	if !session.setCodex(codex) {
		codex.close()
		return
	}

	if err := connection.WriteJSON(devStudioSessionReady{
		Type:              "session.ready",
		ProtocolVersion:   devStudioProtocolVersion,
		InstanceID:        session.instanceID,
		InstanceRootReady: true,
	}); err != nil {
		return
	}

	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			_ = session.send(devStudioProtocolError{Type: "error", Code: "invalid_message", Message: "The bridge received an invalid message."})
			continue
		}
		if protocolError := session.handleBrowserMessage(payload); protocolError != nil {
			_ = session.send(*protocolError)
		}
	}
}

func readDevStudioConnectRequest(connection *websocket.Conn) (devStudioConnectRequest, *devStudioProtocolError) {
	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "invalid_message",
			Message: "The bridge received an invalid message.",
		}
	}
	if messageType != websocket.TextMessage {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "invalid_message",
			Message: "The bridge received an invalid message.",
		}
	}

	var request devStudioConnectRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "invalid_message",
			Message: "The bridge received an invalid message.",
		}
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "invalid_message",
			Message: "The bridge received an invalid message.",
		}
	}

	if request.Type != "session.connect" {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "unsupported_message_type",
			Message: "This message type is not supported.",
		}
	}
	if request.ProtocolVersion != devStudioProtocolVersion {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "unsupported_protocol_version",
			Message: "This DevStudio protocol version is not supported.",
		}
	}
	if !isValidDevStudioInstanceID(request.InstanceID) {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "invalid_instance_id",
			Message: "The instance ID is invalid.",
		}
	}
	if !isValidDevStudioInstanceHost(request.InstanceID, request.InstanceHost) {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "invalid_instance_host",
			Message: "The instance host is invalid.",
		}
	}
	if strings.TrimSpace(request.DeployKey) == "" {
		return devStudioConnectRequest{}, &devStudioProtocolError{
			Type:    "error",
			Code:    "invalid_deploy_key",
			Message: "The deploy key is missing or invalid.",
		}
	}

	return request, nil
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func (bridge *devStudioBridge) beginSession(instanceID, deployKey string, ws *devStudioWorkspace) (*devStudioSession, *devStudioProtocolError) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()

	if bridge.session != nil {
		return nil, &devStudioProtocolError{
			Type:    "error",
			Code:    "session_already_active",
			Message: "A DevStudio browser session is already active.",
		}
	}

	session := &devStudioSession{
		instanceID: instanceID,
		deployKey:  deployKey,
		workspace:  ws,
	}
	bridge.session = session
	return session, nil
}

func (bridge *devStudioBridge) endSession(session *devStudioSession) {
	bridge.mu.Lock()
	if bridge.session == session {
		bridge.session = nil
	}
	bridge.mu.Unlock()

	if session != nil {
		session.close()
	}
}

func (bridge *devStudioBridge) closeWithError(connection *websocket.Conn, protocolError devStudioProtocolError) {
	_ = connection.WriteJSON(protocolError)
	_ = connection.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "invalid DevStudio bridge message"),
		time.Now().Add(time.Second),
	)
	time.Sleep(50 * time.Millisecond)
}

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
	devStudioProtocolVersion = 2
	devStudioMaxMessageSize  = 16 * 1024
	devStudioAppsDirectory   = "apps"
)

// devStudioSessionHandoverWait is how long a new connection waits for a
// session that is still ending, such as the one a reloading page has just
// dropped, before it is refused. A real second tab is still refused, only
// later. A variable so a test can shorten it.
var devStudioSessionHandoverWait = 2 * time.Second

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
		root, err := confineDevStudioInstanceRoot(instanceID, candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			return root, true
		}
	}
	return "", false
}

// confineDevStudioInstanceRoot turns the folder the browser named for an
// instance into the folder the bridge and Codex will work in, or refuses it.
// Codex may write anywhere in that folder, so it must be a folder of the
// instance's own, named after it, inside the person's home folder: never the
// home folder itself, "/", a folder above home, or a place a symbolic link
// leads to outside home. Symbolic links are resolved here, once; everything
// after works from the folder this returns. Each refusal is a plain sentence,
// because the browser shows it to the person who typed the folder.
func confineDevStudioInstanceRoot(instanceID, requestedPath string) (string, error) {
	if !isValidDevStudioInstanceID(instanceID) {
		return "", fmt.Errorf("invalid instance ID: %q", instanceID)
	}
	requested := expandHome(requestedPath)
	if !filepath.IsAbs(requested) {
		abs, err := filepath.Abs(requested)
		if err != nil {
			return "", fmt.Errorf("invalid instance path: %w", err)
		}
		requested = abs
	}
	for _, element := range strings.Split(filepath.ToSlash(requested), "/") {
		if element == ".." {
			return "", errors.New(`the workspace folder must be a direct path, without ".."`)
		}
	}
	nameRefusal := fmt.Errorf("the workspace folder must be named after the instance, %s, and must not be a link to a folder with another name", instanceID)
	if filepath.Base(requested) != instanceID {
		return "", nameRefusal
	}
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find your home folder: %w", err)
	}
	canonicalHome, err := filepath.EvalSymlinks(homeDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to resolve your home folder: %w", err)
	}
	root, err := devStudioResolve(requested)
	if err != nil {
		return "", err
	}
	if filepath.Base(root) != instanceID {
		return "", nameRefusal
	}
	// The separator makes this a real containment check: the home folder
	// itself, "/", every folder above home, and a sibling such as
	// /Users/alice2 next to /Users/alice all fail it.
	if !strings.HasPrefix(root, canonicalHome+string(filepath.Separator)) {
		return "", errors.New("the workspace folder must be inside your home folder, and not the home folder itself")
	}
	return root, nil
}

// devStudioResolve follows every symbolic link in path. Folders at the end of
// path that do not exist yet keep their names, under the real location of the
// deepest folder that does, so a folder about to be created is measured where
// it will really be.
func devStudioResolve(path string) (string, error) {
	existing := filepath.Clean(path)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...), nil
		}
		parent := filepath.Dir(existing)
		if !errors.Is(err, os.ErrNotExist) || parent == existing {
			return "", fmt.Errorf("failed to resolve %s: %w", path, err)
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
}

// devStudioPathInside returns the real location of elements under folder, a
// folder whose symbolic links are already resolved, and refuses one that
// lands outside it. Codex can write anywhere in an instance folder, links
// included, while the bridge runs outside Codex's sandbox, so every path the
// bridge builds there goes through this check: a link the agent planted could
// otherwise send the bridge's own reads and writes somewhere else.
func devStudioPathInside(folder string, elements ...string) (string, error) {
	relative := filepath.Join(elements...)
	path, err := devStudioResolve(filepath.Join(folder, relative))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(path, folder+string(filepath.Separator)) {
		return "", fmt.Errorf("%s leads outside the instance folder", relative)
	}
	// A link to something that does not exist resolves to its own name, and
	// writing to it would create its target wherever it points.
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s is a link to something that does not exist", relative)
	}
	return path, nil
}

type devStudioWorkspaceResolver func(instanceID string) (*devStudioWorkspace, error)

// ensureDevStudioInstanceRootAtPath confines the given path and ensures an instance
// workspace exists there, scaffolding it via scaffold.CreateMonorepoStructure if it
// does not already exist. It returns the confined root.
func ensureDevStudioInstanceRootAtPath(instanceID, targetPath string) (string, error) {
	resolvedPath, err := confineDevStudioInstanceRoot(instanceID, targetPath)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(resolvedPath); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("instance path is not a directory: %s", resolvedPath)
		}
		appsPath, err := devStudioPathInside(resolvedPath, devStudioAppsDirectory)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(appsPath, fsx.DirPerm); err != nil {
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
		bridge.mu.Unlock()
		bridge.endSession(session)
	}()

	server := &http.Server{Handler: bridge.handler()}
	address := listener.Addr().String()

	_, _ = fmt.Fprintf(output, "Bound address: %s\n", address)
	_, _ = fmt.Fprintf(output, "Health: http://%s%s\n", address, devStudioHealthPath)
	_, _ = fmt.Fprintf(output, "WebSocket: ws://%s%s\n", address, devStudioWebSocketPath)
	_, _ = fmt.Fprintln(output, "Press Ctrl+C to stop.")

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

func resolveDevStudioWorkspace(instanceID string) (*devStudioWorkspace, error) {
	return resolveDevStudioWorkspaceAtPath(instanceID, "")
}

func resolveDevStudioWorkspaceAtPath(instanceID, customPath string) (*devStudioWorkspace, error) {
	if !isValidDevStudioInstanceID(instanceID) {
		return nil, fmt.Errorf("invalid instance ID: %q", instanceID)
	}

	var instanceRoot string
	if customPath != "" {
		confinedPath, err := confineDevStudioInstanceRoot(instanceID, customPath)
		if err != nil {
			return nil, err
		}
		instanceRoot = confinedPath
	} else {
		foundPath, ok := findExistingDevStudioInstanceRoot(instanceID)
		if !ok {
			return nil, fmt.Errorf("instance root directory does not exist: %q", instanceID)
		}
		instanceRoot = foundPath
	}

	info, err := os.Stat(instanceRoot)
	if err != nil {
		return nil, fmt.Errorf("instance root directory does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("instance root path is not a directory")
	}

	appsRoot, err := devStudioPathInside(instanceRoot, devStudioAppsDirectory)
	if err != nil {
		return nil, fmt.Errorf("apps directory path escapes instance root: %w", err)
	}
	appsInfo, err := os.Stat(appsRoot)
	if err != nil {
		return nil, fmt.Errorf("apps directory does not exist: %w", err)
	}
	if !appsInfo.IsDir() {
		return nil, fmt.Errorf("apps path is not a directory")
	}

	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read instance root directory: %w", err)
	}

	var appDirs []string
	for _, entry := range entries {
		appPath, err := devStudioPathInside(appsRoot, entry.Name())
		if err != nil {
			continue
		}
		appInfo, err := os.Stat(appPath)
		if err != nil || !appInfo.IsDir() {
			continue
		}
		appConfigPath, err := devStudioPathInside(appPath, "app.scl")
		if err != nil {
			continue
		}
		if appConfigInfo, err := os.Stat(appConfigPath); err == nil && !appConfigInfo.IsDir() {
			appDirs = append(appDirs, entry.Name())
		}
	}

	return &devStudioWorkspace{
		instanceID:   instanceID,
		instanceRoot: instanceRoot,
		appDirs:      appDirs,
	}, nil
}

type devStudioBridge struct {
	mu           sync.Mutex
	session      *devStudioSession
	upgrader     websocket.Upgrader
	resolver     devStudioWorkspaceResolver
	codexStarter devStudioCodexStarter
	// knowledgeFetcher reads the instance's DevStudio playbook and skills at
	// connect; nil reads nothing.
	knowledgeFetcher devStudioKnowledgeFetcher
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
	// ended is closed once the session has closed and left the bridge, so
	// a connection waiting for it can take over.
	ended chan struct{}
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

// deployKeyForCodex returns the key the browser sent, which is empty when it
// sent none, and false once the session is closed.
func (session *devStudioSession) deployKeyForCodex() (string, bool) {
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	if session.closed {
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
	ThreadID          string `json:"threadId"`
	// Resumed means the instance's earlier Codex conversation was resumed and
	// is replayed right after this message.
	Resumed bool `json:"resumed"`
}

type devStudioProtocolError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// WillRetry is always false for errors the bridge itself reports.
	WillRetry bool `json:"willRetry"`
}

func newDevStudioBridge(resolvers ...devStudioWorkspaceResolver) *devStudioBridge {
	bridge := newDevStudioBridgeWithCodexStarter(newExecDevStudioCodexStarter(), resolvers...)
	bridge.knowledgeFetcher = fetchDevStudioKnowledge
	return bridge
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

	log.Printf("[DevStudio Bridge] Session connect received: instanceID=%q, instanceHost=%q, hasDeployKey=%t, hasInstancePath=%t", devStudioLogValue(requestMessage.InstanceID), devStudioLogValue(requestMessage.InstanceHost), requestMessage.DeployKey != "", requestMessage.InstancePath != "")

	var ws *devStudioWorkspace
	if requestMessage.InstancePath != "" {
		instanceRoot, err := ensureDevStudioInstanceRootAtPath(requestMessage.InstanceID, requestMessage.InstancePath)
		if err != nil {
			log.Printf("[DevStudio Bridge] Workspace initialization failed for path %q: %s", devStudioLogValue(requestMessage.InstancePath), devStudioLogValue(err))
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
			log.Printf("[DevStudio Bridge] Instance %q not found in default locations", devStudioLogValue(requestMessage.InstanceID))
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

	keptWorkspaceKey, err := ensureDevStudioSCLConfiguration(ws, requestMessage.InstanceHost, requestMessage.DeployKey)
	if err != nil {
		bridge.closeWithError(connection, devStudioProtocolError{
			Type:    "error",
			Code:    "workspace_configuration_failed",
			Message: "The instance workspace could not be prepared for DevStudio.",
		})
		return
	}
	// Once the workspace's key pair is set up, the CLI signs in with it for
	// as long as the key lives, so the browser's key is ignored: written to
	// simple.scl, the next deploy would use it up, and it may be someone
	// else's. It is not written, logged or given to Codex. Only a workspace
	// with no set-up key takes the browser's key, the first time; with none
	// sent either, simple.scl is left as it is.
	if keptWorkspaceKey {
		requestMessage.DeployKey = ""
	}
	// A workspace made by an older CLI lacks root files a new one is
	// scaffolded with. Without them the session still runs as before, so a
	// failure here is only logged.
	if err := ensureDevStudioWorkspaceFiles(ws); err != nil {
		log.Printf("[DevStudio Bridge] The workspace's missing root files could not be added: %s", devStudioLogValue(err))
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
	// Before Codex starts, so it finds the skills and its conversation
	// opens with the playbook.
	codex.playbook = bridge.deliverKnowledge(session.workspace, deployKey)

	// Codex starts with the session, not on the first message, so that
	// session.ready can name the conversation and no Codex startup ever runs
	// inside the read loop below.
	threadID, resumed, err := codex.connect()
	if err != nil {
		log.Printf("[DevStudio Bridge] Codex could not be started: %s", devStudioLogValue(err))
		// Free the session first, so the browser can reconnect as soon as
		// it reads the error.
		bridge.endSession(session)
		session.writerMu.Lock()
		bridge.closeWithError(connection, devStudioProtocolError{
			Type:    "error",
			Code:    "codex_unavailable",
			Message: "Codex could not be started for this DevStudio session.",
		})
		session.writerMu.Unlock()
		return
	}
	var history []devStudioCodexTurn
	var historyErr error
	if resumed {
		history, historyErr = codex.readHistory()
	}
	if err := session.send(devStudioSessionReady{
		Type:              "session.ready",
		ProtocolVersion:   devStudioProtocolVersion,
		InstanceID:        session.instanceID,
		InstanceRootReady: true,
		ThreadID:          threadID,
		Resumed:           resumed,
	}); err != nil {
		return
	}
	if historyErr != nil {
		log.Printf("[DevStudio Bridge] %s", devStudioLogValue(historyErr))
		_ = session.send(devStudioProtocolError{Type: "error", Code: "history_unavailable", Message: "The earlier conversation could not be shown."})
	}
	codex.replay(history)

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
			Code:    "protocol_version_unsupported",
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
	// The key may be left out or empty; a key of only spaces is a mistake.
	if request.DeployKey != "" && strings.TrimSpace(request.DeployKey) == "" {
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

// beginSession takes the bridge's one session slot. A page that drops its
// connection and reconnects at once can arrive before the bridge has ended
// the old session, so it waits a short while for that session to end.
func (bridge *devStudioBridge) beginSession(instanceID, deployKey string, ws *devStudioWorkspace) (*devStudioSession, *devStudioProtocolError) {
	wait := time.NewTimer(devStudioSessionHandoverWait)
	defer wait.Stop()
	for {
		bridge.mu.Lock()
		current := bridge.session
		if current == nil {
			session := &devStudioSession{
				instanceID: instanceID,
				deployKey:  deployKey,
				workspace:  ws,
				ended:      make(chan struct{}),
			}
			bridge.session = session
			bridge.mu.Unlock()
			return session, nil
		}
		bridge.mu.Unlock()

		select {
		case <-current.ended:
		case <-wait.C:
			return nil, &devStudioProtocolError{
				Type:    "error",
				Code:    "session_already_active",
				Message: "A DevStudio browser session is already active.",
			}
		}
	}
}

// endSession closes session, stopping its Codex and restoring protected
// files, and only then frees the slot, so the next session never overlaps it.
func (bridge *devStudioBridge) endSession(session *devStudioSession) {
	if session == nil {
		return
	}
	session.close()
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.session == session {
		bridge.session = nil
		close(session.ended)
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

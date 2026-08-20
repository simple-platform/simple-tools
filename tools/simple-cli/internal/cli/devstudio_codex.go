package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"simple-cli/internal/fsx"
)

var (
	devStudioEnvBlockPattern = regexp.MustCompile(`^(\s*)env\s+devstudio\s*\{\s*(?:#.*)?$`)
	devStudioFieldPattern    = regexp.MustCompile(`^(\s*)(endpoint|api_key)\s+.*$`)
)

const (
	devStudioCodexExecutable = "codex"
	devStudioCodexTimeout    = 30 * time.Second
)

type devStudioCodexStarter interface {
	Start(devStudioCodexStartConfig) (devStudioCodexProcess, error)
}

type devStudioCodexStartConfig struct {
	WorkspaceRoot string
	InstanceID    string
	DeployKey     string
}

type devStudioCodexProcess interface {
	Input() io.WriteCloser
	Output() io.ReadCloser
	Stop()
}

type execDevStudioCodexStarter struct{}

func newExecDevStudioCodexStarter() devStudioCodexStarter {
	return execDevStudioCodexStarter{}
}

func (execDevStudioCodexStarter) Start(config devStudioCodexStartConfig) (devStudioCodexProcess, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, devStudioCodexExecutable, "app-server", "--stdio")
	cmd.Dir = config.WorkspaceRoot
	cmd.Env = devStudioCodexEnvironment(config.WorkspaceRoot, config.InstanceID, config.DeployKey)

	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open Codex stdin: %w", err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open Codex stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start Codex app-server: %w", err)
	}
	process := &execDevStudioCodexProcess{cancel: cancel, input: input, output: output}
	go func() { _ = cmd.Wait() }()
	return process, nil
}

func devStudioCodexEnvironment(workspaceRoot, instanceID, deployKey string) []string {
	environment := make([]string, 0, len(os.Environ())+5)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "SIMPLE_CLI_HOME=") || strings.HasPrefix(item, "SIMPLE_LOCAL_API_KEY=") || strings.HasPrefix(item, "SIMPLE_INSTANCE_ID=") || strings.HasPrefix(item, "XDG_DATA_HOME=") || strings.HasPrefix(item, "TMPDIR=") {
			continue
		}
		environment = append(environment, item)
	}
	if executable, err := os.Executable(); err == nil {
		environment = prependDevStudioExecutableDirectory(environment, filepath.Dir(executable))
	}
	dataDir := filepath.Join(workspaceRoot, ".simple", "data")
	tmpDir := filepath.Join(workspaceRoot, ".simple", "tmp")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(tmpDir, 0755)

	return append(environment,
		"SIMPLE_CLI_HOME="+workspaceRoot,
		"SIMPLE_LOCAL_API_KEY="+deployKey,
		"SIMPLE_INSTANCE_ID="+instanceID,
		"XDG_DATA_HOME="+dataDir,
		"TMPDIR="+tmpDir,
	)
}

func prependDevStudioExecutableDirectory(environment []string, directory string) []string {
	if directory == "" {
		return environment
	}
	prefix := "PATH="
	for index, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		environment[index] = prefix + directory + string(os.PathListSeparator) + strings.TrimPrefix(item, prefix)
		return environment
	}
	return append(environment, prefix+directory)
}

type execDevStudioCodexProcess struct {
	cancel context.CancelFunc
	input  io.WriteCloser
	output io.ReadCloser
	once   sync.Once
}

func (process *execDevStudioCodexProcess) Input() io.WriteCloser { return process.input }

func (process *execDevStudioCodexProcess) Output() io.ReadCloser { return process.output }

func (process *execDevStudioCodexProcess) Stop() {
	process.once.Do(func() {
		_ = process.input.Close()
		process.cancel()
	})
}

type devStudioCodexAdapter struct {
	mu         sync.Mutex
	turnMu     sync.Mutex
	writeMu    sync.Mutex
	starter    devStudioCodexStarter
	instanceID string
	workspace  *devStudioWorkspace
	deployKey  string
	send       func(any) error
	process    devStudioCodexProcess
	encoder    *json.Encoder
	pending    map[int]chan devStudioJSONRPCResponse
	nextID     int
	threadID   string
	turnID     string
	active     bool
	closed     bool
	protected  map[string]devStudioProtectedFile
}

type devStudioJSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type devStudioJSONRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type devStudioJSONRPCResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type devStudioJSONRPCEnvelope struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type devStudioProtectedFile struct {
	contents []byte
	hash     string
	mode     os.FileMode
}

func newDevStudioCodexAdapter(
	starter devStudioCodexStarter,
	instanceID string,
	workspace *devStudioWorkspace,
	deployKey string,
	send func(any) error,
) *devStudioCodexAdapter {
	return &devStudioCodexAdapter{
		starter:    starter,
		instanceID: instanceID,
		workspace:  workspace,
		deployKey:  deployKey,
		send:       send,
		pending:    make(map[int]chan devStudioJSONRPCResponse),
	}
}

func (session *devStudioSession) handleBrowserMessage(payload []byte) *devStudioProtocolError {
	adapter := session.codexAdapter()
	if adapter == nil {
		return &devStudioProtocolError{Type: "error", Code: "session_closed", Message: "The DevStudio session is closed."}
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return devStudioInvalidMessageError()
	}
	typeValue, ok := message["type"]
	if !ok {
		return devStudioInvalidMessageError()
	}
	var messageType string
	if err := json.Unmarshal(typeValue, &messageType); err != nil {
		return devStudioInvalidMessageError()
	}

	switch messageType {
	case "turn.start":
		if len(message) != 2 {
			return devStudioInvalidMessageError()
		}
		var text string
		if rawText, ok := message["text"]; !ok || json.Unmarshal(rawText, &text) != nil || strings.TrimSpace(text) == "" {
			return &devStudioProtocolError{Type: "error", Code: "invalid_turn", Message: "A non-empty chat request is required."}
		}
		if err := adapter.startTurn(text); err != nil {
			return &devStudioProtocolError{Type: "error", Code: devStudioTurnErrorCode(err), Message: devStudioTurnErrorMessage(err)}
		}
		return nil
	case "turn.cancel":
		if len(message) != 1 {
			return devStudioInvalidMessageError()
		}
		if err := adapter.cancelTurn(); err != nil {
			if strings.Contains(err.Error(), "no_active_turn") {
				return nil
			}
			return &devStudioProtocolError{Type: "error", Code: devStudioTurnErrorCode(err), Message: devStudioTurnErrorMessage(err)}
		}
		return nil
	default:
		return &devStudioProtocolError{Type: "error", Code: "unsupported_message_type", Message: "This message type is not supported."}
	}
}

func devStudioInvalidMessageError() *devStudioProtocolError {
	return &devStudioProtocolError{Type: "error", Code: "invalid_message", Message: "The bridge received an invalid message."}
}

func (adapter *devStudioCodexAdapter) startTurn(text string) error {
	adapter.turnMu.Lock()
	defer adapter.turnMu.Unlock()

	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return errors.New("session_closed")
	}
	if adapter.active {
		adapter.mu.Unlock()
		return errors.New("turn_active")
	}
	adapter.mu.Unlock()

	if err := adapter.ensureProcessAndThread(); err != nil {
		return err
	}
	protected, err := snapshotDevStudioProtectedFiles(adapter.workspace)
	if err != nil {
		return fmt.Errorf("protected_snapshot: %w", err)
	}
	adapter.mu.Lock()
	adapter.protected = protected
	adapter.active = true
	threadID := adapter.threadID
	adapter.mu.Unlock()
	adapter.emitStatus("reading_instructions")
	adapter.emitStatus("thinking")

	result, err := adapter.call("turn/start", map[string]any{
		"threadId":       threadID,
		"cwd":            adapter.workspace.instanceRoot,
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type":          "workspaceWrite",
			"writableRoots": []string{adapter.workspace.instanceRoot},
			"networkAccess": true,
		},
		"input": []map[string]string{{"type": "text", "text": text}},
	})
	if err != nil {
		_, _, _ = adapter.endActiveTurn()
		return err
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.Turn.ID == "" {
		_, _, _ = adapter.endActiveTurn()
		return errors.New("invalid_turn_response")
	}

	adapter.mu.Lock()
	if adapter.active {
		adapter.turnID = response.Turn.ID
	}
	adapter.mu.Unlock()
	return nil
}

func (adapter *devStudioCodexAdapter) ensureProcessAndThread() error {
	adapter.mu.Lock()
	if adapter.process != nil && adapter.threadID != "" {
		adapter.mu.Unlock()
		return nil
	}
	deployKey := adapter.deployKey
	adapter.mu.Unlock()

	process, err := adapter.starter.Start(devStudioCodexStartConfig{
		WorkspaceRoot: adapter.workspace.instanceRoot,
		InstanceID:    adapter.instanceID,
		DeployKey:     deployKey,
	})
	if err != nil {
		return fmt.Errorf("codex_start: %w", err)
	}

	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		process.Stop()
		return errors.New("session_closed")
	}
	adapter.process = process
	adapter.encoder = json.NewEncoder(process.Input())
	adapter.mu.Unlock()
	go adapter.readCodexOutput(process.Output())

	if _, err := adapter.call("initialize", map[string]any{
		"clientInfo": map[string]string{"name": "simple-devstudio", "version": "1"},
	}); err != nil {
		adapter.stopAndClearProcess()
		return fmt.Errorf("codex_initialize: %w", err)
	}
	if err := adapter.notify("initialized", map[string]any{}); err != nil {
		adapter.stopAndClearProcess()
		return fmt.Errorf("codex_initialized: %w", err)
	}
	adapter.emitStatus("inspecting_workspace")
	result, err := adapter.call("thread/start", map[string]any{
		"cwd":                   adapter.workspace.instanceRoot,
		"approvalPolicy":        "never",
		"sandbox":               "workspace-write",
		"developerInstructions": devStudioCodexInstructions(adapter.instanceID, adapter.workspace.instanceRoot),
		"ephemeral":             true,
	})
	if err != nil {
		adapter.stopAndClearProcess()
		return fmt.Errorf("codex_thread_start: %w", err)
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.Thread.ID == "" {
		adapter.stopAndClearProcess()
		return errors.New("invalid_thread_response")
	}
	adapter.mu.Lock()
	adapter.threadID = response.Thread.ID
	adapter.mu.Unlock()
	return nil
}

func (adapter *devStudioCodexAdapter) stopAndClearProcess() {
	adapter.mu.Lock()
	process := adapter.process
	adapter.process = nil
	adapter.encoder = nil
	adapter.threadID = ""
	pending := adapter.pending
	adapter.pending = make(map[int]chan devStudioJSONRPCResponse)
	adapter.mu.Unlock()

	for _, response := range pending {
		response <- devStudioJSONRPCResponse{
			Error: &struct {
				Message string `json:"message"`
			}{Message: "codex_unavailable"},
		}
	}
	if process != nil {
		process.Stop()
	}
}

func (adapter *devStudioCodexAdapter) call(method string, params any) (json.RawMessage, error) {
	adapter.mu.Lock()
	if adapter.closed || adapter.encoder == nil {
		adapter.mu.Unlock()
		return nil, errors.New("session_closed")
	}
	encoder := adapter.encoder
	adapter.nextID++
	id := adapter.nextID
	response := make(chan devStudioJSONRPCResponse, 1)
	adapter.pending[id] = response
	adapter.mu.Unlock()

	adapter.writeMu.Lock()
	err := encoder.Encode(devStudioJSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	adapter.writeMu.Unlock()
	if err != nil {
		adapter.mu.Lock()
		delete(adapter.pending, id)
		adapter.mu.Unlock()
		return nil, fmt.Errorf("codex_write: %w", err)
	}

	select {
	case result := <-response:
		if result.Error != nil {
			return nil, errors.New("codex_request_failed")
		}
		return result.Result, nil
	case <-time.After(devStudioCodexTimeout):
		adapter.mu.Lock()
		delete(adapter.pending, id)
		adapter.mu.Unlock()
		return nil, errors.New("codex_request_timeout")
	}
}

func (adapter *devStudioCodexAdapter) notify(method string, params any) error {
	adapter.mu.Lock()
	if adapter.closed || adapter.encoder == nil {
		adapter.mu.Unlock()
		return errors.New("session_closed")
	}
	encoder := adapter.encoder
	adapter.mu.Unlock()
	adapter.writeMu.Lock()
	defer adapter.writeMu.Unlock()
	return encoder.Encode(devStudioJSONRPCNotification{JSONRPC: "2.0", Method: method, Params: params})
}

func (adapter *devStudioCodexAdapter) readCodexOutput(output io.ReadCloser) {
	defer output.Close()
	decoder := json.NewDecoder(bufio.NewReader(output))
	for {
		var envelope devStudioJSONRPCEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			adapter.handleCodexExit()
			return
		}
		if envelope.ID != nil {
			adapter.mu.Lock()
			response := adapter.pending[*envelope.ID]
			delete(adapter.pending, *envelope.ID)
			adapter.mu.Unlock()
			if response != nil {
				response <- devStudioJSONRPCResponse{ID: *envelope.ID, Result: envelope.Result, Error: envelope.Error}
			}
			continue
		}
		if envelope.Method != "" {
			adapter.handleCodexNotification(envelope.Method, envelope.Params)
		}
	}
}

func (adapter *devStudioCodexAdapter) handleCodexExit() {
	adapter.mu.Lock()
	process := adapter.process
	adapter.process = nil
	adapter.encoder = nil
	adapter.threadID = ""
	closed := adapter.closed
	pending := adapter.pending
	adapter.pending = make(map[int]chan devStudioJSONRPCResponse)
	adapter.mu.Unlock()

	for _, response := range pending {
		response <- devStudioJSONRPCResponse{
			Error: &struct {
				Message string `json:"message"`
			}{Message: "codex_unavailable"},
		}
	}
	if process != nil {
		process.Stop()
	}

	changed, active, err := adapter.endActiveTurn()
	if !active || closed {
		return
	}
	if err != nil {
		adapter.emitError("protected_file_restore_failed", "Protected instruction files changed and could not be restored.")
		return
	}
	if len(changed) > 0 {
		adapter.emitError("protected_files_modified", "Protected instruction files were restored: "+strings.Join(changed, ", "))
		return
	}
	adapter.emitError("codex_unavailable", "Codex stopped before the turn completed.")
}

func (adapter *devStudioCodexAdapter) handleCodexNotification(method string, params json.RawMessage) {
	switch method {
	case "item/agentMessage/delta":
		var event struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(params, &event) == nil && event.Delta != "" {
			_ = adapter.send(map[string]string{"type": "message.delta", "text": adapter.redact(event.Delta)})
		}
	case "turn/started":
		adapter.emitStatus("thinking")
	case "item/commandExecution/outputDelta":
		var event struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &event) == nil && event.ItemID != "" && event.Delta != "" {
			_ = adapter.send(map[string]string{
				"type":      "command.output",
				"commandId": event.ItemID,
				"text":      adapter.redact(event.Delta),
			})
		}
	case "item/started":
		adapter.handleCodexItem(params)
	case "item/completed":
		adapter.handleCodexItem(params)
	case "turn/completed":
		adapter.finishTurn()
	case "error":
		adapter.failTurn("codex_turn_failed", "Codex could not complete the turn.")
	}
}

func (adapter *devStudioCodexAdapter) handleCodexItem(params json.RawMessage) {
	var event struct {
		Item struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Command  string `json:"command"`
			Output   string `json:"aggregatedOutput"`
			ExitCode *int   `json:"exitCode"`
			Changes  []struct {
				Path string `json:"path"`
			} `json:"changes"`
		} `json:"item"`
	}
	if json.Unmarshal(params, &event) != nil {
		return
	}
	switch event.Item.Type {
	case "commandExecution":
		adapter.emitStatus("running_command")
		if event.Item.ID != "" {
			payload := map[string]any{
				"type":      "command.started",
				"commandId": event.Item.ID,
				"command":   adapter.redact(event.Item.Command),
			}
			if event.Item.ExitCode != nil {
				payload["type"] = "command.completed"
				payload["exitCode"] = *event.Item.ExitCode
				if event.Item.Output != "" {
					payload["output"] = adapter.redact(event.Item.Output)
				}
			}
			_ = adapter.send(payload)
		}
	case "fileChange":
		adapter.emitStatus("editing")
		paths := adapter.relativePaths(event.Item.Changes)
		if len(paths) > 0 {
			_ = adapter.send(map[string]any{"type": "files.changed", "paths": paths})
		}
	case "collabAgentToolCall":
		adapter.emitStatus("delegating")
	case "reasoning":
		adapter.emitStatus("thinking")
	}
}

func (adapter *devStudioCodexAdapter) relativePaths(changes []struct {
	Path string `json:"path"`
}) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if relative, ok := devStudioRelativePath(adapter.workspace.instanceRoot, change.Path); ok {
			paths = append(paths, relative)
		}
	}
	sort.Strings(paths)
	return paths
}

func (adapter *devStudioCodexAdapter) cancelTurn() error {
	adapter.turnMu.Lock()
	defer adapter.turnMu.Unlock()
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return errors.New("session_closed")
	}
	if !adapter.active || adapter.threadID == "" || adapter.turnID == "" {
		adapter.mu.Unlock()
		return errors.New("no_active_turn")
	}
	threadID, turnID := adapter.threadID, adapter.turnID
	adapter.mu.Unlock()
	if _, err := adapter.call("turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID}); err != nil {
		return err
	}
	return nil
}

func (adapter *devStudioCodexAdapter) finishTurn() {
	changed, active, err := adapter.endActiveTurn()
	if !active {
		return
	}
	if err != nil {
		adapter.emitError("protected_file_restore_failed", "Protected instruction files changed and could not be restored.")
		return
	}
	if len(changed) > 0 {
		adapter.emitError("protected_files_modified", "Protected instruction files were restored: "+strings.Join(changed, ", "))
		return
	}
	adapter.emitStatus("complete")
	_ = adapter.send(map[string]any{"type": "turn.completed", "success": true})
}

func (adapter *devStudioCodexAdapter) failTurn(code, message string) {
	changed, active, err := adapter.endActiveTurn()
	if !active {
		return
	}
	if err != nil {
		adapter.emitError("protected_file_restore_failed", "Protected instruction files changed and could not be restored.")
		return
	}
	if len(changed) > 0 {
		adapter.emitError("protected_files_modified", "Protected instruction files were restored: "+strings.Join(changed, ", "))
		return
	}
	adapter.emitError(code, message)
}

func (adapter *devStudioCodexAdapter) endActiveTurn() ([]string, bool, error) {
	adapter.mu.Lock()
	if !adapter.active {
		adapter.mu.Unlock()
		return nil, false, nil
	}
	adapter.active = false
	adapter.turnID = ""
	before := adapter.protected
	adapter.protected = nil
	adapter.mu.Unlock()
	changed, err := restoreChangedDevStudioProtectedFiles(adapter.workspace, before)
	return changed, true, err
}

func (adapter *devStudioCodexAdapter) close() {
	_, _, _ = adapter.endActiveTurn()
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return
	}
	adapter.closed = true
	adapter.active = false
	process := adapter.process
	adapter.process = nil
	adapter.encoder = nil
	adapter.threadID = ""
	adapter.turnID = ""
	adapter.deployKey = ""
	pending := adapter.pending
	adapter.pending = make(map[int]chan devStudioJSONRPCResponse)
	adapter.mu.Unlock()
	for _, response := range pending {
		response <- devStudioJSONRPCResponse{
			Error: &struct {
				Message string `json:"message"`
			}{Message: "session_closed"},
		}
	}
	if process != nil {
		process.Stop()
	}
}

func (adapter *devStudioCodexAdapter) emitStatus(status string) {
	_ = adapter.send(map[string]string{"type": "status.changed", "status": status})
}

func (adapter *devStudioCodexAdapter) emitError(code, message string) {
	adapter.emitStatus("error")
	_ = adapter.send(map[string]string{"type": "error", "code": code, "message": message})
}

func (adapter *devStudioCodexAdapter) redact(value string) string {
	adapter.mu.Lock()
	deployKey := adapter.deployKey
	adapter.mu.Unlock()
	if deployKey == "" {
		return value
	}
	return strings.ReplaceAll(value, deployKey, "[redacted]")
}

func devStudioTurnErrorCode(err error) string {
	switch {
	case strings.Contains(err.Error(), "turn_active"):
		return "turn_already_active"
	case strings.Contains(err.Error(), "no_active_turn"):
		return "no_active_turn"
	case strings.Contains(err.Error(), "session_closed"):
		return "session_closed"
	default:
		return "codex_unavailable"
	}
}

func devStudioTurnErrorMessage(err error) string {
	switch devStudioTurnErrorCode(err) {
	case "turn_already_active":
		return "A DevStudio turn is already active."
	case "no_active_turn":
		return "There is no active DevStudio turn to cancel."
	case "session_closed":
		return "The DevStudio session is closed."
	default:
		return "Codex could not be started for this DevStudio turn."
	}
}

func devStudioCodexInstructions(instanceID, instanceRoot string) string {
	return fmt.Sprintf(`You are Codex operating as the intelligence orchestrator for a Simple Platform instance.

Current Simple instance:
%s

Current instance workspace root:
%s

You are working at the instance level. Do not assume that only one app is relevant.

Your job is to determine:
- what the user is asking for
- which existing app or apps are relevant
- what repository instructions and skills apply
- whether changes are required
- which validation steps are appropriate
- whether deployment is appropriate

Before making changes:

1. Inspect the current instance workspace.
2. Identify relevant app directories.
3. Read the existing agent instruction files and skill/workflow files for every app you intend to modify.
4. Follow those files as the primary source of truth.

Rules:

- Work only inside %s.
- Do not access files outside %s.
- Do not modify protected agent instruction files, agent workflow files, skill files, or platform-context files.
- Use SCL for schema and declarative configuration.
- Do not expose, print, inspect, persist, or alter environment secrets.
- SIMPLE_LOCAL_API_KEY exists only for the existing Simple CLI to authenticate with the current DevStudio instance.
- Use the existing Simple CLI and the instance-root simple.scl configuration when command execution is needed.
- Run Simple CLI commands non-interactively; pass --json when supported (for example, simple --json build <app-id>) so progress UIs do not require a TTY.
- When the request requires a new app, scaffold it from the instance workspace root with simple new app <app-id> <name>, adding --desc only when appropriate.
- Never manually create an app directory or hand-write the initial app scaffold. If the Simple CLI is unavailable or scaffolding fails, report the failure and do not create a fallback structure.
- Do not add dependencies unless existing repository instructions require them.
- Explain the apps inspected, apps changed, files changed, instructions/skills used, subagents used, commands run, validation performed, deployment decisions, and outcome.

New App Rules:

- Create a new app only when existing apps don't satisfy the request and the request explicitly requires a new app.
- Ask user permission before creating a new app unless if already told by the user.
- First think like an architect and determine the app's purpose, data schema and all platoform primitives it will use.
- Then validate the plan with the user before you start scafolding.
- When creating spaces, don't fill it with random/sample data. Spaces must be connected with live instnace data using GraphQL query and provided platform api.

Validation and deployment:

- Follow the existing instructions, workflows, and skills in every app you modify.
- Decide which validation and deployment steps are appropriate for the request.
- Run existing Simple CLI commands from the current instance workspace root. Use app paths beneath apps/, such as apps/<app-id>.
- Do not modify the existing instance-root simple.scl file.
- Use the instance-root simple.scl configuration as-is.
- Build, test, and deploy behavior must follow the repository’s existing agent workflows and skills.
- For a deployment request that does not name an environment, use the configured devstudio environment. Do not ask the user to choose dev, staging, or prod.
- Use simple --json deploy apps/<app-id> --env devstudio for a DevStudio deployment unless the user explicitly requests another configured environment.
- Do not deploy an app unless you determine it is appropriate based on the user request and the relevant repository instructions.
- Do not deploy an app that you did not modify unless existing repository instructions explicitly require it for a verified dependency reason.
- If a validation or deployment command fails:
  - do not hide the failure
  - keep source changes in place
  - explain the failure using safe non-secret output
  - continue with subsequent investigation, validation, or remediation when appropriate
- Never print, inspect, persist, or expose environment secrets.`, instanceID, instanceRoot, instanceRoot, instanceRoot)
}

func formatSCLValue(val string) string {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "$") {
		return val
	}
	if (strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)) ||
		(strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`)) {
		return val
	}
	return fmt.Sprintf("%q", val)
}

func ensureDevStudioSCLConfiguration(workspace *devStudioWorkspace, instanceHost, deployKey string) error {
	path := filepath.Join(workspace.instanceRoot, "simple.scl")
	log.Printf("[DevStudio Bridge] Writing SCL configuration to %s", path)
	contents, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to inspect workspace configuration: %w", err)
		}
		contents = []byte(fmt.Sprintf("tenant %s\n\nenv devstudio {\n  endpoint %s\n  api_key %s\n}\n", workspace.instanceID, formatSCLValue(instanceHost), formatSCLValue(deployKey)))
	} else {
		contents, err = ensureDevStudioEnvironment(contents, instanceHost, deployKey)
		if err != nil {
			return fmt.Errorf("failed to prepare workspace configuration: %w", err)
		}
	}
	if err := os.WriteFile(path, contents, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to create workspace configuration: %w", err)
	}
	return nil
}

func ensureDevStudioEnvironment(contents []byte, instanceHost, deployKey string) ([]byte, error) {
	lines := strings.Split(string(contents), "\n")
	for start, line := range lines {
		matches := devStudioEnvBlockPattern.FindStringSubmatch(line)
		if len(matches) == 0 {
			continue
		}

		end, err := devStudioEnvironmentEnd(lines, start)
		if err != nil {
			return nil, err
		}
		block := append([]string(nil), lines[start:end+1]...)
		block = updateDevStudioEnvironmentBlock(block, matches[1], instanceHost, deployKey)
		updated := append([]string(nil), lines[:start]...)
		updated = append(updated, block...)
		updated = append(updated, lines[end+1:]...)
		return []byte(strings.Join(updated, "\n")), nil
	}

	text := string(contents)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" {
		text += "\n"
	}
	text += fmt.Sprintf("env devstudio {\n  endpoint %s\n  api_key %s\n}\n", formatSCLValue(instanceHost), formatSCLValue(deployKey))
	return []byte(text), nil
}

func devStudioEnvironmentEnd(lines []string, start int) (int, error) {
	depth := 0
	for index := start; index < len(lines); index++ {
		depth += strings.Count(lines[index], "{") - strings.Count(lines[index], "}")
		if depth == 0 {
			return index, nil
		}
	}
	return 0, errors.New("simple.scl has an unterminated env devstudio block")
}

func updateDevStudioEnvironmentBlock(block []string, indent, instanceHost, deployKey string) []string {
	endpointLine := indent + "  endpoint " + formatSCLValue(instanceHost)
	apiKeyLine := indent + "  api_key " + formatSCLValue(deployKey)
	hasEndpoint := false
	hasAPIKey := false
	closingIndex := len(block) - 1
	for index := 1; index < len(block)-1; index++ {
		matches := devStudioFieldPattern.FindStringSubmatch(block[index])
		if len(matches) == 0 {
			continue
		}
		switch matches[2] {
		case "endpoint":
			block[index] = matches[1] + "endpoint " + formatSCLValue(instanceHost)
			hasEndpoint = true
		case "api_key":
			block[index] = matches[1] + "api_key " + formatSCLValue(deployKey)
			hasAPIKey = true
		}
	}
	additions := make([]string, 0, 2)
	if !hasEndpoint {
		additions = append(additions, endpointLine)
	}
	if !hasAPIKey {
		additions = append(additions, apiKeyLine)
	}
	if len(additions) > 0 {
		block = append(block[:closingIndex], append(additions, block[closingIndex:]...)...)
	}
	return block
}

func snapshotDevStudioProtectedFiles(workspace *devStudioWorkspace) (map[string]devStudioProtectedFile, error) {
	paths, err := discoverDevStudioProtectedFiles(workspace)
	if err != nil {
		return nil, err
	}
	snapshot := make(map[string]devStudioProtectedFile, len(paths))
	for _, relative := range paths {
		path := filepath.Join(workspace.instanceRoot, relative)
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to snapshot protected file: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect protected file: %w", err)
		}
		snapshot[relative] = devStudioProtectedFile{contents: contents, hash: devStudioHash(contents), mode: info.Mode().Perm()}
	}
	return snapshot, nil
}

func restoreChangedDevStudioProtectedFiles(workspace *devStudioWorkspace, snapshot map[string]devStudioProtectedFile) ([]string, error) {
	if snapshot == nil {
		return nil, nil
	}
	paths, err := discoverDevStudioProtectedFiles(workspace)
	if err != nil {
		return nil, err
	}
	current := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		current[path] = struct{}{}
	}
	changed := make([]string, 0)
	for relative, before := range snapshot {
		path := filepath.Join(workspace.instanceRoot, relative)
		contents, err := os.ReadFile(path)
		if err == nil && devStudioHash(contents) == before.hash {
			if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm() == before.mode {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), fsx.DirPerm); err != nil {
			return nil, fmt.Errorf("failed to restore protected file: %w", err)
		}
		if err := os.WriteFile(path, before.contents, before.mode); err != nil {
			return nil, fmt.Errorf("failed to restore protected file: %w", err)
		}
		if err := os.Chmod(path, before.mode); err != nil {
			return nil, fmt.Errorf("failed to restore protected file: %w", err)
		}
		changed = append(changed, relative)
	}
	for relative := range current {
		if _, existed := snapshot[relative]; existed {
			continue
		}
		changed = append(changed, relative)
	}
	sort.Strings(changed)
	return changed, nil
}

func discoverDevStudioProtectedFiles(workspace *devStudioWorkspace) ([]string, error) {
	roots := []string{workspace.instanceRoot}
	for _, appDir := range workspace.appDirs {
		roots = append(roots, workspace.appPath(appDir))
	}
	seen := make(map[string]struct{})
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			relative, ok := devStudioRelativePath(workspace.instanceRoot, path)
			if !ok || !devStudioProtectedRelativePath(relative) {
				return nil
			}
			seen[relative] = struct{}{}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to discover protected paths: %w", err)
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func devStudioProtectedRelativePath(relative string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for index, part := range parts {
		if part == ".agent" || part == "skills" || (part == ".simple" && index+1 < len(parts) && parts[index+1] == "context") {
			return true
		}
	}
	name := filepath.Base(relative)
	return name == "AGENT.md" || name == "AGENTS.md"
}

func devStudioHash(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func devStudioRelativePath(root, path string) (string, bool) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

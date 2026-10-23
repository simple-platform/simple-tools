package cli

import (
	"bufio"
	"bytes"
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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"simple-cli/internal/fsx"
	"simple-cli/internal/home"
)

var (
	devStudioEnvBlockPattern = regexp.MustCompile(`^(\s*)env\s+devstudio\s*\{\s*(?:#.*)?$`)
	devStudioFieldPattern    = regexp.MustCompile(`^(\s*)(endpoint|api_key)\s+.*$`)
	// devStudioShellWrapperPattern matches the shell Codex runs a command
	// through, such as /bin/zsh -c '…' or bash -lc "…".
	devStudioShellWrapperPattern = regexp.MustCompile(`(?s)^(?:\S*/)?(?:zsh|bash|sh) -l?c (.+)$`)
)

const (
	devStudioCodexExecutable = "codex"
	devStudioCodexTimeout    = 30 * time.Second
	// devStudioDiffLimit caps each file diff sent to the browser, in characters.
	devStudioDiffLimit = 20000
	// devStudioThreadFile stores the instance's Codex conversation id, under
	// ~/.simple/devstudio/<instanceId>, so a reconnect resumes it.
	devStudioThreadFile = "thread.json"
	// devStudioSummarySeparator joins the parts of a reasoning summary.
	devStudioSummarySeparator      = "\n\n"
	devStudioJSONRPCInvalidParams  = -32602
	devStudioJSONRPCMethodNotFound = -32601
)

var (
	errDevStudioSessionClosed    = errors.New("session_closed")
	errDevStudioTurnActive       = errors.New("turn_already_active")
	errDevStudioUnknownQuestion  = errors.New("unknown_question")
	errDevStudioCodexUnavailable = errors.New("codex_unavailable")
	// errDevStudioCodexRejected means Codex answered a call with an error.
	errDevStudioCodexRejected = errors.New("codex_request_failed")
	errDevStudioCodexTimeout  = errors.New("codex_request_timeout")
)

// devStudioCodexOptOutNotifications are Codex notifications DevStudio never
// shows. Opting out keeps them off the pipe; raw reasoning text is among
// them, so it can never reach the browser.
var devStudioCodexOptOutNotifications = []string{
	"account/rateLimits/updated",
	"account/updated",
	"app/list/updated",
	"item/fileChange/outputDelta",
	"item/fileChange/patchUpdated",
	"item/plan/delta",
	"item/reasoning/textDelta",
	"mcpServer/startupStatus/updated",
	"rawResponseItem/completed",
	"remoteControl/status/changed",
	"skills/changed",
	"thread/settings/updated",
	"thread/started",
	"thread/status/changed",
	"thread/tokenUsage/updated",
	"turn/diff/updated",
}

// devStudioCodexThreadConfig is the Codex configuration every DevStudio
// conversation runs with, on start and on resume.
func devStudioCodexThreadConfig() map[string]any {
	return map[string]any{
		"web_search": "live",
		// Lets the agent ask the person a question inside a normal turn, so
		// its skills decide when to ask.
		"features.default_mode_request_user_input": true,
		"suppress_unstable_features_warning":       true,
		"model_reasoning_summary":                  "concise",
		// Codex has no metadata for some deployed models and then sends no
		// reasoning summary unless told the model supports one.
		"model_supports_reasoning_summaries": true,
	}
}

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

// devStudioCodexAdapter drives one Codex app-server for one browser session
// and translates what Codex reports into bridge protocol events. It holds
// mechanism only: what the agent does, and when it asks the person, belongs
// in the agent's skills.
type devStudioCodexAdapter struct {
	mu         sync.Mutex
	writeMu    sync.Mutex
	storeMu    sync.Mutex
	starter    devStudioCodexStarter
	instanceID string
	workspace  *devStudioWorkspace
	deployKey  string
	send       func(any) error
	connection *devStudioCodexConnection
	nextID     int
	threadID   string
	// busy is claimed in the browser read loop before any Codex call, so a
	// second turn or a conversation switch is refused at once.
	busy      bool
	turn      *devStudioTurn
	status    string
	questions map[string]devStudioOpenQuestion
	phases    map[string]string
	commands  map[string]devStudioStartedCommand
	closed    bool
}

// devStudioCodexConnection is one running Codex process. Replies to the
// bridge's own calls are matched in pending by their id.
type devStudioCodexConnection struct {
	process devStudioCodexProcess
	encoder *json.Encoder
	pending map[string]chan devStudioJSONRPCResponse
}

// devStudioTurn is the turn the browser started. Codex names it only once it
// has accepted it, so a cancel that arrives earlier is remembered and sent
// as soon as the id is known.
type devStudioTurn struct {
	id              string
	startedAt       time.Time
	announced       bool
	cancelRequested bool
	interruptSent   bool
	protected       map[string]devStudioProtectedFile
}

type devStudioTurnError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type devStudioProtectedFile struct {
	contents []byte
	hash     string
	mode     os.FileMode
}

type devStudioOpenQuestion struct {
	id         json.RawMessage
	connection *devStudioCodexConnection
}

type devStudioStartedCommand struct {
	command string
	actions []map[string]any
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

// devStudioJSONRPCReply answers a request Codex sent. The id is the raw JSON
// Codex used, so 0 and string ids go back exactly as received.
type devStudioJSONRPCReply struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      json.RawMessage        `json:"id"`
	Result  any                    `json:"result,omitempty"`
	Error   *devStudioJSONRPCError `json:"error,omitempty"`
}

type devStudioJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type devStudioJSONRPCResponse struct {
	Result json.RawMessage
	Err    error
}

type devStudioJSONRPCEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type devStudioCodexItem struct {
	Type    string   `json:"type"`
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Phase   *string  `json:"phase"`
	Summary []string `json:"summary"`
	// Content is read only for the person's own messages; a reasoning
	// item's content is raw reasoning, which is never sent.
	Content          json.RawMessage                `json:"content"`
	Command          string                         `json:"command"`
	Cwd              string                         `json:"cwd"`
	Status           string                         `json:"status"`
	CommandActions   []devStudioCodexCommandAction  `json:"commandActions"`
	AggregatedOutput *string                        `json:"aggregatedOutput"`
	ExitCode         *int                           `json:"exitCode"`
	DurationMs       *int64                         `json:"durationMs"`
	Changes          []devStudioCodexFileChange     `json:"changes"`
	Query            string                         `json:"query"`
	Action           *devStudioCodexWebSearchAction `json:"action"`
}

type devStudioCodexCommandAction struct {
	Type    string  `json:"type"`
	Command string  `json:"command"`
	Name    string  `json:"name"`
	Path    *string `json:"path"`
	Query   *string `json:"query"`
}

type devStudioCodexFileChange struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
	Kind struct {
		Type     string  `json:"type"`
		MovePath *string `json:"move_path"`
	} `json:"kind"`
}

type devStudioCodexWebSearchAction struct {
	Type    string  `json:"type"`
	Query   *string `json:"query"`
	URL     *string `json:"url"`
	Pattern *string `json:"pattern"`
}

type devStudioCodexTurn struct {
	ID         string               `json:"id"`
	Status     string               `json:"status"`
	DurationMs *int64               `json:"durationMs"`
	Error      *devStudioCodexError `json:"error"`
	// Items stay raw so one item the bridge cannot read does not drop the turn.
	Items []json.RawMessage `json:"items"`
}

type devStudioCodexError struct {
	Message        string          `json:"message"`
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
}

// devStudioThreadRecord is what ~/.simple/devstudio/<instanceId>/thread.json
// holds.
type devStudioThreadRecord struct {
	ThreadID string `json:"threadId"`
	// InstanceRoot is the canonical instance folder the conversation belongs
	// to, so two folders for one instance never share a conversation.
	InstanceRoot string `json:"instanceRoot"`
	// Plan is the conversation's latest plan. Codex does not keep plans in
	// its history, so the bridge keeps it for the replay.
	Plan *devStudioPlan `json:"plan,omitempty"`
}

type devStudioPlan struct {
	Explanation *string             `json:"explanation"`
	Steps       []devStudioPlanStep `json:"steps"`
}

type devStudioPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
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
		questions:  make(map[string]devStudioOpenQuestion),
		phases:     make(map[string]string),
		commands:   make(map[string]devStudioStartedCommand),
	}
}

func (session *devStudioSession) handleBrowserMessage(payload []byte) *devStudioProtocolError {
	adapter := session.codexAdapter()
	if adapter == nil {
		return newDevStudioProtocolError("session_closed", "The DevStudio session is closed.")
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

	// Every case returns at once: anything that waits on Codex runs outside
	// the read loop, so a cancel is always read.
	switch messageType {
	case "turn.start":
		if len(message) != 2 {
			return devStudioInvalidMessageError()
		}
		var text string
		if rawText, ok := message["text"]; !ok || json.Unmarshal(rawText, &text) != nil || strings.TrimSpace(text) == "" {
			return newDevStudioProtocolError("invalid_turn", "A non-empty chat request is required.")
		}
		return devStudioAdapterError(adapter.startTurn(text))
	case "turn.cancel":
		if len(message) != 1 {
			return devStudioInvalidMessageError()
		}
		return devStudioAdapterError(adapter.cancelTurn())
	case "question.answer":
		if len(message) != 3 {
			return devStudioInvalidMessageError()
		}
		var requestID string
		var answers map[string][]string
		if json.Unmarshal(message["requestId"], &requestID) != nil || requestID == "" ||
			json.Unmarshal(message["answers"], &answers) != nil || answers == nil {
			return devStudioInvalidMessageError()
		}
		return devStudioAdapterError(adapter.answerQuestion(requestID, answers))
	case "conversation.new":
		if len(message) != 1 {
			return devStudioInvalidMessageError()
		}
		return devStudioAdapterError(adapter.newConversation())
	default:
		return newDevStudioProtocolError("unsupported_message_type", "This message type is not supported.")
	}
}

func newDevStudioProtocolError(code, message string) *devStudioProtocolError {
	return &devStudioProtocolError{Type: "error", Code: code, Message: message}
}

func devStudioInvalidMessageError() *devStudioProtocolError {
	return newDevStudioProtocolError("invalid_message", "The bridge received an invalid message.")
}

func devStudioAdapterError(err error) *devStudioProtocolError {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errDevStudioTurnActive):
		return newDevStudioProtocolError("turn_already_active", "A DevStudio turn is already active.")
	case errors.Is(err, errDevStudioUnknownQuestion):
		return newDevStudioProtocolError("unknown_question", "This question is no longer open.")
	case errors.Is(err, errDevStudioSessionClosed):
		return newDevStudioProtocolError("session_closed", "The DevStudio session is closed.")
	default:
		return newDevStudioProtocolError("codex_unavailable", "Codex is not available for this DevStudio session.")
	}
}

// connect starts Codex and opens this instance's conversation, resuming the
// stored one when there is one. The session calls it before session.ready,
// because session.ready carries the conversation id.
func (adapter *devStudioCodexAdapter) connect() (string, bool, error) {
	resumed, err := adapter.ensureCodex()
	if err != nil {
		return "", false, err
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return adapter.threadID, resumed, nil
}

// ensureCodex starts Codex when no process is running: at connect, and again
// on the next turn after Codex stopped. It reports whether a stored
// conversation was resumed.
func (adapter *devStudioCodexAdapter) ensureCodex() (bool, error) {
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return false, errDevStudioSessionClosed
	}
	if adapter.connection != nil {
		adapter.mu.Unlock()
		return false, nil
	}
	deployKey := adapter.deployKey
	adapter.mu.Unlock()

	process, err := adapter.starter.Start(devStudioCodexStartConfig{
		WorkspaceRoot: adapter.workspace.instanceRoot,
		InstanceID:    adapter.instanceID,
		DeployKey:     deployKey,
	})
	if err != nil {
		return false, fmt.Errorf("failed to start Codex: %w", err)
	}
	connection, err := adapter.attach(process)
	if err != nil {
		return false, err
	}
	if _, err := adapter.call("initialize", map[string]any{
		"clientInfo": map[string]string{"name": "simple-devstudio", "version": "2"},
		"capabilities": map[string]any{
			"experimentalApi":           true,
			"requestAttestation":        false,
			"optOutNotificationMethods": devStudioCodexOptOutNotifications,
		},
	}); err != nil {
		adapter.dropConnection(connection)
		return false, fmt.Errorf("failed to initialize Codex: %w", err)
	}
	if err := adapter.notify("initialized", map[string]any{}); err != nil {
		adapter.dropConnection(connection)
		return false, fmt.Errorf("failed to initialize Codex: %w", err)
	}
	resumed, err := adapter.openThread()
	if err != nil {
		adapter.dropConnection(connection)
		return false, err
	}
	return resumed, nil
}

// attach makes process the adapter's Codex connection and starts reading it.
func (adapter *devStudioCodexAdapter) attach(process devStudioCodexProcess) (*devStudioCodexConnection, error) {
	connection := &devStudioCodexConnection{
		process: process,
		encoder: json.NewEncoder(process.Input()),
		pending: make(map[string]chan devStudioJSONRPCResponse),
	}
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		process.Stop()
		return nil, errDevStudioSessionClosed
	}
	adapter.connection = connection
	adapter.mu.Unlock()
	go adapter.readCodexOutput(connection)
	return connection, nil
}

// openThread resumes the conversation stored for this instance, or starts a
// new one when there is none or Codex no longer has it.
func (adapter *devStudioCodexAdapter) openThread() (bool, error) {
	adapter.storeMu.Lock()
	stored, err := readDevStudioThreadID(adapter.workspace)
	adapter.storeMu.Unlock()
	if err != nil {
		log.Printf("[DevStudio Bridge] Starting a new conversation: %s", devStudioLogValue(err))
	}
	if stored != "" {
		params := adapter.threadParams()
		params["threadId"] = stored
		result, err := adapter.call("thread/resume", params)
		switch {
		case err == nil:
			threadID, err := devStudioThreadIDFrom(result)
			if err != nil {
				return false, err
			}
			adapter.setThreadID(threadID)
			return true, nil
		case !errors.Is(err, errDevStudioCodexRejected):
			return false, fmt.Errorf("failed to resume the Codex conversation: %w", err)
		}
		// Codex answered that it no longer has this conversation, for
		// example because its history was deleted.
		log.Printf("[DevStudio Bridge] Codex could not resume the stored conversation; starting a new one")
	}
	return false, adapter.startThread()
}

func (adapter *devStudioCodexAdapter) startThread() error {
	params := adapter.threadParams()
	params["ephemeral"] = false
	// Codex 0.144.1 cannot read back a conversation stored in its newer
	// paginated form (thread/read answers "paginated_threads is not
	// supported yet"), so the replay needs the legacy form.
	params["historyMode"] = "legacy"
	result, err := adapter.call("thread/start", params)
	if err != nil {
		return fmt.Errorf("failed to start a Codex conversation: %w", err)
	}
	threadID, err := devStudioThreadIDFrom(result)
	if err != nil {
		return err
	}
	// A new conversation id replaces the stored record, so the previous
	// conversation's plan goes with it.
	adapter.storeMu.Lock()
	err = writeDevStudioThreadID(adapter.workspace, threadID)
	adapter.storeMu.Unlock()
	if err != nil {
		return err
	}
	adapter.setThreadID(threadID)
	return nil
}

func (adapter *devStudioCodexAdapter) threadParams() map[string]any {
	return map[string]any{
		"cwd":                   adapter.workspace.instanceRoot,
		"approvalPolicy":        "never",
		"sandbox":               "workspace-write",
		"developerInstructions": devStudioCodexInstructions(adapter.instanceID, adapter.workspace.instanceRoot),
		"config":                devStudioCodexThreadConfig(),
	}
}

func (adapter *devStudioCodexAdapter) setThreadID(threadID string) {
	adapter.mu.Lock()
	adapter.threadID = threadID
	adapter.mu.Unlock()
}

func devStudioThreadIDFrom(result json.RawMessage) (string, error) {
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.Thread.ID == "" {
		return "", errors.New("codex returned a conversation without an id")
	}
	return response.Thread.ID, nil
}

// readHistory reads the resumed conversation's turns, to replay them.
func (adapter *devStudioCodexAdapter) readHistory() ([]devStudioCodexTurn, error) {
	adapter.mu.Lock()
	threadID := adapter.threadID
	adapter.mu.Unlock()
	result, err := adapter.call("thread/read", map[string]any{"threadId": threadID, "includeTurns": true})
	if err != nil {
		return nil, fmt.Errorf("failed to read the Codex conversation: %w", err)
	}
	var response struct {
		Thread struct {
			Turns []devStudioCodexTurn `json:"turns"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to read the Codex conversation: %w", err)
	}
	return response.Thread.Turns, nil
}

// replay sends earlier turns as completed events marked replay, so the
// browser can rebuild its transcript after a reconnect. Codex gives stored
// items new ids (item-1, item-2, …) that may repeat across turns, so each
// replayed id is prefixed with its turn id to keep it unique.
func (adapter *devStudioCodexAdapter) replay(turns []devStudioCodexTurn) {
	for _, turn := range turns {
		for _, raw := range turn.Items {
			var item devStudioCodexItem
			if json.Unmarshal(raw, &item) == nil {
				item.ID = turn.ID + ":" + item.ID
				adapter.emitItemCompleted(item, true)
			}
		}
		event := adapter.turnCompletedEvent(turn.ID, devStudioTurnStatus(turn.Status), turn.DurationMs, adapter.codexTurnError(turn.Error))
		event["replay"] = true
		adapter.emit(event)
	}
	adapter.replayPlan()
}

// replayPlan sends the conversation's latest plan, kept by the bridge, once
// at the end of the replay.
func (adapter *devStudioCodexAdapter) replayPlan() {
	adapter.mu.Lock()
	threadID := adapter.threadID
	adapter.mu.Unlock()
	adapter.storeMu.Lock()
	record, err := readDevStudioThreadRecord(adapter.workspace)
	adapter.storeMu.Unlock()
	if err != nil || record.ThreadID != threadID || record.Plan == nil {
		return
	}
	event := devStudioPlanEvent(*record.Plan)
	event["replay"] = true
	adapter.emit(event)
}

// savePlan keeps the latest plan beside the conversation id for the replay.
func (adapter *devStudioCodexAdapter) savePlan(plan devStudioPlan) {
	adapter.mu.Lock()
	threadID := adapter.threadID
	adapter.mu.Unlock()
	adapter.storeMu.Lock()
	defer adapter.storeMu.Unlock()
	record, err := readDevStudioThreadRecord(adapter.workspace)
	if err == nil && record.ThreadID == threadID {
		record.Plan = &plan
		err = writeDevStudioThreadRecord(adapter.workspace, record)
	}
	if err != nil {
		log.Printf("[DevStudio Bridge] The plan could not be kept for the replay: %s", devStudioLogValue(err))
	}
}

func devStudioPlanEvent(plan devStudioPlan) map[string]any {
	steps := plan.Steps
	if steps == nil {
		steps = []devStudioPlanStep{}
	}
	return map[string]any{"type": "plan.updated", "explanation": plan.Explanation, "steps": steps}
}

func (adapter *devStudioCodexAdapter) startTurn(text string) error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return errDevStudioSessionClosed
	}
	if adapter.busy {
		return errDevStudioTurnActive
	}
	adapter.busy = true
	turn := &devStudioTurn{startedAt: time.Now()}
	adapter.turn = turn
	go adapter.runTurn(turn, text)
	return nil
}

func (adapter *devStudioCodexAdapter) runTurn(turn *devStudioTurn, text string) {
	if _, err := adapter.ensureCodex(); err != nil {
		log.Printf("[DevStudio Bridge] Codex could not be started for a turn: %s", devStudioLogValue(err))
		adapter.finishTurn(turn, "failed", nil, &devStudioTurnError{Code: "codex_unavailable", Message: "Codex could not be started for this turn."}, true)
		return
	}
	protected, err := snapshotDevStudioProtectedFiles(adapter.workspace)
	if err != nil {
		adapter.finishTurn(turn, "failed", nil, &devStudioTurnError{Code: "protected_snapshot_failed", Message: "Protected instruction files could not be checked before the turn."}, true)
		return
	}
	adapter.mu.Lock()
	if adapter.turn != turn {
		adapter.mu.Unlock()
		return
	}
	turn.protected = protected
	cancelled := turn.cancelRequested
	threadID := adapter.threadID
	adapter.mu.Unlock()
	if cancelled {
		adapter.finishTurn(turn, "interrupted", nil, nil, false)
		return
	}

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
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err == nil && (json.Unmarshal(result, &response) != nil || response.Turn.ID == "") {
		err = errors.New("codex returned a turn without an id")
	}
	if err != nil {
		log.Printf("[DevStudio Bridge] Codex could not start a turn: %s", devStudioLogValue(err))
		adapter.finishTurn(turn, "failed", nil, &devStudioTurnError{Code: "codex_unavailable", Message: "Codex could not start this turn."}, true)
		return
	}
	adapter.setTurnID(turn, response.Turn.ID)
}

// setTurnID records the id Codex gave the turn, from whichever comes first:
// the turn/start reply or the turn/started notification. A cancel that came
// before the id is sent now.
func (adapter *devStudioCodexAdapter) setTurnID(turn *devStudioTurn, turnID string) {
	adapter.mu.Lock()
	if adapter.turn != turn || turnID == "" {
		adapter.mu.Unlock()
		return
	}
	if turn.id == "" {
		turn.id = turnID
	}
	interrupt := turn.cancelRequested && !turn.interruptSent
	turn.interruptSent = turn.interruptSent || interrupt
	threadID, id := adapter.threadID, turn.id
	adapter.mu.Unlock()
	if interrupt {
		go adapter.interrupt(threadID, id)
	}
}

func (adapter *devStudioCodexAdapter) cancelTurn() error {
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return errDevStudioSessionClosed
	}
	turn := adapter.turn
	if turn == nil {
		// Nothing is running: the turn ended before the cancel arrived.
		adapter.mu.Unlock()
		return nil
	}
	turn.cancelRequested = true
	interrupt := turn.id != "" && !turn.interruptSent
	turn.interruptSent = turn.interruptSent || interrupt
	threadID, turnID := adapter.threadID, turn.id
	adapter.mu.Unlock()
	if interrupt {
		go adapter.interrupt(threadID, turnID)
	}
	return nil
}

// interrupt asks Codex to stop the turn. Codex then ends it with
// turn/completed, status interrupted.
func (adapter *devStudioCodexAdapter) interrupt(threadID, turnID string) {
	if _, err := adapter.call("turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID}); err != nil {
		log.Printf("[DevStudio Bridge] Codex did not accept the cancel: %s", devStudioLogValue(err))
	}
}

func (adapter *devStudioCodexAdapter) newConversation() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return errDevStudioSessionClosed
	}
	if adapter.busy {
		return errDevStudioTurnActive
	}
	adapter.busy = true
	go adapter.runNewConversation()
	return nil
}

func (adapter *devStudioCodexAdapter) runNewConversation() {
	_, err := adapter.ensureCodex()
	if err == nil {
		err = adapter.startThread()
	}
	adapter.mu.Lock()
	adapter.busy = false
	threadID := adapter.threadID
	closed := adapter.closed
	adapter.mu.Unlock()
	if closed {
		return
	}
	if err != nil {
		log.Printf("[DevStudio Bridge] Codex could not start a new conversation: %s", devStudioLogValue(err))
		adapter.emitError("codex_unavailable", "Codex could not start a new conversation.")
		return
	}
	adapter.emit(map[string]any{"type": "conversation.started", "threadId": threadID})
}

func (adapter *devStudioCodexAdapter) answerQuestion(requestID string, answers map[string][]string) error {
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return errDevStudioSessionClosed
	}
	question, ok := adapter.questions[requestID]
	delete(adapter.questions, requestID)
	adapter.mu.Unlock()
	if !ok {
		return errDevStudioUnknownQuestion
	}

	result := make(map[string]any, len(answers))
	for questionID, values := range answers {
		if values == nil {
			values = []string{}
		}
		result[questionID] = map[string][]string{"answers": values}
	}
	if err := adapter.reply(question.connection, question.id, map[string]any{"answers": result}, nil); err != nil {
		return fmt.Errorf("%w: %w", errDevStudioCodexUnavailable, err)
	}
	adapter.emit(map[string]any{"type": "question.resolved", "requestId": requestID})
	adapter.emitStatus("thinking")
	return nil
}

func (adapter *devStudioCodexAdapter) call(method string, params any) (json.RawMessage, error) {
	adapter.mu.Lock()
	connection := adapter.connection
	if adapter.closed {
		adapter.mu.Unlock()
		return nil, errDevStudioSessionClosed
	}
	if connection == nil {
		adapter.mu.Unlock()
		return nil, errDevStudioCodexUnavailable
	}
	adapter.nextID++
	id := adapter.nextID
	key := strconv.Itoa(id)
	response := make(chan devStudioJSONRPCResponse, 1)
	connection.pending[key] = response
	adapter.mu.Unlock()

	if err := adapter.write(connection, devStudioJSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		adapter.mu.Lock()
		delete(connection.pending, key)
		adapter.mu.Unlock()
		return nil, fmt.Errorf("failed to write to Codex: %w", err)
	}

	timer := time.NewTimer(devStudioCodexTimeout)
	defer timer.Stop()
	select {
	case result := <-response:
		return result.Result, result.Err
	case <-timer.C:
		adapter.mu.Lock()
		delete(connection.pending, key)
		adapter.mu.Unlock()
		return nil, errDevStudioCodexTimeout
	}
}

func (adapter *devStudioCodexAdapter) notify(method string, params any) error {
	adapter.mu.Lock()
	connection := adapter.connection
	adapter.mu.Unlock()
	if connection == nil {
		return errDevStudioCodexUnavailable
	}
	return adapter.write(connection, devStudioJSONRPCNotification{JSONRPC: "2.0", Method: method, Params: params})
}

func (adapter *devStudioCodexAdapter) reply(connection *devStudioCodexConnection, id json.RawMessage, result any, rpcError *devStudioJSONRPCError) error {
	return adapter.write(connection, devStudioJSONRPCReply{JSONRPC: "2.0", ID: id, Result: result, Error: rpcError})
}

func (adapter *devStudioCodexAdapter) write(connection *devStudioCodexConnection, message any) error {
	adapter.writeMu.Lock()
	defer adapter.writeMu.Unlock()
	return connection.encoder.Encode(message)
}

// readCodexOutput reads one Codex process until it stops. A message with a
// method is from Codex: with an id it is a request that must be answered,
// without one a notification. A message with only an id answers a call.
func (adapter *devStudioCodexAdapter) readCodexOutput(connection *devStudioCodexConnection) {
	output := connection.process.Output()
	defer func() { _ = output.Close() }()
	decoder := json.NewDecoder(bufio.NewReader(output))
	for {
		var envelope devStudioJSONRPCEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			adapter.handleCodexExit(connection)
			return
		}
		hasID := len(envelope.ID) > 0 && string(envelope.ID) != "null"
		switch {
		case envelope.Method != "" && hasID:
			adapter.handleCodexRequest(connection, envelope.ID, envelope.Method, envelope.Params)
		case envelope.Method != "":
			adapter.handleCodexNotification(envelope.Method, envelope.Params)
		case hasID:
			adapter.resolveCall(connection, envelope)
		}
	}
}

func (adapter *devStudioCodexAdapter) resolveCall(connection *devStudioCodexConnection, envelope devStudioJSONRPCEnvelope) {
	key := devStudioRequestKey(envelope.ID)
	adapter.mu.Lock()
	response := connection.pending[key]
	delete(connection.pending, key)
	adapter.mu.Unlock()
	if response == nil {
		return
	}
	if envelope.Error != nil {
		response <- devStudioJSONRPCResponse{Err: fmt.Errorf("%w: %s", errDevStudioCodexRejected, adapter.redact(envelope.Error.Message))}
		return
	}
	response <- devStudioJSONRPCResponse{Result: envelope.Result}
}

// handleCodexRequest answers every request Codex sends, or Codex waits on it
// forever. Questions go to the browser; approvals are declined, because the
// approval policy is never; anything else is refused.
func (adapter *devStudioCodexAdapter) handleCodexRequest(connection *devStudioCodexConnection, id json.RawMessage, method string, params json.RawMessage) {
	var err error
	switch method {
	case "item/tool/requestUserInput":
		adapter.askQuestion(connection, id, params)
		return
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		err = adapter.reply(connection, id, map[string]string{"decision": "decline"}, nil)
	case "execCommandApproval", "applyPatchApproval":
		err = adapter.reply(connection, id, map[string]string{"decision": "denied"}, nil)
	case "item/permissions/requestApproval":
		err = adapter.reply(connection, id, map[string]any{"permissions": map[string]any{}, "scope": "turn"}, nil)
	default:
		err = adapter.reply(connection, id, nil, &devStudioJSONRPCError{Code: devStudioJSONRPCMethodNotFound, Message: "DevStudio does not handle " + method + "."})
	}
	if err != nil {
		log.Printf("[DevStudio Bridge] Failed to answer the Codex request %s: %s", devStudioLogValue(method), devStudioLogValue(err))
	}
}

func (adapter *devStudioCodexAdapter) askQuestion(connection *devStudioCodexConnection, id json.RawMessage, params json.RawMessage) {
	var request struct {
		ItemID    string `json:"itemId"`
		Questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			IsOther  bool   `json:"isOther"`
			IsSecret bool   `json:"isSecret"`
			Options  *[]struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(params, &request) != nil || len(request.Questions) == 0 {
		if err := adapter.reply(connection, id, nil, &devStudioJSONRPCError{Code: devStudioJSONRPCInvalidParams, Message: "DevStudio could not read this question."}); err != nil {
			log.Printf("[DevStudio Bridge] Failed to refuse a Codex question: %s", devStudioLogValue(err))
		}
		return
	}

	questions := make([]map[string]any, 0, len(request.Questions))
	for _, question := range request.Questions {
		var options any
		if question.Options != nil {
			list := make([]map[string]string, 0, len(*question.Options))
			for _, option := range *question.Options {
				list = append(list, map[string]string{"label": adapter.redact(option.Label), "description": adapter.redact(option.Description)})
			}
			options = list
		}
		questions = append(questions, map[string]any{
			"id":       question.ID,
			"header":   adapter.redact(question.Header),
			"question": adapter.redact(question.Question),
			"isOther":  question.IsOther,
			"isSecret": question.IsSecret,
			"options":  options,
		})
	}
	requestID := devStudioRequestKey(id)
	adapter.mu.Lock()
	adapter.questions[requestID] = devStudioOpenQuestion{id: append(json.RawMessage(nil), id...), connection: connection}
	adapter.mu.Unlock()
	adapter.emitStatus("waiting_for_answer")
	adapter.emit(map[string]any{"type": "question.asked", "requestId": requestID, "itemId": request.ItemID, "questions": questions})
}

// devStudioRequestKey is the text of a JSON-RPC id, so the number 0 and the
// string "0" stay different keys.
func devStudioRequestKey(id json.RawMessage) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, id); err != nil {
		return string(id)
	}
	return compact.String()
}

func (adapter *devStudioCodexAdapter) handleCodexNotification(method string, params json.RawMessage) {
	if !adapter.isCurrentThread(params) {
		return
	}
	switch method {
	case "turn/started":
		adapter.handleTurnStarted(params)
	case "turn/completed":
		adapter.handleTurnCompleted(params)
	case "turn/plan/updated":
		adapter.handlePlanUpdated(params)
	case "item/started", "item/completed":
		var event struct {
			Item devStudioCodexItem `json:"item"`
		}
		if json.Unmarshal(params, &event) != nil || event.Item.ID == "" {
			return
		}
		if method == "item/started" {
			adapter.emitItemStarted(event.Item)
		} else {
			adapter.emitItemCompleted(event.Item, false)
		}
	case "item/agentMessage/delta":
		var event struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &event) != nil || event.Delta == "" {
			return
		}
		adapter.mu.Lock()
		phase, ok := adapter.phases[event.ItemID]
		adapter.mu.Unlock()
		if !ok {
			phase = "final_answer"
		}
		adapter.emit(map[string]any{"type": "message.delta", "itemId": event.ItemID, "phase": phase, "text": adapter.redact(event.Delta)})
	case "item/reasoning/summaryPartAdded":
		var event struct {
			ItemID       string `json:"itemId"`
			SummaryIndex int    `json:"summaryIndex"`
		}
		// Parts after the first are separated the way reasoning.completed
		// joins them, so the deltas add up to the completed text.
		if json.Unmarshal(params, &event) == nil && event.SummaryIndex > 0 {
			adapter.emit(map[string]any{"type": "reasoning.delta", "itemId": event.ItemID, "text": devStudioSummarySeparator})
		}
	case "item/reasoning/summaryTextDelta":
		var event struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &event) == nil && event.Delta != "" {
			adapter.emit(map[string]any{"type": "reasoning.delta", "itemId": event.ItemID, "text": adapter.redact(event.Delta)})
		}
	case "item/commandExecution/outputDelta":
		var event struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &event) == nil && event.ItemID != "" && event.Delta != "" {
			adapter.emit(map[string]any{"type": "command.output", "commandId": event.ItemID, "text": adapter.redact(event.Delta)})
		}
	case "serverRequest/resolved":
		adapter.handleRequestResolved(params)
	case "error":
		adapter.handleCodexError(params)
	}
}

// isCurrentThread drops notifications about other Codex conversations, such
// as a helper agent's, so their turn ending does not end the person's turn.
func (adapter *devStudioCodexAdapter) isCurrentThread(params json.RawMessage) bool {
	var event struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(params, &event) != nil || event.ThreadID == "" {
		return true
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return event.ThreadID == adapter.threadID
}

func (adapter *devStudioCodexAdapter) handleTurnStarted(params json.RawMessage) {
	var event struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &event) != nil || event.Turn.ID == "" {
		return
	}
	adapter.mu.Lock()
	turn := adapter.turn
	if turn == nil || turn.announced || (turn.id != "" && turn.id != event.Turn.ID) {
		adapter.mu.Unlock()
		return
	}
	turn.announced = true
	adapter.mu.Unlock()
	adapter.setTurnID(turn, event.Turn.ID)
	adapter.emit(map[string]any{"type": "turn.started", "turnId": event.Turn.ID})
	adapter.emitStatus("thinking")
}

func (adapter *devStudioCodexAdapter) handleTurnCompleted(params json.RawMessage) {
	var event struct {
		Turn devStudioCodexTurn `json:"turn"`
	}
	if json.Unmarshal(params, &event) != nil {
		return
	}
	adapter.mu.Lock()
	turn := adapter.turn
	if turn == nil || (turn.id != "" && turn.id != event.Turn.ID) {
		adapter.mu.Unlock()
		return
	}
	adapter.mu.Unlock()
	// Codex already sent its own error notification for a failed turn.
	adapter.finishTurn(turn, devStudioTurnStatus(event.Turn.Status), event.Turn.DurationMs, adapter.codexTurnError(event.Turn.Error), false)
}

func (adapter *devStudioCodexAdapter) handlePlanUpdated(params json.RawMessage) {
	var event struct {
		Explanation *string `json:"explanation"`
		Plan        []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if json.Unmarshal(params, &event) != nil {
		return
	}
	plan := devStudioPlan{Steps: make([]devStudioPlanStep, 0, len(event.Plan))}
	for _, step := range event.Plan {
		status := "pending"
		switch step.Status {
		case "inProgress":
			status = "in_progress"
		case "completed":
			status = "completed"
		}
		plan.Steps = append(plan.Steps, devStudioPlanStep{Step: adapter.redact(step.Step), Status: status})
	}
	if event.Explanation != nil {
		explanation := adapter.redact(*event.Explanation)
		plan.Explanation = &explanation
	}
	adapter.emit(devStudioPlanEvent(plan))
	adapter.savePlan(plan)
}

func (adapter *devStudioCodexAdapter) handleRequestResolved(params json.RawMessage) {
	var event struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &event) != nil || len(event.RequestID) == 0 {
		return
	}
	requestID := devStudioRequestKey(event.RequestID)
	adapter.mu.Lock()
	_, open := adapter.questions[requestID]
	delete(adapter.questions, requestID)
	adapter.mu.Unlock()
	if open {
		adapter.emit(map[string]any{"type": "question.resolved", "requestId": requestID})
		adapter.emitStatus("thinking")
	}
}

// handleCodexError passes a Codex error on. With willRetry Codex is retrying
// and the turn goes on; without it, turn/completed with failed follows.
func (adapter *devStudioCodexAdapter) handleCodexError(params json.RawMessage) {
	var event struct {
		Error     devStudioCodexError `json:"error"`
		WillRetry bool                `json:"willRetry"`
	}
	if json.Unmarshal(params, &event) != nil {
		return
	}
	adapter.emit(map[string]any{
		"type":      "error",
		"code":      devStudioCodexErrorCode(event.Error.CodexErrorInfo),
		"message":   adapter.redact(event.Error.Message),
		"willRetry": event.WillRetry,
	})
	if event.WillRetry {
		adapter.emitStatus("retrying")
	}
}

func (adapter *devStudioCodexAdapter) emitItemStarted(item devStudioCodexItem) {
	switch item.Type {
	case "reasoning":
		adapter.emitStatus("thinking")
		adapter.emit(map[string]any{"type": "reasoning.started", "itemId": item.ID})
	case "agentMessage":
		phase := devStudioMessagePhase(item.Phase)
		adapter.mu.Lock()
		adapter.phases[item.ID] = phase
		adapter.mu.Unlock()
		adapter.emit(map[string]any{"type": "message.started", "itemId": item.ID, "phase": phase})
	case "commandExecution":
		command := adapter.commandSummary(item)
		adapter.mu.Lock()
		adapter.commands[item.ID] = command
		adapter.mu.Unlock()
		adapter.emitStatus(devStudioCommandStatus(command.actions))
		adapter.emit(map[string]any{
			"type":      "command.started",
			"commandId": item.ID,
			"command":   command.command,
			"actions":   command.actions,
			"cwd":       devStudioNullablePath(adapter.workspace.instanceRoot, adapter.workspace.instanceRoot, item.Cwd),
		})
	case "fileChange":
		adapter.emitStatus("editing")
	case "webSearch":
		adapter.emitStatus("searching_web")
	case "collabAgentToolCall":
		adapter.emitStatus("delegating")
	}
}

// emitItemCompleted sends the event for a finished item, live or, with
// replay, from the stored conversation.
func (adapter *devStudioCodexAdapter) emitItemCompleted(item devStudioCodexItem, replay bool) {
	var event map[string]any
	switch item.Type {
	case "userMessage":
		// Live, the browser already shows what the person sent, and has no
		// Codex item id to match an echo against.
		if replay {
			event = map[string]any{"type": "user.message", "itemId": item.ID, "text": adapter.redact(devStudioUserMessageText(item.Content))}
		}
	case "reasoning":
		event = map[string]any{"type": "reasoning.completed", "itemId": item.ID, "text": adapter.redact(strings.Join(item.Summary, devStudioSummarySeparator))}
	case "agentMessage":
		adapter.mu.Lock()
		delete(adapter.phases, item.ID)
		adapter.mu.Unlock()
		event = map[string]any{"type": "message.completed", "itemId": item.ID, "phase": devStudioMessagePhase(item.Phase), "text": adapter.redact(item.Text)}
	case "commandExecution":
		adapter.mu.Lock()
		delete(adapter.commands, item.ID)
		adapter.mu.Unlock()
		command := adapter.commandSummary(item)
		output := ""
		if item.AggregatedOutput != nil {
			output = adapter.redact(*item.AggregatedOutput)
		}
		event = map[string]any{
			"type":       "command.completed",
			"commandId":  item.ID,
			"command":    command.command,
			"actions":    command.actions,
			"status":     devStudioCommandCompletionStatus(item.Status),
			"exitCode":   item.ExitCode,
			"durationMs": item.DurationMs,
			"output":     output,
		}
	case "fileChange":
		changes := adapter.fileChanges(item.Changes)
		if len(changes) == 0 {
			break
		}
		event = map[string]any{"type": "files.changed", "itemId": item.ID, "status": devStudioFileChangeStatus(item.Status), "changes": changes}
	case "webSearch":
		event = adapter.researchStep(item)
	}
	if event != nil {
		if replay {
			event["replay"] = true
		}
		adapter.emit(event)
	}
	switch item.Type {
	case "commandExecution", "fileChange", "webSearch", "collabAgentToolCall":
		if !replay {
			adapter.emitStatus("thinking")
		}
	}
}

func (adapter *devStudioCodexAdapter) commandSummary(item devStudioCodexItem) devStudioStartedCommand {
	root := adapter.workspace.instanceRoot
	base := root
	if item.Cwd != "" {
		base = item.Cwd
	}
	actions := make([]map[string]any, 0, len(item.CommandActions))
	for _, action := range item.CommandActions {
		entry := map[string]any{"type": action.Type, "command": adapter.redact(action.Command)}
		switch action.Type {
		case "read":
			entry["path"] = devStudioNullablePath(root, base, devStudioString(action.Path))
			entry["name"] = adapter.redact(action.Name)
		case "listFiles":
			entry["path"] = devStudioNullablePath(root, base, devStudioString(action.Path))
		case "search":
			var query any
			if action.Query != nil {
				query = adapter.redact(*action.Query)
			}
			entry["query"] = query
			entry["path"] = devStudioNullablePath(root, base, devStudioString(action.Path))
		default:
			entry["type"] = "unknown"
		}
		actions = append(actions, entry)
	}
	return devStudioStartedCommand{command: adapter.redact(devStudioUnwrapShellCommand(item.Command)), actions: actions}
}

func (adapter *devStudioCodexAdapter) fileChanges(changes []devStudioCodexFileChange) []map[string]any {
	root := adapter.workspace.instanceRoot
	result := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		path, ok := devStudioDisplayPath(root, root, change.Path)
		if !ok {
			continue
		}
		diff, truncated := devStudioTruncate(adapter.redact(change.Diff), devStudioDiffLimit)
		entry := map[string]any{"path": path, "kind": change.Kind.Type, "diff": diff, "diffTruncated": truncated}
		if change.Kind.MovePath != nil {
			if movePath, ok := devStudioDisplayPath(root, root, *change.Kind.MovePath); ok {
				entry["movePath"] = movePath
			}
		}
		result = append(result, entry)
	}
	sort.SliceStable(result, func(left, right int) bool {
		return result[left]["path"].(string) < result[right]["path"].(string)
	})
	return result
}

func (adapter *devStudioCodexAdapter) researchStep(item devStudioCodexItem) map[string]any {
	action := "other"
	var query, url *string
	if item.Action != nil {
		switch item.Action.Type {
		case "search":
			action, query = "search", item.Action.Query
		case "openPage":
			action, url = "open_page", item.Action.URL
		case "findInPage":
			action, url, query = "find_in_page", item.Action.URL, item.Action.Pattern
		}
	}
	if action == "search" && devStudioString(query) == "" {
		query = &item.Query
	}
	event := map[string]any{"type": "research.step", "itemId": item.ID, "action": action}
	if text := devStudioString(query); text != "" {
		event["query"] = adapter.redact(text)
	}
	if text := devStudioString(url); text != "" {
		event["url"] = adapter.redact(text)
	}
	return event
}

// finishTurn ends the browser's turn once, whichever path gets there first:
// Codex's turn/completed, a failure while starting it, Codex stopping, or
// the session closing. Protected instruction files are restored first, and a
// restore turns the outcome into a failure the person sees.
func (adapter *devStudioCodexAdapter) finishTurn(turn *devStudioTurn, status string, durationMs *int64, failure *devStudioTurnError, announce bool) {
	adapter.mu.Lock()
	if turn == nil || adapter.turn != turn {
		adapter.mu.Unlock()
		return
	}
	adapter.turn = nil
	adapter.busy = false
	adapter.status = ""
	questions := adapter.questions
	adapter.questions = make(map[string]devStudioOpenQuestion)
	commands := adapter.commands
	adapter.commands = make(map[string]devStudioStartedCommand)
	closed := adapter.closed
	turnID := turn.id
	adapter.mu.Unlock()

	changed, err := restoreChangedDevStudioProtectedFiles(adapter.workspace, turn.protected)
	if closed {
		return
	}
	switch {
	case err != nil:
		status, announce = "failed", true
		failure = &devStudioTurnError{Code: "protected_file_restore_failed", Message: "Protected instruction files changed and could not be restored."}
	case len(changed) > 0:
		status, announce = "failed", true
		failure = &devStudioTurnError{Code: "protected_files_modified", Message: "Protected instruction files were restored: " + strings.Join(changed, ", ")}
	}

	for _, requestID := range devStudioSortedKeys(questions) {
		adapter.emit(map[string]any{"type": "question.resolved", "requestId": requestID})
	}
	for _, commandID := range devStudioSortedKeys(commands) {
		command := commands[commandID]
		adapter.emit(map[string]any{
			"type":       "command.completed",
			"commandId":  commandID,
			"command":    command.command,
			"actions":    command.actions,
			"status":     "interrupted",
			"exitCode":   nil,
			"durationMs": nil,
			"output":     "",
		})
	}
	if failure != nil && announce {
		adapter.emitError(failure.Code, failure.Message)
	}
	if durationMs == nil {
		elapsed := time.Since(turn.startedAt).Milliseconds()
		durationMs = &elapsed
	}
	adapter.emit(adapter.turnCompletedEvent(turnID, status, durationMs, failure))
}

func (adapter *devStudioCodexAdapter) turnCompletedEvent(turnID, status string, durationMs *int64, failure *devStudioTurnError) map[string]any {
	var id any
	if turnID != "" {
		id = turnID
	}
	event := map[string]any{"type": "turn.completed", "turnId": id, "status": status, "durationMs": durationMs}
	if failure != nil {
		event["error"] = failure
	}
	return event
}

func (adapter *devStudioCodexAdapter) codexTurnError(codexError *devStudioCodexError) *devStudioTurnError {
	if codexError == nil {
		return nil
	}
	return &devStudioTurnError{Code: devStudioCodexErrorCode(codexError.CodexErrorInfo), Message: adapter.redact(codexError.Message)}
}

// handleCodexExit runs when a Codex process stops. Its open calls fail at
// once, and a turn it was running ends as failed. The next turn starts Codex
// again and resumes the stored conversation.
func (adapter *devStudioCodexAdapter) handleCodexExit(connection *devStudioCodexConnection) {
	adapter.mu.Lock()
	pending := connection.pending
	connection.pending = make(map[string]chan devStudioJSONRPCResponse)
	current := adapter.connection == connection
	if current {
		adapter.connection = nil
	}
	turn := adapter.turn
	closed := adapter.closed
	adapter.mu.Unlock()

	failDevStudioPendingCalls(pending, errDevStudioCodexUnavailable)
	connection.process.Stop()
	if !current || closed || turn == nil {
		return
	}
	adapter.finishTurn(turn, "failed", nil, &devStudioTurnError{Code: "codex_unavailable", Message: "Codex stopped before the turn completed."}, true)
}

// dropConnection stops a Codex process that failed while starting.
func (adapter *devStudioCodexAdapter) dropConnection(connection *devStudioCodexConnection) {
	adapter.mu.Lock()
	if adapter.connection == connection {
		adapter.connection = nil
	}
	pending := connection.pending
	connection.pending = make(map[string]chan devStudioJSONRPCResponse)
	adapter.mu.Unlock()
	failDevStudioPendingCalls(pending, errDevStudioCodexUnavailable)
	connection.process.Stop()
}

func failDevStudioPendingCalls(pending map[string]chan devStudioJSONRPCResponse, err error) {
	for _, response := range pending {
		response <- devStudioJSONRPCResponse{Err: err}
	}
}

func (adapter *devStudioCodexAdapter) close() {
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return
	}
	adapter.closed = true
	turn := adapter.turn
	connection := adapter.connection
	adapter.connection = nil
	adapter.mu.Unlock()

	// Restores protected files; nothing is sent, the browser is gone.
	adapter.finishTurn(turn, "interrupted", nil, nil, false)
	adapter.mu.Lock()
	adapter.deployKey = ""
	var pending map[string]chan devStudioJSONRPCResponse
	if connection != nil {
		pending = connection.pending
		connection.pending = make(map[string]chan devStudioJSONRPCResponse)
	}
	adapter.mu.Unlock()
	failDevStudioPendingCalls(pending, errDevStudioSessionClosed)
	if connection != nil {
		connection.process.Stop()
	}
}

// emitStatus says what the agent is doing now. It is sent only while a turn
// the browser has been told about runs, and only when it changes.
func (adapter *devStudioCodexAdapter) emitStatus(status string) {
	adapter.mu.Lock()
	if adapter.turn == nil || !adapter.turn.announced || adapter.status == status {
		adapter.mu.Unlock()
		return
	}
	adapter.status = status
	adapter.mu.Unlock()
	adapter.emit(map[string]string{"type": "status.changed", "status": status})
}

func (adapter *devStudioCodexAdapter) emitError(code, message string) {
	adapter.emit(map[string]any{"type": "error", "code": code, "message": message, "willRetry": false})
}

func (adapter *devStudioCodexAdapter) emit(event any) {
	if adapter.send != nil {
		_ = adapter.send(event)
	}
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

// devStudioLogValue makes a value safe to print in one log line. Errors and
// values can carry what the browser or Codex sent, such as an instance
// folder, and a line break in them could forge a log line.
func devStudioLogValue(value any) string {
	text := strings.ReplaceAll(fmt.Sprint(value), "\n", `\n`)
	return strings.ReplaceAll(text, "\r", `\r`)
}

func devStudioMessagePhase(phase *string) string {
	if phase != nil && *phase == "commentary" {
		return "commentary"
	}
	return "final_answer"
}

func devStudioTurnStatus(status string) string {
	switch status {
	case "completed", "failed":
		return status
	default:
		// A turn stored while still running was cut off when Codex stopped.
		return "interrupted"
	}
}

func devStudioCommandCompletionStatus(status string) string {
	switch status {
	case "completed", "failed", "declined":
		return status
	default:
		return "interrupted"
	}
}

func devStudioFileChangeStatus(status string) string {
	switch status {
	case "completed", "declined":
		return status
	default:
		return "failed"
	}
}

// devStudioCommandStatus tells what a command is for from Codex's parsed
// actions: reading instruction files, looking around, or running something.
func devStudioCommandStatus(actions []map[string]any) string {
	if len(actions) == 0 {
		return "running_command"
	}
	status := "inspecting_workspace"
	for _, action := range actions {
		switch action["type"] {
		case "read":
			if path, ok := action["path"].(string); ok && devStudioProtectedRelativePath(path) {
				status = "reading_instructions"
			}
		case "listFiles", "search":
		default:
			return "running_command"
		}
	}
	return status
}

// devStudioCodexErrorCode names a Codex error by its kind, such as
// usageLimitExceeded or responseStreamDisconnected.
func devStudioCodexErrorCode(info json.RawMessage) string {
	var name string
	if json.Unmarshal(info, &name) == nil && name != "" {
		return name
	}
	var variants map[string]json.RawMessage
	if json.Unmarshal(info, &variants) == nil && len(variants) == 1 {
		for name := range variants {
			return name
		}
	}
	return "codex_error"
}

// devStudioUnwrapShellCommand removes the shell Codex runs a command through,
// such as /bin/zsh -c '…', leaving the command the agent wrote. A command
// that is not exactly one wrapped shell word is returned unchanged.
func devStudioUnwrapShellCommand(command string) string {
	matches := devStudioShellWrapperPattern.FindStringSubmatch(command)
	if matches == nil {
		return command
	}
	word, ok := devStudioShellWord(matches[1])
	if !ok || strings.TrimSpace(word) == "" {
		return command
	}
	return word
}

// devStudioShellWord unquotes text that must be a single shell word made of
// single-quoted, double-quoted and plain parts.
func devStudioShellWord(text string) (string, bool) {
	var word strings.Builder
	for index := 0; index < len(text); {
		switch character := text[index]; {
		case character == '\'':
			end := strings.IndexByte(text[index+1:], '\'')
			if end < 0 {
				return "", false
			}
			word.WriteString(text[index+1 : index+1+end])
			index += end + 2
		case character == '"':
			index++
			closed := false
			for index < len(text) && !closed {
				switch {
				case text[index] == '"':
					closed = true
					index++
				case text[index] == '\\' && index+1 < len(text) && strings.IndexByte("$`\"\\\n", text[index+1]) >= 0:
					word.WriteByte(text[index+1])
					index += 2
				default:
					word.WriteByte(text[index])
					index++
				}
			}
			if !closed {
				return "", false
			}
		case character == '\\' && index+1 < len(text):
			word.WriteByte(text[index+1])
			index += 2
		case character == ' ' || character == '\t' || character == '\n':
			return "", false
		default:
			word.WriteByte(character)
			index++
		}
	}
	return word.String(), true
}

// devStudioDisplayPath gives path relative to the instance root. A relative
// path is resolved against base, a command's working directory. The root
// itself is "."; a path outside the root has no display path.
func devStudioDisplayPath(root, base, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		if !filepath.IsAbs(base) {
			base = filepath.Join(root, base)
		}
		path = filepath.Join(base, path)
	}
	if filepath.Clean(path) == filepath.Clean(root) {
		return ".", true
	}
	return devStudioRelativePath(root, path)
}

func devStudioNullablePath(root, base, path string) any {
	if relative, ok := devStudioDisplayPath(root, base, path); ok {
		return relative
	}
	return nil
}

func devStudioString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func devStudioTruncate(text string, limit int) (string, bool) {
	if len(text) <= limit || utf8.RuneCountInString(text) <= limit {
		return text, false
	}
	return string([]rune(text)[:limit]), true
}

func devStudioSortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// devStudioThreadPath is where the bridge keeps an instance's conversation:
// in its own folder, never in the workspace. Codex's sandbox lets the agent
// write anywhere in the workspace, symlinks included, while the bridge runs
// outside that sandbox.
func devStudioThreadPath(instanceID string) (string, error) {
	// The id names a folder, so it is checked here, beside that use, as well
	// as when the browser connects.
	if instanceID == "." || instanceID == ".." || !devStudioInstanceIDPattern.MatchString(instanceID) {
		return "", fmt.Errorf("invalid instance ID: %q", instanceID)
	}
	homeDir, err := home.Dir()
	if err != nil {
		return "", fmt.Errorf("failed to find the Simple CLI home: %w", err)
	}
	return filepath.Join(homeDir, ".simple", "devstudio", instanceID, devStudioThreadFile), nil
}

// devStudioUserMessageText is the text of one of the person's messages.
// Images, skills and mentions have no text and are left out.
func devStudioUserMessageText(content json.RawMessage) string {
	var inputs []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &inputs) != nil {
		return ""
	}
	texts := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if input.Type == "text" && input.Text != "" {
			texts = append(texts, input.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// readDevStudioThreadID returns the conversation stored for this instance
// folder, or "" when there is none. A conversation stored for another folder
// of the same instance is not returned.
func readDevStudioThreadID(workspace *devStudioWorkspace) (string, error) {
	record, err := readDevStudioThreadRecord(workspace)
	if err != nil || record.ThreadID == "" {
		return "", err
	}
	if record.InstanceRoot != workspace.instanceRoot {
		log.Printf("[DevStudio Bridge] The stored conversation belongs to another folder of this instance; starting a new one")
		return "", nil
	}
	return record.ThreadID, nil
}

// writeDevStudioThreadID stores a conversation id with no plan yet.
func writeDevStudioThreadID(workspace *devStudioWorkspace, threadID string) error {
	return writeDevStudioThreadRecord(workspace, devStudioThreadRecord{ThreadID: threadID})
}

func readDevStudioThreadRecord(workspace *devStudioWorkspace) (devStudioThreadRecord, error) {
	var record devStudioThreadRecord
	path, err := devStudioThreadPath(workspace.instanceID)
	if err != nil {
		return record, err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return record, nil
	}
	if err != nil {
		return record, fmt.Errorf("failed to read the stored conversation: %w", err)
	}
	if err := json.Unmarshal(contents, &record); err != nil {
		return devStudioThreadRecord{}, fmt.Errorf("failed to read the stored conversation: %w", err)
	}
	record.ThreadID = strings.TrimSpace(record.ThreadID)
	return record, nil
}

// writeDevStudioThreadRecord stores record for this instance folder.
func writeDevStudioThreadRecord(workspace *devStudioWorkspace, record devStudioThreadRecord) error {
	path, err := devStudioThreadPath(workspace.instanceID)
	if err != nil {
		return err
	}
	record.InstanceRoot = workspace.instanceRoot
	if err := os.MkdirAll(filepath.Dir(path), fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to store the conversation: %w", err)
	}
	contents, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to store the conversation: %w", err)
	}
	if err := os.WriteFile(path, append(contents, '\n'), fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to store the conversation: %w", err)
	}
	return nil
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
	log.Printf("[DevStudio Bridge] Writing SCL configuration to %s", devStudioLogValue(path))
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

package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeDevStudioCodexStarter stands in for codex app-server. Its processes
// answer the bridge's calls the way codex-cli 0.144.1 did in a recorded live
// turn, and record every request and reply the bridge sends.
type fakeDevStudioCodexStarter struct {
	mu                   sync.Mutex
	starts               []devStudioCodexStartConfig
	requests             []fakeDevStudioCodexRequest
	replies              []fakeDevStudioCodexReply
	changePaths          []string
	holdTurnOpen         bool
	exitOnMethod         string
	failInitialize       bool
	failThreadStart      bool
	failResume           bool
	failRead             bool
	malformedThreadStart bool
	malformedTurnStart   bool
	// turnStartGate, when set, holds the turn/start reply until it is closed.
	turnStartGate     chan struct{}
	history           []map[string]any
	threadStarts      int
	currentThread     string
	onTurnStart       func(process *fakeDevStudioCodexProcess)
	processes         []*fakeDevStudioCodexProcess
	interruptReceived chan struct{}
}

type fakeDevStudioCodexRequest struct {
	Method string
	Params map[string]any
}

// fakeDevStudioCodexReply is the bridge's answer to a request the fake sent.
type fakeDevStudioCodexReply struct {
	ID     string
	Result map[string]any
	Error  map[string]any
}

func newFakeDevStudioCodexStarter(changePaths []string) *fakeDevStudioCodexStarter {
	return &fakeDevStudioCodexStarter{
		changePaths:       changePaths,
		interruptReceived: make(chan struct{}, 1),
	}
}

func (starter *fakeDevStudioCodexStarter) Start(config devStudioCodexStartConfig) (devStudioCodexProcess, error) {
	process := newFakeDevStudioCodexProcess(starter, config.WorkspaceRoot)
	starter.mu.Lock()
	starter.starts = append(starter.starts, config)
	starter.processes = append(starter.processes, process)
	starter.mu.Unlock()
	return process, nil
}

func (starter *fakeDevStudioCodexStarter) startCount() int {
	starter.mu.Lock()
	defer starter.mu.Unlock()
	return len(starter.starts)
}

func (starter *fakeDevStudioCodexStarter) recordRequest(method string, raw json.RawMessage) {
	var params map[string]any
	_ = json.Unmarshal(raw, &params)
	starter.mu.Lock()
	starter.requests = append(starter.requests, fakeDevStudioCodexRequest{Method: method, Params: params})
	starter.mu.Unlock()
}

func (starter *fakeDevStudioCodexStarter) requestMethods() []string {
	starter.mu.Lock()
	defer starter.mu.Unlock()
	methods := make([]string, 0, len(starter.requests))
	for _, request := range starter.requests {
		methods = append(methods, request.Method)
	}
	return methods
}

func (starter *fakeDevStudioCodexStarter) request(method string) (fakeDevStudioCodexRequest, bool) {
	starter.mu.Lock()
	defer starter.mu.Unlock()
	for _, request := range starter.requests {
		if request.Method == method {
			return request, true
		}
	}
	return fakeDevStudioCodexRequest{}, false
}

func (starter *fakeDevStudioCodexStarter) waitForRequest(t *testing.T, method string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := starter.request(method); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Codex never received %s", method)
}

// waitForReply waits for the bridge's answer to the request with this raw
// JSON id, such as 0 or "approval-7".
func (starter *fakeDevStudioCodexStarter) waitForReply(t *testing.T, id string) fakeDevStudioCodexReply {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		starter.mu.Lock()
		for _, reply := range starter.replies {
			if reply.ID == id {
				starter.mu.Unlock()
				return reply
			}
		}
		starter.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the bridge did not answer Codex request %s", id)
	return fakeDevStudioCodexReply{}
}

type fakeDevStudioCodexProcess struct {
	inputReader  *io.PipeReader
	inputWriter  *io.PipeWriter
	outputReader *io.PipeReader
	outputWriter *io.PipeWriter
	starter      *fakeDevStudioCodexStarter
	root         string
	writeMu      sync.Mutex
	encoder      *json.Encoder
	once         sync.Once
	stopMu       sync.Mutex
	closed       bool
}

func newFakeDevStudioCodexProcess(starter *fakeDevStudioCodexStarter, root string) *fakeDevStudioCodexProcess {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	process := &fakeDevStudioCodexProcess{
		inputReader:  inputReader,
		inputWriter:  inputWriter,
		outputReader: outputReader,
		outputWriter: outputWriter,
		starter:      starter,
		root:         root,
		encoder:      json.NewEncoder(outputWriter),
	}
	go process.run()
	return process
}

func (process *fakeDevStudioCodexProcess) Input() io.WriteCloser { return process.inputWriter }

func (process *fakeDevStudioCodexProcess) Output() io.ReadCloser { return process.outputReader }

func (process *fakeDevStudioCodexProcess) isStopped() bool {
	process.stopMu.Lock()
	defer process.stopMu.Unlock()
	return process.closed
}

func (process *fakeDevStudioCodexProcess) Stop() {
	process.once.Do(func() {
		process.stopMu.Lock()
		process.closed = true
		process.stopMu.Unlock()
		_ = process.inputWriter.Close()
		_ = process.outputWriter.Close()
	})
}

func (process *fakeDevStudioCodexProcess) write(message map[string]any) {
	message["jsonrpc"] = "2.0"
	process.writeMu.Lock()
	defer process.writeMu.Unlock()
	_ = process.encoder.Encode(message)
}

func (process *fakeDevStudioCodexProcess) reply(id int, result map[string]any) {
	process.write(map[string]any{"id": id, "result": result})
}

func (process *fakeDevStudioCodexProcess) fail(id int, message string) {
	process.write(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": message}})
}

func (process *fakeDevStudioCodexProcess) notify(method string, params map[string]any) {
	if _, ok := params["threadId"]; !ok {
		process.starter.mu.Lock()
		params["threadId"] = process.starter.currentThread
		process.starter.mu.Unlock()
	}
	process.write(map[string]any{"method": method, "params": params})
}

// ask sends a request from Codex to the bridge. id is kept as given, so a
// test can use 0 or a string.
func (process *fakeDevStudioCodexProcess) ask(id any, method string, params map[string]any) {
	process.write(map[string]any{"id": id, "method": method, "params": params})
}

// run answers the bridge. Answers are written from their own goroutines, so
// the fake keeps reading while it writes, as Codex does.
func (process *fakeDevStudioCodexProcess) run() {
	decoder := json.NewDecoder(process.inputReader)
	for {
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result map[string]any  `json:"result"`
			Error  map[string]any  `json:"error"`
		}
		if err := decoder.Decode(&message); err != nil {
			return
		}
		if message.Method == "" {
			process.starter.mu.Lock()
			process.starter.replies = append(process.starter.replies, fakeDevStudioCodexReply{ID: string(message.ID), Result: message.Result, Error: message.Error})
			process.starter.mu.Unlock()
			continue
		}
		process.starter.recordRequest(message.Method, message.Params)
		if len(message.ID) == 0 {
			continue
		}
		var id int
		_ = json.Unmarshal(message.ID, &id)
		process.starter.mu.Lock()
		exitOn := process.starter.exitOnMethod
		process.starter.mu.Unlock()
		if exitOn != "" && exitOn == message.Method {
			process.Stop()
			return
		}
		go process.answer(id, message.Method, message.Params)
	}
}

func (process *fakeDevStudioCodexProcess) answer(id int, method string, raw json.RawMessage) {
	starter := process.starter
	starter.mu.Lock()
	defer starter.mu.Unlock()
	switch method {
	case "initialize":
		if starter.failInitialize {
			process.fail(id, "init_failed")
			return
		}
		process.reply(id, map[string]any{"userAgent": "fake", "codexHome": "/tmp", "platformFamily": "unix", "platformOs": "linux"})
	case "thread/start":
		if starter.failThreadStart {
			process.fail(id, "thread_start_failed")
			return
		}
		if starter.malformedThreadStart {
			process.reply(id, map[string]any{"thread": map[string]any{}})
			return
		}
		starter.threadStarts++
		threadID := "thread_fake"
		if starter.threadStarts > 1 {
			threadID = "thread_fake_" + string(rune('0'+starter.threadStarts))
		}
		starter.currentThread = threadID
		process.reply(id, map[string]any{"thread": map[string]any{"id": threadID, "turns": []any{}}})
	case "thread/resume":
		var params struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(raw, &params)
		if starter.failResume {
			process.fail(id, "no rollout found for thread id "+params.ThreadID)
			return
		}
		starter.currentThread = params.ThreadID
		process.reply(id, map[string]any{"thread": map[string]any{"id": params.ThreadID}})
	case "thread/read":
		var params struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(raw, &params)
		if starter.failRead {
			process.fail(id, "thread_read_failed")
			return
		}
		process.reply(id, map[string]any{"thread": map[string]any{"id": params.ThreadID, "turns": starter.history}})
	case "turn/start":
		go process.startTurn(id, starter.turnStartGate, starter.malformedTurnStart, starter.onTurnStart, starter.holdTurnOpen)
	case "turn/interrupt":
		process.reply(id, map[string]any{})
		select {
		case starter.interruptReceived <- struct{}{}:
		default:
		}
		go process.completeTurn("interrupted", nil)
	}
}

func (process *fakeDevStudioCodexProcess) startTurn(id int, gate chan struct{}, malformed bool, onTurn func(*fakeDevStudioCodexProcess), holdOpen bool) {
	if gate != nil {
		<-gate
	}
	if malformed {
		process.reply(id, map[string]any{"turn": map[string]any{}})
		return
	}
	process.reply(id, map[string]any{"turn": map[string]any{"id": "turn_fake", "status": "inProgress"}})
	process.notify("turn/started", map[string]any{"turn": map[string]any{"id": "turn_fake", "status": "inProgress"}})
	switch {
	case onTurn != nil:
		onTurn(process)
	case !holdOpen:
		process.emitTurnEvents()
	}
}

func (process *fakeDevStudioCodexProcess) item(method string, item map[string]any) {
	process.notify(method, map[string]any{"turnId": "turn_fake", "item": item})
}

// emitTurnEvents plays one turn shaped like the recorded live turn: a plan,
// a reasoning summary, a commentary note, a command, a file change, a web
// search and the final answer.
func (process *fakeDevStudioCodexProcess) emitTurnEvents() {
	userMessage := map[string]any{"type": "userMessage", "id": "user_1", "content": []map[string]any{{"type": "text", "text": "Change two apps."}}}
	process.item("item/started", userMessage)
	process.item("item/completed", userMessage)
	process.notify("turn/plan/updated", map[string]any{"turnId": "turn_fake", "explanation": nil, "plan": []map[string]any{
		{"step": "Inspect existing workspace apps", "status": "inProgress"},
		{"step": "Confirm preferred lead fields", "status": "pending"},
	}})
	process.item("item/started", map[string]any{"type": "reasoning", "id": "rs_1", "summary": []string{}, "content": []string{}})
	process.notify("item/reasoning/summaryPartAdded", map[string]any{"itemId": "rs_1", "summaryIndex": 0})
	process.notify("item/reasoning/summaryTextDelta", map[string]any{"itemId": "rs_1", "delta": "Checking", "summaryIndex": 0})
	process.notify("item/reasoning/summaryPartAdded", map[string]any{"itemId": "rs_1", "summaryIndex": 1})
	process.notify("item/reasoning/summaryTextDelta", map[string]any{"itemId": "rs_1", "delta": "the apps", "summaryIndex": 1})
	process.item("item/completed", map[string]any{"type": "reasoning", "id": "rs_1", "summary": []string{"Checking", "the apps"}, "content": []string{"raw reasoning never sent"}})
	process.item("item/started", map[string]any{"type": "agentMessage", "id": "msg_1", "text": "", "phase": "commentary"})
	process.notify("item/agentMessage/delta", map[string]any{"itemId": "msg_1", "delta": "I'll deploy app-one."})
	process.item("item/completed", map[string]any{"type": "agentMessage", "id": "msg_1", "text": "I'll deploy app-one.", "phase": "commentary"})

	command := map[string]any{
		"type": "commandExecution", "id": "command_1", "cwd": process.root, "status": "inProgress",
		"command":          "/bin/zsh -c 'simple deploy apps/app-one --env dev'",
		"commandActions":   []map[string]any{{"type": "unknown", "command": "simple deploy apps/app-one --env dev"}},
		"aggregatedOutput": nil, "exitCode": nil, "durationMs": nil,
	}
	process.item("item/started", command)
	process.notify("item/commandExecution/outputDelta", map[string]any{"turnId": "turn_fake", "itemId": "command_1", "delta": "Deploying app-one\n"})
	completed := map[string]any{}
	for key, value := range command {
		completed[key] = value
	}
	completed["status"], completed["aggregatedOutput"], completed["exitCode"], completed["durationMs"] = "completed", "Deployed app-one@1.0.1\n", 0, 509
	process.item("item/completed", completed)

	changes := make([]map[string]any, 0, len(process.starter.changePaths))
	for _, path := range process.starter.changePaths {
		changes = append(changes, map[string]any{"path": path, "kind": map[string]any{"type": "update", "move_path": nil}, "diff": "@@ -1 +1 @@\n-old\n+new\n"})
	}
	process.item("item/started", map[string]any{"type": "fileChange", "id": "change_1", "changes": changes, "status": "inProgress"})
	process.item("item/completed", map[string]any{"type": "fileChange", "id": "change_1", "changes": changes, "status": "completed"})
	process.item("item/started", map[string]any{"type": "webSearch", "id": "ws_1", "query": "", "action": map[string]any{"type": "other"}})
	process.item("item/completed", map[string]any{"type": "webSearch", "id": "ws_1", "query": "crm lead fields", "action": map[string]any{"type": "search", "query": "crm lead fields", "queries": []string{"crm lead fields"}}})
	process.item("item/started", map[string]any{"type": "agentMessage", "id": "msg_2", "text": "", "phase": nil})
	process.notify("item/agentMessage/delta", map[string]any{"itemId": "msg_2", "delta": "Safe Codex response"})
	process.item("item/completed", map[string]any{"type": "agentMessage", "id": "msg_2", "text": "Safe Codex response", "phase": nil})
	process.completeTurn("completed", nil)
}

func (process *fakeDevStudioCodexProcess) completeTurn(status string, turnError map[string]any) {
	process.notify("turn/completed", map[string]any{"turn": map[string]any{"id": "turn_fake", "items": []any{}, "status": status, "error": turnError, "durationMs": 1234}})
}

// connectDevStudioSession opens a session for instanceID against bridge and
// returns the connection and its session.ready message.
func connectDevStudioSession(t *testing.T, bridge *devStudioBridge, instanceID string) (*websocket.Conn, map[string]any) {
	t.Helper()
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	t.Cleanup(func() { _ = connection.Close() })
	sendDevStudioMessage(t, connection, sessionConnectMessage(instanceID))
	ready := readDevStudioMessage(t, connection)
	if ready["type"] != "session.ready" {
		t.Fatalf("session type = %v, want session.ready: %v", ready["type"], ready)
	}
	return connection, ready
}

func TestDevStudioCodexStartsAtConnectAndUsesVerifiedProtocol(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_codex")
	createDevStudioApp(t, baseDir, "inst_codex", "app-one")
	instanceRoot := filepath.Join(baseDir, "inst_codex")
	starter := newFakeDevStudioCodexStarter(nil)
	connection, ready := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_codex")

	if starter.startCount() != 1 {
		t.Fatalf("Codex starts before the first turn = %d, want 1", starter.startCount())
	}
	if ready["threadId"] != "thread_fake" || ready["resumed"] != false || ready["protocolVersion"] != float64(2) {
		t.Fatalf("session.ready = %v", ready)
	}
	starter.mu.Lock()
	start := starter.starts[0]
	starter.mu.Unlock()
	if start.WorkspaceRoot != instanceRoot || start.InstanceID != "inst_codex" || start.DeployKey != "test_deploy_key_secret" {
		t.Fatal("Codex start config did not retain the session-only instance context")
	}

	initialize, _ := starter.request("initialize")
	capabilities, _ := initialize.Params["capabilities"].(map[string]any)
	if capabilities["experimentalApi"] != true || capabilities["requestAttestation"] != false {
		t.Fatalf("initialize capabilities = %v", capabilities)
	}
	optOut := anyStrings(capabilities["optOutNotificationMethods"].([]any))
	for _, method := range []string{"thread/tokenUsage/updated", "mcpServer/startupStatus/updated", "item/reasoning/textDelta"} {
		if !strings.Contains(strings.Join(optOut, ","), method) {
			t.Fatalf("initialize does not opt out of %s: %v", method, optOut)
		}
	}

	threadStart, _ := starter.request("thread/start")
	if threadStart.Params["cwd"] != instanceRoot || threadStart.Params["approvalPolicy"] != "never" || threadStart.Params["sandbox"] != "workspace-write" ||
		threadStart.Params["ephemeral"] != false || threadStart.Params["historyMode"] != "legacy" {
		t.Fatalf("thread/start params = %v", threadStart.Params)
	}
	config, _ := threadStart.Params["config"].(map[string]any)
	wantConfig := map[string]any{
		"web_search": "live", "features.default_mode_request_user_input": true, "suppress_unstable_features_warning": true,
		"model_reasoning_summary": "concise", "model_supports_reasoning_summaries": true,
	}
	for key, want := range wantConfig {
		if config[key] != want {
			t.Fatalf("thread/start config %s = %v, want %v", key, config[key], want)
		}
	}
	record, err := readDevStudioThreadRecord(&devStudioWorkspace{instanceID: "inst_codex", instanceRoot: instanceRoot})
	if err != nil || record.ThreadID != "thread_fake" || record.InstanceRoot != instanceRoot {
		t.Fatalf("stored conversation = %+v, %v", record, err)
	}
	if _, err := os.Stat(filepath.Join(instanceRoot, ".simple", "devstudio")); !os.IsNotExist(err) {
		t.Fatalf("the bridge wrote its record inside the workspace the agent can edit: %v", err)
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Inspect both applications."})
	readDevStudioEvent(t, connection, "turn.completed")
	if got, want := starter.requestMethods(), []string{"initialize", "initialized", "thread/start", "turn/start"}; !sameStrings(got, want) {
		t.Fatalf("protocol methods = %v, want %v", got, want)
	}
	turnStart, _ := starter.request("turn/start")
	if turnStart.Params["threadId"] != "thread_fake" || turnStart.Params["cwd"] != instanceRoot {
		t.Fatalf("turn/start params = %v", turnStart.Params)
	}
}

func TestDevStudioCodexInstructionsRequireSimpleCLIScaffolding(t *testing.T) {
	instructions := devStudioCodexInstructions("acme", "/workspace/simple-devstudio/acme")
	if !strings.Contains(instructions, "simple new app <app-id> <name>") {
		t.Fatalf("instructions do not require the verified Simple CLI app scaffold: %s", instructions)
	}
	if !strings.Contains(instructions, "simple --json build <app-id>") {
		t.Fatalf("instructions do not explain non-interactive Simple CLI usage: %s", instructions)
	}
}

func TestDevStudioCodexInstructionsAllowFollowUpAfterValidationFailure(t *testing.T) {
	instructions := devStudioCodexInstructions("acme", "/workspace/simple-devstudio/acme")
	if strings.Contains(instructions, "do not retry automatically") || strings.Contains(instructions, "do not invent a workaround") {
		t.Fatalf("instructions still prohibit follow-up after a validation failure: %s", instructions)
	}
	if !strings.Contains(instructions, "continue with subsequent investigation, validation, or remediation when appropriate") {
		t.Fatalf("instructions do not allow follow-up after a validation failure: %s", instructions)
	}
}

func TestDevStudioCodexInstructionsDefaultDeploymentsToDevStudio(t *testing.T) {
	instructions := devStudioCodexInstructions("acme", "/workspace/simple-devstudio/acme")
	if !strings.Contains(instructions, "Do not ask the user to choose dev, staging, or prod.") {
		t.Fatalf("instructions do not default deployments to devstudio: %s", instructions)
	}
	if !strings.Contains(instructions, "simple --json deploy apps/<app-id> --env devstudio") {
		t.Fatalf("instructions do not provide the DevStudio deploy command: %s", instructions)
	}
}

func TestDevStudioCodexMapsTurnEventsToProtocolVersion2(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_events")
	createDevStudioApp(t, baseDir, "inst_events", "app-one")
	createDevStudioApp(t, baseDir, "inst_events", "app-two")
	instanceRoot := filepath.Join(baseDir, "inst_events")
	starter := newFakeDevStudioCodexStarter([]string{
		filepath.Join(instanceRoot, devStudioAppsDirectory, "app-two", "records", "two.scl"),
		filepath.Join(instanceRoot, devStudioAppsDirectory, "app-one", "records", "one.scl"),
		"/elsewhere/outside.scl",
	})
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_events")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Change two apps."})
	events := readDevStudioEventsUntil(t, connection, "turn.completed")

	byType := map[string][]map[string]any{}
	for _, event := range events {
		eventType, _ := event["type"].(string)
		byType[eventType] = append(byType[eventType], event)
	}
	if started := byType["turn.started"]; len(started) != 1 || started[0]["turnId"] != "turn_fake" || events[0]["type"] != "turn.started" {
		t.Fatalf("turn.started = %v, first event %v", started, events[0])
	}
	plan := byType["plan.updated"][0]
	steps, _ := plan["steps"].([]any)
	if plan["explanation"] != nil || len(steps) != 2 || steps[0].(map[string]any)["status"] != "in_progress" || steps[1].(map[string]any)["status"] != "pending" {
		t.Fatalf("plan.updated = %v", plan)
	}

	reasoning := ""
	for _, delta := range byType["reasoning.delta"] {
		reasoning += delta["text"].(string)
	}
	completedReasoning := byType["reasoning.completed"][0]
	if len(byType["reasoning.started"]) != 1 || reasoning != "Checking\n\nthe apps" || completedReasoning["text"] != reasoning {
		t.Fatalf("reasoning deltas %q, completed %v", reasoning, completedReasoning)
	}

	messages := byType["message.completed"]
	if len(messages) != 2 || messages[0]["phase"] != "commentary" || messages[0]["text"] != "I'll deploy app-one." ||
		messages[1]["phase"] != "final_answer" || messages[1]["text"] != "Safe Codex response" {
		t.Fatalf("message.completed = %v", messages)
	}
	for _, delta := range byType["message.delta"] {
		if delta["itemId"] == "msg_1" && delta["phase"] != "commentary" {
			t.Fatalf("commentary delta lost its phase: %v", delta)
		}
	}

	commandStarted := byType["command.started"][0]
	wantActions := []any{map[string]any{"type": "unknown", "command": "simple deploy apps/app-one --env dev"}}
	if commandStarted["command"] != "simple deploy apps/app-one --env dev" || commandStarted["cwd"] != "." || !sameJSON(commandStarted["actions"], wantActions) {
		t.Fatalf("command.started = %v", commandStarted)
	}
	commandCompleted := byType["command.completed"][0]
	if commandCompleted["status"] != "completed" || commandCompleted["exitCode"] != float64(0) || commandCompleted["durationMs"] != float64(509) ||
		commandCompleted["output"] != "Deployed app-one@1.0.1\n" || commandCompleted["command"] != "simple deploy apps/app-one --env dev" {
		t.Fatalf("command.completed = %v", commandCompleted)
	}
	if output := byType["command.output"][0]; output["commandId"] != "command_1" || output["text"] != "Deploying app-one\n" {
		t.Fatalf("command.output = %v", output)
	}

	files := byType["files.changed"]
	wantChanges := []any{
		map[string]any{"path": "apps/app-one/records/one.scl", "kind": "update", "diff": "@@ -1 +1 @@\n-old\n+new\n", "diffTruncated": false},
		map[string]any{"path": "apps/app-two/records/two.scl", "kind": "update", "diff": "@@ -1 +1 @@\n-old\n+new\n", "diffTruncated": false},
	}
	if len(files) != 1 || files[0]["itemId"] != "change_1" || files[0]["status"] != "completed" || !sameJSON(files[0]["changes"], wantChanges) {
		t.Fatalf("files.changed = %v", files)
	}
	research := byType["research.step"]
	if len(research) != 1 || research[0]["action"] != "search" || research[0]["query"] != "crm lead fields" || research[0]["itemId"] != "ws_1" {
		t.Fatalf("research.step = %v", research)
	}

	for _, status := range []string{"thinking", "running_command", "editing", "searching_web"} {
		if !hasDevStudioStatus(events, status) {
			t.Fatalf("events missing status %s: %v", status, events)
		}
	}
	for _, event := range events {
		if event["type"] == "status.changed" && (event["status"] == "complete" || event["status"] == "error") {
			t.Fatalf("version 1 status sent: %v", event)
		}
		if strings.Contains(mustJSON(t, event), "raw reasoning never sent") {
			t.Fatalf("raw reasoning text reached the browser: %v", event)
		}
	}
	if hasDevStudioEvent(events, "user.message") {
		t.Fatalf("the person's own message was echoed live: %v", events)
	}
	completed := events[len(events)-1]
	if completed["turnId"] != "turn_fake" || completed["status"] != "completed" || completed["durationMs"] != float64(1234) || completed["error"] != nil {
		t.Fatalf("turn.completed = %v", completed)
	}
	record, err := readDevStudioThreadRecord(&devStudioWorkspace{instanceID: "inst_events", instanceRoot: instanceRoot})
	if err != nil || record.ThreadID != "thread_fake" || record.Plan == nil || len(record.Plan.Steps) != 2 || record.Plan.Steps[0].Status != "in_progress" {
		t.Fatalf("stored record = %+v, %v, want the latest plan beside the conversation id", record, err)
	}
}

func TestDevStudioBridgeDoesNotOwnSimpleCLIExecution(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_no_bridge_cli")
	createDevStudioApp(t, baseDir, "inst_no_bridge_cli", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_no_bridge_cli")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Decide whether deployment is appropriate."})
	readDevStudioEvent(t, connection, "turn.completed")

	if starter.startCount() != 1 {
		t.Fatalf("Codex starts = %d, want 1", starter.startCount())
	}
	if got := strings.Join(starter.requestMethods(), ","); strings.Contains(got, "simple build") || strings.Contains(got, "simple test") || strings.Contains(got, "simple deploy") {
		t.Fatalf("bridge sent a Simple CLI command through the Codex protocol: %s", got)
	}
}

func TestDevStudioCodexRejectsSecondTurnAndInterruptsActiveTurn(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_cancel")
	createDevStudioApp(t, baseDir, "inst_cancel", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	starter.holdTurnOpen = true
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_cancel")

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Long task"})
	readDevStudioEvent(t, connection, "turn.started")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Second task"})
	secondTurnEvents := readDevStudioEventsUntil(t, connection, "error")
	if secondTurnError := secondTurnEvents[len(secondTurnEvents)-1]; secondTurnError["code"] != "turn_already_active" || secondTurnError["willRetry"] != false {
		t.Fatalf("second turn error = %v, want turn_already_active", secondTurnError)
	}
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.cancel"})
	select {
	case <-starter.interruptReceived:
	case <-time.After(time.Second):
		t.Fatal("turn.cancel did not call turn/interrupt")
	}
	if completed := readDevStudioEvent(t, connection, "turn.completed"); completed["status"] != "interrupted" {
		t.Fatalf("turn.completed = %v, want interrupted", completed)
	}
}

// A turn start waits on Codex for up to three calls. The read loop must stay
// free meanwhile, or a cancel sent then is not read until Codex answers.
func TestDevStudioCodexReadLoopStaysFreeWhileTurnStarts(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_free_loop")
	createDevStudioApp(t, baseDir, "inst_free_loop", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	starter.holdTurnOpen = true
	gate := make(chan struct{})
	starter.turnStartGate = gate
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_free_loop")

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Slow start"})
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Second task"})
	if message := readDevStudioMessage(t, connection); message["code"] != "turn_already_active" {
		t.Fatalf("message while Codex starts the turn = %v, want turn_already_active at once", message)
	}
	// Codex has the turn but has not named it yet.
	starter.waitForRequest(t, "turn/start")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.cancel"})
	time.Sleep(20 * time.Millisecond)
	close(gate)

	select {
	case <-starter.interruptReceived:
	case <-time.After(time.Second):
		t.Fatal("a cancel sent before Codex named the turn was never passed on")
	}
	if completed := readDevStudioEvent(t, connection, "turn.completed"); completed["status"] != "interrupted" {
		t.Fatalf("turn.completed = %v, want interrupted", completed)
	}
}

func TestDevStudioCodexAnswersEveryCodexRequest(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_requests")
	createDevStudioApp(t, baseDir, "inst_requests", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	starter.onTurnStart = func(process *fakeDevStudioCodexProcess) {
		process.ask("approval-7", "item/commandExecution/requestApproval", map[string]any{"threadId": "thread_fake", "turnId": "turn_fake", "itemId": "call_1"})
		process.ask("approval-8", "applyPatchApproval", map[string]any{"conversationId": "thread_fake"})
		process.ask(5, "currentTime/read", map[string]any{})
		// The recorded live question had the id 0.
		process.ask(0, "item/tool/requestUserInput", map[string]any{
			"threadId": "thread_fake", "turnId": "turn_fake", "itemId": "call_question",
			"questions": []map[string]any{{
				"id": "lead_fields", "header": "Lead Fields", "question": "Which fields matter most?", "isOther": true, "isSecret": false,
				"options": []map[string]any{{"label": "Contact and follow-up (Recommended)", "description": "Name, company, email."}},
			}, {
				"id": "notes", "header": "Notes", "question": "Anything else?", "isOther": true, "isSecret": false, "options": nil,
			}},
		})
	}
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_requests")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Add lead tracking."})

	events := readDevStudioEventsUntil(t, connection, "question.asked")
	if !hasDevStudioStatus(events, "waiting_for_answer") {
		t.Fatalf("events missing waiting_for_answer: %v", events)
	}
	asked := events[len(events)-1]
	wantQuestions := []any{
		map[string]any{"id": "lead_fields", "header": "Lead Fields", "question": "Which fields matter most?", "isOther": true, "isSecret": false,
			"options": []any{map[string]any{"label": "Contact and follow-up (Recommended)", "description": "Name, company, email."}}},
		map[string]any{"id": "notes", "header": "Notes", "question": "Anything else?", "isOther": true, "isSecret": false, "options": nil},
	}
	if asked["requestId"] != "0" || asked["itemId"] != "call_question" || !sameJSON(asked["questions"], wantQuestions) {
		t.Fatalf("question.asked = %v", asked)
	}
	if reply := starter.waitForReply(t, `"approval-7"`); reply.Result["decision"] != "decline" {
		t.Fatalf("command approval reply = %v, want decline", reply)
	}
	if reply := starter.waitForReply(t, `"approval-8"`); reply.Result["decision"] != "denied" {
		t.Fatalf("patch approval reply = %v, want denied", reply)
	}
	if reply := starter.waitForReply(t, "5"); reply.Error["code"] != float64(devStudioJSONRPCMethodNotFound) {
		t.Fatalf("unknown request reply = %v, want a JSON-RPC error", reply)
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "question.answer", "requestId": "0", "answers": map[string]any{"lead_fields": []string{"Contact and follow-up (Recommended)"}, "notes": []string{}}})
	if resolved := readDevStudioEvent(t, connection, "question.resolved"); resolved["requestId"] != "0" {
		t.Fatalf("question.resolved = %v", resolved)
	}
	reply := starter.waitForReply(t, "0")
	wantAnswer := map[string]any{"answers": map[string]any{
		"lead_fields": map[string]any{"answers": []any{"Contact and follow-up (Recommended)"}},
		"notes":       map[string]any{"answers": []any{}},
	}}
	if !sameJSON(reply.Result, wantAnswer) {
		t.Fatalf("question reply = %v, want %v", reply.Result, wantAnswer)
	}
	sendDevStudioMessage(t, connection, map[string]any{"type": "question.answer", "requestId": "0", "answers": map[string]any{}})
	if events := readDevStudioEventsUntil(t, connection, "error"); events[len(events)-1]["code"] != "unknown_question" {
		t.Fatalf("second answer = %v, want unknown_question", events)
	}

	starter.mu.Lock()
	process := starter.processes[0]
	starter.mu.Unlock()
	process.notify("serverRequest/resolved", map[string]any{"requestId": 0})
	process.completeTurn("completed", nil)
	for _, event := range readDevStudioEventsUntil(t, connection, "turn.completed") {
		if event["type"] == "question.resolved" {
			t.Fatalf("question resolved twice: %v", event)
		}
	}
}

func TestDevStudioCodexOpenQuestionResolvesWhenTurnEnds(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_open_question")
	createDevStudioApp(t, baseDir, "inst_open_question", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	starter.onTurnStart = func(process *fakeDevStudioCodexProcess) {
		process.ask("q-1", "item/tool/requestUserInput", map[string]any{"itemId": "call_q", "questions": []map[string]any{{"id": "q", "header": "Q", "question": "Which?", "options": nil}}})
	}
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_open_question")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Ask me."})
	if asked := readDevStudioEvent(t, connection, "question.asked"); asked["requestId"] != `"q-1"` {
		t.Fatalf("question.asked = %v", asked)
	}
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.cancel"})
	events := readDevStudioEventsUntil(t, connection, "turn.completed")
	if !hasDevStudioEvent(events, "question.resolved") || events[len(events)-1]["status"] != "interrupted" {
		t.Fatalf("events after cancel = %v", events)
	}
}

// Codex reports a dropped model stream with willRetry and goes on; only an
// error without it, followed by a failed turn, ends the turn.
func TestDevStudioCodexRetryingErrorDoesNotEndTurn(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_retry")
	createDevStudioApp(t, baseDir, "inst_retry", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	starter.onTurnStart = func(process *fakeDevStudioCodexProcess) {
		process.notify("error", map[string]any{"turnId": "turn_fake", "willRetry": true, "error": map[string]any{
			"message": "Reconnecting... 1/5", "codexErrorInfo": map[string]any{"responseStreamDisconnected": map[string]any{"httpStatusCode": nil}},
		}})
		process.item("item/completed", map[string]any{"type": "agentMessage", "id": "msg_1", "text": "Done after retry", "phase": "final_answer"})
		process.completeTurn("completed", nil)
	}
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_retry")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Go."})
	events := readDevStudioEventsUntil(t, connection, "turn.completed")

	var retry map[string]any
	for _, event := range events {
		if event["type"] == "error" {
			retry = event
		}
	}
	if retry == nil || retry["willRetry"] != true || retry["code"] != "responseStreamDisconnected" || retry["message"] != "Reconnecting... 1/5" {
		t.Fatalf("retry error = %v", retry)
	}
	if !hasDevStudioStatus(events, "retrying") || !hasDevStudioEvent(events, "message.completed") {
		t.Fatalf("turn did not continue after the retry: %v", events)
	}
	if completed := events[len(events)-1]; completed["status"] != "completed" {
		t.Fatalf("turn.completed = %v, want completed", completed)
	}
}

func TestDevStudioCodexForwardsTurnCompletionStatus(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		turnError map[string]any
		wantError map[string]any
	}{
		{"completed", "completed", nil, nil},
		{"interrupted", "interrupted", nil, nil},
		{
			"failed", "failed",
			map[string]any{"message": "Usage limit reached", "codexErrorInfo": "usageLimitExceeded", "additionalDetails": nil},
			map[string]any{"code": "usageLimitExceeded", "message": "Usage limit reached"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instanceID := "inst_status_" + test.name
			baseDir := setupTestProductionRoot(t, instanceID)
			createDevStudioApp(t, baseDir, instanceID, "app-one")
			starter := newFakeDevStudioCodexStarter(nil)
			starter.onTurnStart = func(process *fakeDevStudioCodexProcess) {
				process.completeTurn(test.status, test.turnError)
			}
			connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), instanceID)
			sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Go."})
			completed := readDevStudioEvent(t, connection, "turn.completed")
			if completed["status"] != test.status || completed["durationMs"] != float64(1234) {
				t.Fatalf("turn.completed = %v, want %s", completed, test.status)
			}
			if (test.wantError == nil && completed["error"] != nil) || (test.wantError != nil && !sameJSON(completed["error"], test.wantError)) {
				t.Fatalf("turn.completed error = %v, want %v", completed["error"], test.wantError)
			}
		})
	}
}

func TestDevStudioCodexResumesStoredConversationAndReplaysIt(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_resume")
	createDevStudioApp(t, baseDir, "inst_resume", "app-one")
	instanceRoot := filepath.Join(baseDir, "inst_resume")
	explanation := "Smallest change first"
	if err := writeDevStudioThreadRecord(&devStudioWorkspace{instanceID: "inst_resume", instanceRoot: instanceRoot}, devStudioThreadRecord{
		ThreadID: "thread_saved",
		Plan:     &devStudioPlan{Explanation: &explanation, Steps: []devStudioPlanStep{{Step: "Read the apps", Status: "completed"}}},
	}); err != nil {
		t.Fatalf("store conversation: %v", err)
	}
	starter := newFakeDevStudioCodexStarter(nil)
	starter.history = []map[string]any{{
		"id": "turn_old", "status": "completed", "durationMs": 900, "error": nil,
		"items": []map[string]any{
			{"type": "userMessage", "id": "item-1", "content": []map[string]any{{"type": "text", "text": "Hello"}}},
			{"type": "reasoning", "id": "rs_old", "summary": []string{"Looked around"}, "content": []string{}},
			{"type": "agentMessage", "id": "msg_old", "text": "Reading the apps.", "phase": "commentary"},
			{"type": "commandExecution", "id": "cmd_old", "command": "/bin/zsh -c 'ls apps'", "cwd": instanceRoot, "status": "completed",
				"commandActions": []map[string]any{{"type": "listFiles", "command": "ls apps", "path": "apps"}}, "aggregatedOutput": "app-one\n", "exitCode": 0, "durationMs": 3},
			{"type": "fileChange", "id": "fc_old", "status": "completed", "changes": []map[string]any{
				{"path": filepath.Join(instanceRoot, "apps", "app-one", "app.scl"), "kind": map[string]any{"type": "add"}, "diff": "+app"},
			}},
			{"type": "webSearch", "id": "ws_old", "query": "", "action": map[string]any{"type": "openPage", "url": "https://example.com/leads"}},
			{"type": "agentMessage", "id": "msg_final", "text": "Done.", "phase": nil},
		},
	}, {
		"id": "turn_cut", "status": "inProgress", "durationMs": nil, "error": nil, "items": []map[string]any{
			{"type": "userMessage", "id": "item-1", "content": []map[string]any{{"type": "text", "text": "Stopped"}}},
		},
	}}
	connection, ready := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_resume")
	if ready["threadId"] != "thread_saved" || ready["resumed"] != true {
		t.Fatalf("session.ready = %v, want the stored conversation resumed", ready)
	}
	resume, ok := starter.request("thread/resume")
	if !ok || resume.Params["threadId"] != "thread_saved" || resume.Params["config"] == nil {
		t.Fatalf("thread/resume params = %v", resume.Params)
	}
	if _, started := starter.request("thread/start"); started {
		t.Fatal("bridge started a new conversation instead of resuming the stored one")
	}

	wantTypes := []string{"user.message", "reasoning.completed", "message.completed", "command.completed", "files.changed", "research.step", "message.completed", "turn.completed", "user.message", "turn.completed", "plan.updated"}
	var replayed []map[string]any
	for len(replayed) < len(wantTypes) {
		replayed = append(replayed, readDevStudioMessage(t, connection))
	}
	for index, event := range replayed {
		if event["type"] != wantTypes[index] || event["replay"] != true {
			t.Fatalf("replayed event %d = %v, want %s with replay", index, event, wantTypes[index])
		}
	}
	if replayed[0]["itemId"] != "turn_old:item-1" || replayed[0]["text"] != "Hello" {
		t.Fatalf("replayed user.message = %v", replayed[0])
	}
	// Both turns stored an item-1; the turn id keeps the two apart.
	if replayed[8]["itemId"] != "turn_cut:item-1" || replayed[8]["text"] != "Stopped" {
		t.Fatalf("second turn's user.message = %v", replayed[8])
	}
	if plan := replayed[10]; plan["explanation"] != explanation || !sameJSON(plan["steps"], []any{map[string]any{"step": "Read the apps", "status": "completed"}}) {
		t.Fatalf("replayed plan = %v", plan)
	}
	// Shift the indexes below past the user message.
	replayed = replayed[1:]
	if replayed[2]["command"] != "ls apps" || !sameJSON(replayed[2]["actions"], []any{map[string]any{"type": "listFiles", "command": "ls apps", "path": "apps"}}) {
		t.Fatalf("replayed command = %v", replayed[2])
	}
	if changes := replayed[3]["changes"].([]any); changes[0].(map[string]any)["path"] != "apps/app-one/app.scl" || changes[0].(map[string]any)["kind"] != "add" {
		t.Fatalf("replayed files.changed = %v", replayed[3])
	}
	if replayed[4]["action"] != "open_page" || replayed[4]["url"] != "https://example.com/leads" {
		t.Fatalf("replayed research.step = %v", replayed[4])
	}
	if replayed[2]["commandId"] != "turn_old:cmd_old" {
		t.Fatalf("replayed command id = %v, want it prefixed with its turn id", replayed[2]["commandId"])
	}
	if replayed[6]["turnId"] != "turn_old" || replayed[6]["status"] != "completed" || replayed[8]["status"] != "interrupted" {
		t.Fatalf("replayed turn ends = %v, %v", replayed[6], replayed[8])
	}
}

func TestDevStudioCodexStartsNewConversationWhenStoredOneIsGone(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_gone")
	createDevStudioApp(t, baseDir, "inst_gone", "app-one")
	workspace := &devStudioWorkspace{instanceID: "inst_gone", instanceRoot: filepath.Join(baseDir, "inst_gone")}
	if err := writeDevStudioThreadID(workspace, "thread_deleted"); err != nil {
		t.Fatalf("store conversation: %v", err)
	}
	starter := newFakeDevStudioCodexStarter(nil)
	starter.failResume = true
	_, ready := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_gone")
	if ready["threadId"] != "thread_fake" || ready["resumed"] != false {
		t.Fatalf("session.ready = %v, want a new conversation", ready)
	}
	if stored, err := readDevStudioThreadID(workspace); err != nil || stored != "thread_fake" {
		t.Fatalf("stored conversation = %q, %v, want thread_fake", stored, err)
	}
}

// Two folders can hold the same instance; each keeps its own conversation.
func TestDevStudioCodexDoesNotResumeAnotherFoldersConversation(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_moved")
	createDevStudioApp(t, baseDir, "inst_moved", "app-one")
	instanceRoot := filepath.Join(baseDir, "inst_moved")
	if err := writeDevStudioThreadID(&devStudioWorkspace{instanceID: "inst_moved", instanceRoot: "/elsewhere/inst_moved"}, "thread_elsewhere"); err != nil {
		t.Fatalf("store conversation: %v", err)
	}
	starter := newFakeDevStudioCodexStarter(nil)
	_, ready := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_moved")
	if ready["threadId"] != "thread_fake" || ready["resumed"] != false {
		t.Fatalf("session.ready = %v, want a new conversation", ready)
	}
	if _, resumed := starter.request("thread/resume"); resumed {
		t.Fatal("the bridge resumed a conversation stored for another folder")
	}
	record, err := readDevStudioThreadRecord(&devStudioWorkspace{instanceID: "inst_moved", instanceRoot: instanceRoot})
	if err != nil || record.ThreadID != "thread_fake" || record.InstanceRoot != instanceRoot {
		t.Fatalf("stored record = %+v, %v, want this folder's new conversation", record, err)
	}
}

func TestDevStudioThreadPathStaysInTheCLIHome(t *testing.T) {
	cliHome := t.TempDir()
	t.Setenv("SIMPLE_CLI_HOME", cliHome)
	if path, err := devStudioThreadPath("inst_acme"); err != nil || path != filepath.Join(cliHome, ".simple", "devstudio", "inst_acme", "thread.json") {
		t.Fatalf("devStudioThreadPath = %q, %v", path, err)
	}
	for _, instanceID := range []string{"", ".", "..", "../outside", "a/b", `a\b`} {
		if path, err := devStudioThreadPath(instanceID); err == nil {
			t.Fatalf("devStudioThreadPath(%q) = %q, want an error", instanceID, path)
		}
	}
}

func TestDevStudioCodexConversationNewStartsFreshConversation(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_new_conversation")
	createDevStudioApp(t, baseDir, "inst_new_conversation", "app-one")
	workspace := &devStudioWorkspace{instanceID: "inst_new_conversation", instanceRoot: filepath.Join(baseDir, "inst_new_conversation")}
	starter := newFakeDevStudioCodexStarter(nil)
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_new_conversation")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Plan something."})
	readDevStudioEvent(t, connection, "turn.completed")
	if record, err := readDevStudioThreadRecord(workspace); err != nil || record.Plan == nil {
		t.Fatalf("stored record = %+v, %v, want a plan before conversation.new", record, err)
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "conversation.new"})
	if started := readDevStudioMessage(t, connection); started["type"] != "conversation.started" || started["threadId"] != "thread_fake_2" {
		t.Fatalf("answer to conversation.new = %v", started)
	}
	if record, err := readDevStudioThreadRecord(workspace); err != nil || record.ThreadID != "thread_fake_2" || record.Plan != nil {
		t.Fatalf("stored record = %+v, %v, want thread_fake_2 with no plan", record, err)
	}
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Fresh start."})
	readDevStudioEvent(t, connection, "turn.completed")
	starter.mu.Lock()
	lastTurn := starter.requests[len(starter.requests)-1]
	starter.mu.Unlock()
	if lastTurn.Method != "turn/start" || lastTurn.Params["threadId"] != "thread_fake_2" {
		t.Fatalf("last request = %v, want turn/start on the new conversation", lastTurn)
	}
}

func TestDevStudioCodexReportsUnreadableHistoryAfterReady(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_no_history")
	createDevStudioApp(t, baseDir, "inst_no_history", "app-one")
	if err := writeDevStudioThreadID(&devStudioWorkspace{instanceID: "inst_no_history", instanceRoot: filepath.Join(baseDir, "inst_no_history")}, "thread_saved"); err != nil {
		t.Fatalf("store conversation: %v", err)
	}
	starter := newFakeDevStudioCodexStarter(nil)
	starter.failRead = true
	connection, ready := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_no_history")
	if ready["resumed"] != true {
		t.Fatalf("session.ready = %v, want resumed", ready)
	}
	if message := readDevStudioMessage(t, connection); message["code"] != "history_unavailable" {
		t.Fatalf("message after ready = %v, want history_unavailable", message)
	}
}

func TestDevStudioCodexIgnoresOtherConversations(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_helper")
	createDevStudioApp(t, baseDir, "inst_helper", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	starter.onTurnStart = func(process *fakeDevStudioCodexProcess) {
		// A helper agent runs in its own Codex conversation.
		process.notify("item/completed", map[string]any{"threadId": "thread_helper", "turnId": "turn_helper", "item": map[string]any{"type": "agentMessage", "id": "msg_helper", "text": "helper", "phase": nil}})
		process.notify("turn/completed", map[string]any{"threadId": "thread_helper", "turn": map[string]any{"id": "turn_helper", "status": "completed"}})
		process.item("item/completed", map[string]any{"type": "agentMessage", "id": "msg_main", "text": "main", "phase": nil})
		process.completeTurn("completed", nil)
	}
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_helper")
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Delegate."})
	events := readDevStudioEventsUntil(t, connection, "turn.completed")
	for _, event := range events {
		if event["itemId"] == "msg_helper" {
			t.Fatalf("helper conversation reached the browser: %v", event)
		}
	}
	if !hasDevStudioEvent(events, "message.completed") || events[len(events)-1]["turnId"] != "turn_fake" {
		t.Fatalf("events = %v, want the main turn to finish itself", events)
	}
}

func TestDevStudioCodexReportsFailuresAfterReady(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_later_failures")
	createDevStudioApp(t, baseDir, "inst_later_failures", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_later_failures")

	starter.mu.Lock()
	starter.failThreadStart = true
	starter.mu.Unlock()
	sendDevStudioMessage(t, connection, map[string]any{"type": "conversation.new"})
	if message := readDevStudioMessage(t, connection); message["code"] != "codex_unavailable" {
		t.Fatalf("failed conversation.new = %v, want codex_unavailable", message)
	}

	// Codex stops while idle, then cannot start again for the next turn.
	starter.mu.Lock()
	starter.failInitialize = true
	process := starter.processes[0]
	starter.mu.Unlock()
	process.Stop()
	deadline := time.Now().Add(time.Second)
	for !process.isStopped() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Go."})
	assertDevStudioTurnFailed(t, readDevStudioEventsUntil(t, connection, "turn.completed"), "codex_unavailable")
	if starter.startCount() != 2 {
		t.Fatalf("Codex starts = %d, want a restart attempt", starter.startCount())
	}
}

func TestDevStudioCodexRejectsMalformedBrowserMessages(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_malformed_browser")
	createDevStudioApp(t, baseDir, "inst_malformed_browser", "app-one")
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(newFakeDevStudioCodexStarter(nil)), "inst_malformed_browser")

	// A cancel with nothing running is answered with nothing; the next
	// message's error proves no reply was queued for it.
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.cancel"})
	tests := []struct {
		name    string
		payload string
		code    string
	}{
		{"not JSON", `not json`, "invalid_message"},
		{"no type", `{}`, "invalid_message"},
		{"type not a string", `{"type":5}`, "invalid_message"},
		{"turn without text", `{"type":"turn.start"}`, "invalid_message"},
		{"turn with blank text", `{"type":"turn.start","text":"  "}`, "invalid_turn"},
		{"cancel with extra field", `{"type":"turn.cancel","now":true}`, "invalid_message"},
		{"answer without request id", `{"type":"question.answer","requestId":"","answers":{}}`, "invalid_message"},
		{"answer not a list", `{"type":"question.answer","requestId":"0","answers":{"q":"x"}}`, "invalid_message"},
		{"answer missing answers", `{"type":"question.answer","requestId":"0","extra":{}}`, "invalid_message"},
		{"answer to no question", `{"type":"question.answer","requestId":"7","answers":{}}`, "unknown_question"},
		{"new conversation with extra field", `{"type":"conversation.new","now":true}`, "invalid_message"},
		{"unknown type", `{"type":"files.delete"}`, "unsupported_message_type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := connection.WriteMessage(websocket.TextMessage, []byte(test.payload)); err != nil {
				t.Fatalf("write message: %v", err)
			}
			if message := readDevStudioMessage(t, connection); message["type"] != "error" || message["code"] != test.code || message["willRetry"] != false {
				t.Fatalf("reply = %v, want %s", message, test.code)
			}
		})
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, []byte("{}")); err != nil {
		t.Fatalf("write binary message: %v", err)
	}
	if message := readDevStudioMessage(t, connection); message["code"] != "invalid_message" {
		t.Fatalf("binary message reply = %v, want invalid_message", message)
	}
}

func TestDevStudioCodexClosedSessionRefusesWork(t *testing.T) {
	adapter := newDevStudioCodexAdapter(newFakeDevStudioCodexStarter(nil), "inst", &devStudioWorkspace{instanceRoot: t.TempDir()}, "key", nil)
	adapter.close()
	if _, _, err := adapter.connect(); !errors.Is(err, errDevStudioSessionClosed) {
		t.Fatalf("connect after close = %v", err)
	}
	for name, err := range map[string]error{
		"turn":         adapter.startTurn("hello"),
		"cancel":       adapter.cancelTurn(),
		"answer":       adapter.answerQuestion("0", map[string][]string{}),
		"conversation": adapter.newConversation(),
	} {
		if code := devStudioAdapterError(err); code == nil || code.Code != "session_closed" {
			t.Fatalf("%s after close = %v, want session_closed", name, code)
		}
	}
}

func TestDevStudioUnwrapShellCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"single quoted", `/bin/zsh -c 'ls apps; cat .simple/context/workflows.md'`, `ls apps; cat .simple/context/workflows.md`},
		{"double quoted with inner quotes", `/bin/zsh -c "devbox run -- sh -c 'ls apps'"`, `devbox run -- sh -c 'ls apps'`},
		{"login shell with escaped quote", `/bin/bash -lc 'echo '\''hi'\'''`, `echo 'hi'`},
		{"escapes inside double quotes", `bash -lc "echo \"\$HOME\" \\ done"`, `echo "$HOME" \ done`},
		{"multiline", "/bin/zsh -c 'cat <<EOF\nline\nEOF'", "cat <<EOF\nline\nEOF"},
		{"no wrapper", `simple build apps/app-one`, `simple build apps/app-one`},
		{"two words after -c", `/bin/zsh -c 'ls' 'apps'`, `/bin/zsh -c 'ls' 'apps'`},
		{"unterminated quote", `/bin/zsh -c 'ls apps`, `/bin/zsh -c 'ls apps`},
		{"unterminated double quote", `/bin/zsh -c "ls apps`, `/bin/zsh -c "ls apps`},
		{"empty command", `/bin/zsh -c ''`, `/bin/zsh -c ''`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := devStudioUnwrapShellCommand(test.command); got != test.want {
				t.Fatalf("devStudioUnwrapShellCommand(%q) = %q, want %q", test.command, got, test.want)
			}
		})
	}
}

func TestDevStudioCommandStatusFromActions(t *testing.T) {
	tests := []struct {
		name    string
		actions []map[string]any
		want    string
	}{
		{"no actions", nil, "running_command"},
		{"looking around", []map[string]any{{"type": "listFiles", "path": "apps"}, {"type": "search", "path": nil}}, "inspecting_workspace"},
		{"reading instructions", []map[string]any{{"type": "listFiles", "path": "apps"}, {"type": "read", "path": ".simple/context/workflows.md"}}, "reading_instructions"},
		{"reading a record", []map[string]any{{"type": "read", "path": "apps/app-one/app.scl"}}, "inspecting_workspace"},
		{"running something", []map[string]any{{"type": "read", "path": "apps/app-one/AGENTS.md"}, {"type": "unknown"}}, "running_command"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := devStudioCommandStatus(test.actions); got != test.want {
				t.Fatalf("devStudioCommandStatus = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDevStudioCodexCommandActionsUseInstanceRelativePaths(t *testing.T) {
	root := "/workspace/acme"
	adapter := newDevStudioCodexAdapter(nil, "acme", &devStudioWorkspace{instanceRoot: root}, "secret_key", nil)
	query := "invoice secret_key"
	summary := adapter.commandSummary(devStudioCodexItem{
		Command: "/bin/zsh -c 'rg invoice apps; cat tables.scl'",
		Cwd:     root + "/apps/app-one",
		CommandActions: []devStudioCodexCommandAction{
			{Type: "search", Command: "rg invoice", Query: &query, Path: stringPointer("records")},
			{Type: "read", Command: "cat tables.scl", Name: "tables.scl", Path: stringPointer(root + "/apps/app-one/tables.scl")},
			{Type: "read", Command: "cat ~/.codex/AGENTS.md", Name: "AGENTS.md", Path: stringPointer("/Users/someone/.codex/AGENTS.md")},
			{Type: "listFiles", Command: "ls"},
			{Type: "somethingNew", Command: "make"},
		},
	})
	want := []any{
		map[string]any{"type": "search", "command": "rg invoice", "query": "invoice [redacted]", "path": "apps/app-one/records"},
		map[string]any{"type": "read", "command": "cat tables.scl", "name": "tables.scl", "path": "apps/app-one/tables.scl"},
		map[string]any{"type": "read", "command": "cat ~/.codex/AGENTS.md", "name": "AGENTS.md", "path": nil},
		map[string]any{"type": "listFiles", "command": "ls", "path": nil},
		map[string]any{"type": "unknown", "command": "make"},
	}
	if summary.command != "rg invoice apps; cat tables.scl" || !sameJSON(summary.actions, want) {
		t.Fatalf("command summary = %q, %v", summary.command, summary.actions)
	}
}

func TestDevStudioCodexFileChangesAreRelativeAndCapped(t *testing.T) {
	root := "/workspace/acme"
	adapter := newDevStudioCodexAdapter(nil, "acme", &devStudioWorkspace{instanceRoot: root}, "secret_key", nil)
	long := strings.Repeat("é", devStudioDiffLimit+5)
	var moved devStudioCodexFileChange
	moved.Path, moved.Diff, moved.Kind.Type, moved.Kind.MovePath = root+"/apps/a/old.scl", "-a secret_key", "update", stringPointer(root+"/apps/a/new.scl")
	var big devStudioCodexFileChange
	big.Path, big.Diff, big.Kind.Type = "apps/a/big.scl", long, "add"
	var outside devStudioCodexFileChange
	outside.Path, outside.Kind.Type = "/etc/hosts", "delete"

	changes := adapter.fileChanges([]devStudioCodexFileChange{moved, big, outside})
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want the two inside the instance", changes)
	}
	if changes[0]["path"] != "apps/a/big.scl" || changes[0]["diffTruncated"] != true || len([]rune(changes[0]["diff"].(string))) != devStudioDiffLimit {
		t.Fatalf("large change = path %v, truncated %v", changes[0]["path"], changes[0]["diffTruncated"])
	}
	if changes[1]["path"] != "apps/a/old.scl" || changes[1]["movePath"] != "apps/a/new.scl" || changes[1]["diff"] != "-a [redacted]" || changes[1]["diffTruncated"] != false {
		t.Fatalf("moved change = %v", changes[1])
	}
}

func TestDevStudioSessionCreatesMissingSimpleSCLWithDeployKey(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_scl")
	createDevStudioApp(t, baseDir, "inst_scl", "app-one")
	sclPath := filepath.Join(baseDir, "inst_scl", "simple.scl")
	if err := os.Remove(sclPath); err != nil {
		t.Fatalf("remove simple.scl: %v", err)
	}
	bridge := newDevStudioBridgeWithCodexStarter(newFakeDevStudioCodexStarter(nil))
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer func() { _ = connection.Close() }()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_scl"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q", message["type"])
	}
	contents, err := os.ReadFile(sclPath)
	if err != nil {
		t.Fatalf("read simple.scl: %v", err)
	}
	want := "tenant inst_scl\n\nenv devstudio {\n  endpoint \"inst_scl.simple.lcl\"\n  api_key \"test_deploy_key_secret\"\n}\n"
	if string(contents) != want {
		t.Fatalf("simple.scl = %q, want %q", contents, want)
	}
}

func TestDevStudioSessionAddsDevStudioEnvironmentToExistingSimpleSCL(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_existing_scl")
	createDevStudioApp(t, baseDir, "inst_existing_scl", "app-one")
	sclPath := filepath.Join(baseDir, "inst_existing_scl", "simple.scl")
	original := "tenant preserved\n\nenv dev {\n  endpoint preserved.example\n  api_key $PRESERVED_KEY\n}\n"
	if err := os.WriteFile(sclPath, []byte(original), 0644); err != nil {
		t.Fatalf("write simple.scl: %v", err)
	}
	bridge := newDevStudioBridgeWithCodexStarter(newFakeDevStudioCodexStarter(nil))
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer func() { _ = connection.Close() }()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_existing_scl"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q", message["type"])
	}
	contents, err := os.ReadFile(sclPath)
	if err != nil {
		t.Fatalf("read simple.scl: %v", err)
	}
	want := original + "\nenv devstudio {\n  endpoint \"inst_existing_scl.simple.lcl\"\n  api_key \"test_deploy_key_secret\"\n}\n"
	if string(contents) != want {
		t.Fatalf("simple.scl = %q, want %q", contents, want)
	}
}

func TestDevStudioSessionUpdatesExistingDevStudioEnvironment(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_existing_devstudio")
	createDevStudioApp(t, baseDir, "inst_existing_devstudio", "app-one")
	sclPath := filepath.Join(baseDir, "inst_existing_devstudio", "simple.scl")
	original := "tenant preserved\n\nenv devstudio {\n  endpoint old.example\n  api_key $OLD_KEY\n}\n\nenv prod {\n  endpoint prod.example\n}\n"
	if err := os.WriteFile(sclPath, []byte(original), 0644); err != nil {
		t.Fatalf("write simple.scl: %v", err)
	}
	bridge := newDevStudioBridgeWithCodexStarter(newFakeDevStudioCodexStarter(nil))
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer func() { _ = connection.Close() }()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_existing_devstudio"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q", message["type"])
	}
	contents, err := os.ReadFile(sclPath)
	if err != nil {
		t.Fatalf("read simple.scl: %v", err)
	}
	want := "tenant preserved\n\nenv devstudio {\n  endpoint \"inst_existing_devstudio.simple.lcl\"\n  api_key \"test_deploy_key_secret\"\n}\n\nenv prod {\n  endpoint prod.example\n}\n"
	if string(contents) != want {
		t.Fatalf("simple.scl = %q, want %q", contents, want)
	}
}

func TestDevStudioCodexEnvironmentUsesOnlyCurrentDevStudioVariables(t *testing.T) {
	t.Setenv("HOME", "/codex/home")
	t.Setenv("SIMPLE_CLI_HOME", "/read-only/cli-home")
	t.Setenv("SIMPLE_LOCAL_API_KEY", "old-key")
	t.Setenv("SIMPLE_INSTANCE_ID", "old-instance")
	environment := devStudioCodexEnvironment("/workspace/simple-devstudio/inst_current", "inst_current", "current-key")
	var home, cliHome, localAPIKey, instanceID []string
	for _, item := range environment {
		if strings.HasPrefix(item, "HOME=") {
			home = append(home, item)
		}
		if strings.HasPrefix(item, "SIMPLE_CLI_HOME=") {
			cliHome = append(cliHome, item)
		}
		if strings.HasPrefix(item, "SIMPLE_LOCAL_API_KEY=") {
			localAPIKey = append(localAPIKey, item)
		}
		if strings.HasPrefix(item, "SIMPLE_INSTANCE_ID=") {
			instanceID = append(instanceID, item)
		}
	}
	if !sameStrings(home, []string{"HOME=/codex/home"}) {
		t.Fatalf("HOME entries = %v", home)
	}
	if !sameStrings(cliHome, []string{"SIMPLE_CLI_HOME=/workspace/simple-devstudio/inst_current"}) {
		t.Fatalf("SIMPLE_CLI_HOME entries = %v", cliHome)
	}
	if !sameStrings(localAPIKey, []string{"SIMPLE_LOCAL_API_KEY=current-key"}) {
		t.Fatalf("SIMPLE_LOCAL_API_KEY entries = %v", localAPIKey)
	}
	if !sameStrings(instanceID, []string{"SIMPLE_INSTANCE_ID=inst_current"}) {
		t.Fatalf("SIMPLE_INSTANCE_ID entries = %v", instanceID)
	}
}

func TestDevStudioCodexEnvironmentIncludesCurrentCLIExecutableDirectory(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve current executable: %v", err)
	}
	wantPrefix := "PATH=" + filepath.Dir(executable)
	for _, item := range devStudioCodexEnvironment("/workspace/simple-devstudio/acme", "acme", "current-key") {
		if strings.HasPrefix(item, wantPrefix) {
			return
		}
	}
	t.Fatalf("Codex environment does not include executable directory prefix %q", wantPrefix)
}

func TestDevStudioProtectedFilesAreRestoredAfterTurn(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_protected")
	appDir := createDevStudioApp(t, baseDir, "inst_protected", "app-one")
	protectedPath := filepath.Join(appDir, "AGENTS.md")
	if err := os.WriteFile(protectedPath, []byte("original instructions\n"), 0644); err != nil {
		t.Fatalf("write protected file: %v", err)
	}
	workspace, err := resolveDevStudioWorkspace("inst_protected")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	snapshot, err := snapshotDevStudioProtectedFiles(workspace)
	if err != nil {
		t.Fatalf("snapshot protected files: %v", err)
	}
	if err := os.WriteFile(protectedPath, []byte("changed instructions\n"), 0644); err != nil {
		t.Fatalf("mutate protected file: %v", err)
	}
	changed, err := restoreChangedDevStudioProtectedFiles(workspace, snapshot)
	if err != nil {
		t.Fatalf("restore protected files: %v", err)
	}
	if !sameStrings(changed, []string{"apps/app-one/AGENTS.md"}) {
		t.Fatalf("changed protected paths = %v", changed)
	}
	contents, err := os.ReadFile(protectedPath)
	if err != nil || string(contents) != "original instructions\n" {
		t.Fatalf("restored protected file = %q, %v", contents, err)
	}
}

func TestDevStudioProtectedFilesPreservesNewProtectedFilesAndReportsThem(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_new_protected")
	instanceRoot := filepath.Join(baseDir, "inst_new_protected")
	appDir := createDevStudioApp(t, baseDir, "inst_new_protected", "app-one")

	// Pre-existing protected files
	appAgents := filepath.Join(appDir, "AGENTS.md")
	if err := os.WriteFile(appAgents, []byte("app agents v1\n"), 0644); err != nil {
		t.Fatalf("write app agents: %v", err)
	}
	agentDir := filepath.Join(instanceRoot, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("mkdir .agent: %v", err)
	}
	rootRules := filepath.Join(agentDir, "rules.md")
	if err := os.WriteFile(rootRules, []byte("rules v1\n"), 0644); err != nil {
		t.Fatalf("write rules: %v", err)
	}

	workspace, err := resolveDevStudioWorkspace("inst_new_protected")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	snapshot, err := snapshotDevStudioProtectedFiles(workspace)
	if err != nil {
		t.Fatalf("snapshot protected files: %v", err)
	}

	// 1. Modify pre-existing app AGENTS.md
	if err := os.WriteFile(appAgents, []byte("app agents modified\n"), 0644); err != nil {
		t.Fatalf("modify app agents: %v", err)
	}
	// 2. Delete pre-existing .agent/rules.md
	if err := os.Remove(rootRules); err != nil {
		t.Fatalf("delete rules: %v", err)
	}
	// 3. Create newly created protected instruction files
	newAppAgent := filepath.Join(appDir, "AGENT.md")
	if err := os.WriteFile(newAppAgent, []byte("new app agent instructions\n"), 0644); err != nil {
		t.Fatalf("write new app agent: %v", err)
	}
	skillDir := filepath.Join(appDir, "skills", "custom")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	newSkill := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(newSkill, []byte("new skill content\n"), 0644); err != nil {
		t.Fatalf("write new skill: %v", err)
	}
	contextDir := filepath.Join(instanceRoot, ".simple", "context")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatalf("mkdir context: %v", err)
	}
	newContext := filepath.Join(contextDir, "overview.md")
	if err := os.WriteFile(newContext, []byte("context overview\n"), 0644); err != nil {
		t.Fatalf("write new context: %v", err)
	}

	changed, err := restoreChangedDevStudioProtectedFiles(workspace, snapshot)
	if err != nil {
		t.Fatalf("restore protected files: %v", err)
	}

	wantChanged := []string{
		".agent/rules.md",
		".simple/context/overview.md",
		"apps/app-one/AGENT.md",
		"apps/app-one/AGENTS.md",
		"apps/app-one/skills/custom/SKILL.md",
	}
	if !sameStrings(changed, wantChanged) {
		t.Fatalf("changed paths = %v, want %v", changed, wantChanged)
	}

	// Verify modified pre-existing file was restored
	if contents, err := os.ReadFile(appAgents); err != nil || string(contents) != "app agents v1\n" {
		t.Fatalf("app AGENTS.md not restored properly: %q, %v", contents, err)
	}
	// Verify deleted pre-existing file was restored
	if contents, err := os.ReadFile(rootRules); err != nil || string(contents) != "rules v1\n" {
		t.Fatalf(".agent/rules.md not restored properly: %q, %v", contents, err)
	}
	// Verify newly created protected files were NOT deleted
	if contents, err := os.ReadFile(newAppAgent); err != nil || string(contents) != "new app agent instructions\n" {
		t.Fatalf("new AGENT.md was unexpectedly deleted or corrupted: %q, %v", contents, err)
	}
	if contents, err := os.ReadFile(newSkill); err != nil || string(contents) != "new skill content\n" {
		t.Fatalf("new SKILL.md was unexpectedly deleted or corrupted: %q, %v", contents, err)
	}
	if contents, err := os.ReadFile(newContext); err != nil || string(contents) != "context overview\n" {
		t.Fatalf("new context overview.md was unexpectedly deleted or corrupted: %q, %v", contents, err)
	}
}

func TestDevStudioProtectedFilesRestoreRecreatesParentDirectoriesAndPreservesModes(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_recreate_parents")
	instanceRoot := filepath.Join(baseDir, "inst_recreate_parents")
	appDir1 := createDevStudioApp(t, baseDir, "inst_recreate_parents", "app-one")
	appDir2 := createDevStudioApp(t, baseDir, "inst_recreate_parents", "app-two")

	files := []struct {
		relPath  string
		contents string
		mode     os.FileMode
	}{
		{
			relPath:  ".agent/rules.md",
			contents: "agent rules v1\n",
			mode:     0600,
		},
		{
			relPath:  ".simple/context/overview.md",
			contents: "context overview v1\n",
			mode:     0644,
		},
		{
			relPath:  "apps/app-one/AGENTS.md",
			contents: "app-one agents v1\n",
			mode:     0644,
		},
		{
			relPath:  "apps/app-one/skills/custom/SKILL.md",
			contents: "app-one skill v1\n",
			mode:     0755,
		},
		{
			relPath:  "apps/app-two/skills/nested/deep/SKILL.md",
			contents: "app-two deep skill v1\n",
			mode:     0640,
		},
	}

	for _, f := range files {
		absPath := filepath.Join(instanceRoot, f.relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("mkdir for %s: %v", f.relPath, err)
		}
		if err := os.WriteFile(absPath, []byte(f.contents), f.mode); err != nil {
			t.Fatalf("write %s: %v", f.relPath, err)
		}
		if err := os.Chmod(absPath, f.mode); err != nil {
			t.Fatalf("chmod %s: %v", f.relPath, err)
		}
	}

	workspace, err := resolveDevStudioWorkspace("inst_recreate_parents")
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}

	snapshot, err := snapshotDevStudioProtectedFiles(workspace)
	if err != nil {
		t.Fatalf("snapshot protected files: %v", err)
	}
	if len(snapshot) != len(files) {
		t.Fatalf("snapshot length = %d, want %d", len(snapshot), len(files))
	}

	// 1. Delete .agent parent directory
	if err := os.RemoveAll(filepath.Join(instanceRoot, ".agent")); err != nil {
		t.Fatalf("remove .agent dir: %v", err)
	}
	// 2. Delete .simple/context parent directory
	if err := os.RemoveAll(filepath.Join(instanceRoot, ".simple", "context")); err != nil {
		t.Fatalf("remove .simple/context dir: %v", err)
	}
	// 3. Delete app-one skills parent directory
	if err := os.RemoveAll(filepath.Join(appDir1, "skills")); err != nil {
		t.Fatalf("remove app-one skills dir: %v", err)
	}
	// 4. Delete entire app-two directory
	if err := os.RemoveAll(appDir2); err != nil {
		t.Fatalf("remove app-two dir: %v", err)
	}
	// 5. Mutate app-one AGENTS.md in-place
	if err := os.WriteFile(filepath.Join(appDir1, "AGENTS.md"), []byte("mutated instructions\n"), 0644); err != nil {
		t.Fatalf("mutate app-one AGENTS.md: %v", err)
	}

	// 6. Create a newly created protected file in app-one
	newSkillDir := filepath.Join(appDir1, "skills", "newskill")
	if err := os.MkdirAll(newSkillDir, 0755); err != nil {
		t.Fatalf("mkdir new skill dir: %v", err)
	}
	newSkillPath := filepath.Join(newSkillDir, "SKILL.md")
	if err := os.WriteFile(newSkillPath, []byte("brand new skill\n"), 0644); err != nil {
		t.Fatalf("write new skill: %v", err)
	}

	changed, err := restoreChangedDevStudioProtectedFiles(workspace, snapshot)
	if err != nil {
		t.Fatalf("restore protected files: %v", err)
	}

	wantChanged := []string{
		".agent/rules.md",
		".simple/context/overview.md",
		"apps/app-one/AGENTS.md",
		"apps/app-one/skills/custom/SKILL.md",
		"apps/app-one/skills/newskill/SKILL.md",
		"apps/app-two/skills/nested/deep/SKILL.md",
	}
	if !sameStrings(changed, wantChanged) {
		t.Fatalf("changed paths = %v, want %v", changed, wantChanged)
	}

	// Verify all pre-existing files were restored with correct content, mode, and parent directory
	for _, f := range files {
		absPath := filepath.Join(instanceRoot, f.relPath)
		parentDir := filepath.Dir(absPath)
		parentInfo, err := os.Stat(parentDir)
		if err != nil {
			t.Fatalf("parent directory missing for %s: %v", f.relPath, err)
		}
		if !parentInfo.IsDir() {
			t.Fatalf("parent path for %s is not a directory", f.relPath)
		}

		contents, err := os.ReadFile(absPath)
		if err != nil {
			t.Fatalf("read restored file %s: %v", f.relPath, err)
		}
		if string(contents) != f.contents {
			t.Fatalf("restored content for %s = %q, want %q", f.relPath, string(contents), f.contents)
		}

		fileInfo, err := os.Stat(absPath)
		if err != nil {
			t.Fatalf("stat restored file %s: %v", f.relPath, err)
		}
		if fileInfo.Mode().Perm() != f.mode {
			t.Fatalf("restored mode for %s = %o, want %o", f.relPath, fileInfo.Mode().Perm(), f.mode)
		}
	}

	// Verify newly created protected file was preserved
	newContents, err := os.ReadFile(newSkillPath)
	if err != nil || string(newContents) != "brand new skill\n" {
		t.Fatalf("new skill content = %q, %v, want brand new skill\\n", newContents, err)
	}
}

func TestDevStudioCodexTurnRestoresProtectedFilesWhenParentDirectoryDeleted(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_turn_del_parent")
	instanceRoot := filepath.Join(baseDir, "inst_turn_del_parent")
	_ = createDevStudioApp(t, baseDir, "inst_turn_del_parent", "app-one")

	agentDir := filepath.Join(instanceRoot, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("mkdir .agent: %v", err)
	}
	rulesFile := filepath.Join(agentDir, "rules.md")
	if err := os.WriteFile(rulesFile, []byte("original rules\n"), 0600); err != nil {
		t.Fatalf("write rules.md: %v", err)
	}
	if err := os.Chmod(rulesFile, 0600); err != nil {
		t.Fatalf("chmod rules.md: %v", err)
	}

	starter := newFakeDevStudioCodexStarter(nil)
	starter.onTurnStart = func(p *fakeDevStudioCodexProcess) {
		_ = os.RemoveAll(agentDir)
		p.Stop()
	}
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_turn_del_parent")

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Delete agent directory"})
	events := readDevStudioEventsUntil(t, connection, "turn.completed")
	assertDevStudioTurnFailed(t, events, "protected_files_modified")

	contents, err := os.ReadFile(rulesFile)
	if err != nil || string(contents) != "original rules\n" {
		t.Fatalf("restored rules.md = %q, %v, want original rules", string(contents), err)
	}
	info, err := os.Stat(rulesFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("restored rules.md mode = %o, %v, want 0600", info.Mode().Perm(), err)
	}
}

func TestDevStudioCodexFailsPendingCallsImmediatelyOnUnexpectedExit(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_exit_test")
	createDevStudioApp(t, baseDir, "inst_exit_test", "app-one")

	starter := newFakeDevStudioCodexStarter(nil)
	starter.exitOnMethod = "turn/start"
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_exit_test")

	start := time.Now()
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Run a task that exits unexpectedly."})
	events := readDevStudioEventsUntil(t, connection, "turn.completed")
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Fatalf("pending call took %v to fail; expected immediate failure without waiting for timeout", elapsed)
	}
	assertDevStudioTurnFailed(t, events, "codex_unavailable")
}

func TestDevStudioCodexAdapterCallFailsImmediatelyOnOutputClose(t *testing.T) {
	starter := newFakeDevStudioCodexStarter(nil)
	proc, err := starter.Start(devStudioCodexStartConfig{
		WorkspaceRoot: "/tmp",
		InstanceID:    "inst_test",
		DeployKey:     "test_key",
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	adapter := newDevStudioCodexAdapter(starter, "inst_test", &devStudioWorkspace{instanceRoot: "/tmp"}, "test_key", nil)
	if _, err := adapter.attach(proc); err != nil {
		t.Fatalf("attach process: %v", err)
	}

	errCh := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := adapter.call("custom/unhandledMethod", map[string]any{"key": "val"})
		errCh <- err
	}()

	// Wait briefly for call to be registered in pending
	time.Sleep(20 * time.Millisecond)

	// Close output unexpectedly
	proc.Stop()

	select {
	case callErr := <-errCh:
		elapsed := time.Since(start)
		if elapsed >= 2*time.Second {
			t.Fatalf("call took %v to fail; expected immediate failure", elapsed)
		}
		if callErr == nil {
			t.Fatal("expected error from call on output close, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("adapter.call did not fail within 3 seconds of output close")
	}
}

func TestDevStudioCodexProcessExitRestoresProtectedFilesAndRecoversOnLaterTurn(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_exit_recover")
	appDir := createDevStudioApp(t, baseDir, "inst_exit_recover", "app-one")
	agentsFile := filepath.Join(appDir, "AGENTS.md")
	if err := os.WriteFile(agentsFile, []byte("original instructions\n"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	starter := newFakeDevStudioCodexStarter(nil)
	starter.onTurnStart = func(p *fakeDevStudioCodexProcess) {
		_ = os.WriteFile(agentsFile, []byte("mutated protected content\n"), 0644)
		p.Stop()
	}
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_exit_recover")

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn 1"})
	assertDevStudioTurnFailed(t, readDevStudioEventsUntil(t, connection, "turn.completed"), "protected_files_modified")

	contents, err := os.ReadFile(agentsFile)
	if err != nil || string(contents) != "original instructions\n" {
		t.Fatalf("restored AGENTS.md = %q, want original instructions", string(contents))
	}

	starter.mu.Lock()
	if len(starter.processes) != 1 || !starter.processes[0].isStopped() {
		starter.mu.Unlock()
		t.Fatal("expected first process to be stopped")
	}
	starter.onTurnStart = nil
	starter.mu.Unlock()

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn 2"})
	if completed := readDevStudioEvent(t, connection, "turn.completed"); completed["status"] != "completed" {
		t.Fatalf("turn 2 = %v, want completed", completed)
	}
	if starter.startCount() != 2 {
		t.Fatalf("Codex start count = %d, want 2", starter.startCount())
	}
	// The restarted Codex continues the same conversation.
	if resume, ok := starter.request("thread/resume"); !ok || resume.Params["threadId"] != "thread_fake" {
		t.Fatalf("restart did not resume the conversation: %v", starter.requestMethods())
	}

	contents, err = os.ReadFile(agentsFile)
	if err != nil || string(contents) != "original instructions\n" {
		t.Fatalf("AGENTS.md after turn 2 = %q, want original instructions", string(contents))
	}
}

func TestDevStudioCodexStartupFailureClosesSessionAndNextConnectionRetries(t *testing.T) {
	tests := []struct {
		name  string
		fail  func(starter *fakeDevStudioCodexStarter, failing bool)
		state string
	}{
		{"initialize fails", func(starter *fakeDevStudioCodexStarter, failing bool) { starter.failInitialize = failing }, "initialize"},
		{"thread start fails", func(starter *fakeDevStudioCodexStarter, failing bool) { starter.failThreadStart = failing }, "thread/start"},
		{"thread start has no id", func(starter *fakeDevStudioCodexStarter, failing bool) { starter.malformedThreadStart = failing }, "thread/start"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instanceID := "inst_startup_" + string(rune('a'+index))
			baseDir := setupTestProductionRoot(t, instanceID)
			createDevStudioApp(t, baseDir, instanceID, "app-one")
			starter := newFakeDevStudioCodexStarter(nil)
			test.fail(starter, true)
			server := startDevStudioBridgeServer(t, newDevStudioBridgeWithCodexStarter(starter))
			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + devStudioWebSocketPath

			first := dialDevStudio(t, wsURL)
			defer func() { _ = first.Close() }()
			sendDevStudioMessage(t, first, sessionConnectMessage(instanceID))
			if message := readDevStudioMessage(t, first); message["type"] != "error" || message["code"] != "codex_unavailable" || message["willRetry"] != false {
				t.Fatalf("connect while Codex fails at %s = %v", test.state, message)
			}
			if _, _, err := first.ReadMessage(); err == nil {
				t.Fatal("the bridge kept the socket open after a startup error")
			}
			starter.mu.Lock()
			stopped := len(starter.processes) == 1 && starter.processes[0].isStopped()
			test.fail(starter, false)
			starter.mu.Unlock()
			if !stopped {
				t.Fatal("expected the failed Codex process to be stopped")
			}

			second := dialDevStudio(t, wsURL)
			defer func() { _ = second.Close() }()
			sendDevStudioMessage(t, second, sessionConnectMessage(instanceID))
			if message := readDevStudioMessage(t, second); message["type"] != "session.ready" {
				t.Fatalf("retry connect = %v, want session.ready", message)
			}
			if starter.startCount() != 2 {
				t.Fatalf("Codex start count = %d, want 2", starter.startCount())
			}
		})
	}
}

func TestDevStudioCodexTurnStartMalformedResponseCleansUpStateForSubsequentTurn(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_turn_malformed")
	createDevStudioApp(t, baseDir, "inst_turn_malformed", "app-one")

	starter := newFakeDevStudioCodexStarter(nil)
	starter.malformedTurnStart = true
	connection, _ := connectDevStudioSession(t, newDevStudioBridgeWithCodexStarter(starter), "inst_turn_malformed")

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Malformed turn"})
	assertDevStudioTurnFailed(t, readDevStudioEventsUntil(t, connection, "turn.completed"), "codex_unavailable")

	starter.mu.Lock()
	starter.malformedTurnStart = false
	starter.mu.Unlock()

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Normal turn"})
	if completed := readDevStudioEvent(t, connection, "turn.completed"); completed["status"] != "completed" {
		t.Fatalf("normal turn = %v, want completed", completed)
	}
}

func TestDevStudioCodexDeployKeyRace(t *testing.T) {
	starter := newFakeDevStudioCodexStarter(nil)
	proc, err := starter.Start(devStudioCodexStartConfig{
		WorkspaceRoot: "/tmp",
		InstanceID:    "inst_race",
		DeployKey:     "secret_deploy_key_val",
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	adapter := newDevStudioCodexAdapter(starter, "inst_race", &devStudioWorkspace{instanceRoot: "/tmp"}, "secret_deploy_key_val", nil)
	if _, err := adapter.attach(proc); err != nil {
		t.Fatalf("attach process: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = adapter.redact("message with secret_deploy_key_val inside")
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(1 * time.Millisecond)
		adapter.close()
	}()

	wg.Wait()
}

// assertDevStudioTurnFailed checks that a turn's events end with an error
// event carrying code, then turn.completed failed with the same error.
func assertDevStudioTurnFailed(t *testing.T, events []map[string]any, code string) {
	t.Helper()
	if len(events) < 2 {
		t.Fatalf("events = %v, want an error then turn.completed", events)
	}
	errorEvent, completed := events[len(events)-2], events[len(events)-1]
	if errorEvent["type"] != "error" || errorEvent["code"] != code || errorEvent["willRetry"] != false {
		t.Fatalf("error event = %v, want %s", errorEvent, code)
	}
	turnError, _ := completed["error"].(map[string]any)
	if completed["status"] != "failed" || turnError["code"] != code {
		t.Fatalf("turn.completed = %v, want failed with %s", completed, code)
	}
}

func createDevStudioApp(t *testing.T, baseDir, instanceID, appID string) string {
	t.Helper()
	instanceRoot := filepath.Join(baseDir, instanceID)
	appDir := filepath.Join(instanceRoot, devStudioAppsDirectory, appID)
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.scl"), []byte("app "+appID+"\n"), 0644); err != nil {
		t.Fatalf("write app.scl: %v", err)
	}
	return appDir
}

func startDevStudioBridgeServer(t *testing.T, bridge *devStudioBridge) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(bridge.handler())
	t.Cleanup(server.Close)
	return server
}

func readDevStudioEvent(t *testing.T, connection *websocket.Conn, eventType string) map[string]any {
	t.Helper()
	for _, event := range readDevStudioEventsUntil(t, connection, eventType) {
		if event["type"] == eventType {
			return event
		}
	}
	t.Fatalf("event %q not received", eventType)
	return nil
}

func readDevStudioEventsUntil(t *testing.T, connection *websocket.Conn, finalType string) []map[string]any {
	t.Helper()
	events := make([]map[string]any, 0)
	for {
		event := readDevStudioMessage(t, connection)
		events = append(events, event)
		if event["type"] == finalType {
			return events
		}
	}
}

func hasDevStudioEvent(events []map[string]any, eventType string) bool {
	for _, event := range events {
		if event["type"] == eventType {
			return true
		}
	}
	return false
}

func hasDevStudioStatus(events []map[string]any, status string) bool {
	for _, event := range events {
		if event["type"] == "status.changed" && event["status"] == status {
			return true
		}
	}
	return false
}

func anyStrings(values []any) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func sameStrings(got, want []string) bool {
	return strings.Join(got, "\x00") == strings.Join(want, "\x00")
}

// sameJSON compares two values by their JSON, so a decoded event can be
// checked against a literal.
func sameJSON(got, want any) bool {
	gotJSON, gotErr := json.Marshal(got)
	wantJSON, wantErr := json.Marshal(want)
	return gotErr == nil && wantErr == nil && string(gotJSON) == string(wantJSON)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %v: %v", value, err)
	}
	return string(encoded)
}

func stringPointer(value string) *string {
	return &value
}

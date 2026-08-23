package cli

import (
	"encoding/json"
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

type fakeDevStudioCodexStarter struct {
	mu                   sync.Mutex
	starts               []devStudioCodexStartConfig
	requests             []fakeDevStudioCodexRequest
	changePaths          []string
	holdTurnOpen         bool
	exitOnMethod         string
	failInitialize       bool
	failThreadStart      bool
	malformedThreadStart bool
	malformedTurnStart   bool
	onTurnStart          func(process *fakeDevStudioCodexProcess)
	processes            []*fakeDevStudioCodexProcess
	interruptReceived    chan struct{}
}

type fakeDevStudioCodexRequest struct {
	Method string
	Params map[string]any
}

func newFakeDevStudioCodexStarter(changePaths []string) *fakeDevStudioCodexStarter {
	return &fakeDevStudioCodexStarter{
		changePaths:       changePaths,
		interruptReceived: make(chan struct{}, 1),
	}
}

func (starter *fakeDevStudioCodexStarter) Start(config devStudioCodexStartConfig) (devStudioCodexProcess, error) {
	process := newFakeDevStudioCodexProcess(starter)
	starter.mu.Lock()
	starter.starts = append(starter.starts, config)
	starter.processes = append(starter.processes, process)
	starter.mu.Unlock()
	return process, nil
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

type fakeDevStudioCodexProcess struct {
	inputReader  *io.PipeReader
	inputWriter  *io.PipeWriter
	outputReader *io.PipeReader
	outputWriter *io.PipeWriter
	starter      *fakeDevStudioCodexStarter
	once         sync.Once
	stopMu       sync.Mutex
	closed       bool
}

func newFakeDevStudioCodexProcess(starter *fakeDevStudioCodexStarter) *fakeDevStudioCodexProcess {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	process := &fakeDevStudioCodexProcess{
		inputReader:  inputReader,
		inputWriter:  inputWriter,
		outputReader: outputReader,
		outputWriter: outputWriter,
		starter:      starter,
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

func (process *fakeDevStudioCodexProcess) run() {
	decoder := json.NewDecoder(process.inputReader)
	encoder := json.NewEncoder(process.outputWriter)
	for {
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&request); err != nil {
			return
		}
		process.starter.recordRequest(request.Method, request.Params)
		if request.ID == nil {
			continue
		}
		process.starter.mu.Lock()
		exitOn := process.starter.exitOnMethod
		process.starter.mu.Unlock()
		if exitOn != "" && exitOn == request.Method {
			process.Stop()
			return
		}
		switch request.Method {
		case "initialize":
			process.starter.mu.Lock()
			failInit := process.starter.failInitialize
			process.starter.mu.Unlock()
			if failInit {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "error": map[string]any{"message": "init_failed"}})
				continue
			}
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": map[string]any{"userAgent": "fake", "codexHome": "/tmp", "platformFamily": "unix", "platformOs": "linux"}})
		case "thread/start":
			process.starter.mu.Lock()
			failThread := process.starter.failThreadStart
			malformedThread := process.starter.malformedThreadStart
			process.starter.mu.Unlock()
			if failThread {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "error": map[string]any{"message": "thread_start_failed"}})
				continue
			}
			if malformedThread {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": map[string]any{"thread": map[string]any{}}})
				continue
			}
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": map[string]any{"thread": map[string]any{"id": "thread_fake"}}})
		case "turn/start":
			process.starter.mu.Lock()
			onTurn := process.starter.onTurnStart
			malformedTurn := process.starter.malformedTurnStart
			holdOpen := process.starter.holdTurnOpen
			process.starter.mu.Unlock()
			if malformedTurn {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": map[string]any{"turn": map[string]any{}}})
				continue
			}
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": map[string]any{"turn": map[string]any{"id": "turn_fake"}}})
			if onTurn != nil {
				onTurn(process)
			} else if !holdOpen {
				process.emitTurnEvents(encoder)
			}
		case "turn/interrupt":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": map[string]any{}})
			select {
			case process.starter.interruptReceived <- struct{}{}:
			default:
			}
			process.emitTurnCompleted(encoder)
		}
	}
}

func (process *fakeDevStudioCodexProcess) emitTurnEvents(encoder *json.Encoder) {
	command := "simple deploy apps/app-one --env dev"
	_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "item/started", "params": map[string]any{"threadId": "thread_fake", "turnId": "turn_fake", "startedAtMs": 1, "item": map[string]any{"type": "commandExecution", "id": "command_1", "command": command}}})
	_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "item/commandExecution/outputDelta", "params": map[string]any{"threadId": "thread_fake", "turnId": "turn_fake", "itemId": "command_1", "delta": "Deploying app-one\n"}})
	_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{"threadId": "thread_fake", "turnId": "turn_fake", "completedAtMs": 2, "item": map[string]any{"type": "commandExecution", "id": "command_1", "command": command, "aggregatedOutput": "Deployed app-one@1.0.1\n", "exitCode": 0}}})
	_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread_fake", "turnId": "turn_fake", "itemId": "message_1", "delta": "Safe Codex response"}})
	changes := make([]map[string]string, 0, len(process.starter.changePaths))
	for _, path := range process.starter.changePaths {
		changes = append(changes, map[string]string{"path": path})
	}
	_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{"threadId": "thread_fake", "turnId": "turn_fake", "completedAtMs": 2, "item": map[string]any{"type": "fileChange", "id": "change_1", "changes": changes}}})
	process.emitTurnCompleted(encoder)
}

func (process *fakeDevStudioCodexProcess) emitTurnCompleted(encoder *json.Encoder) {
	_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "thread_fake", "turn": map[string]any{"id": "turn_fake"}}})
}

func TestDevStudioCodexStartsOnlyForFirstTurnAndUsesVerifiedProtocol(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_codex")
	createDevStudioApp(t, baseDir, "inst_codex", "app-one")
	starter := newFakeDevStudioCodexStarter([]string{filepath.Join(baseDir, "inst_codex", devStudioAppsDirectory, "app-one", "records", "settings.scl")})
	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_codex"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}
	if len(starter.starts) != 0 {
		t.Fatalf("Codex starts before turn.start = %d, want 0", len(starter.starts))
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Inspect both applications."})
	readDevStudioEvent(t, connection, "turn.completed")

	starter.mu.Lock()
	starts := append([]devStudioCodexStartConfig(nil), starter.starts...)
	starter.mu.Unlock()
	if len(starts) != 1 {
		t.Fatalf("Codex starts = %d, want 1", len(starts))
	}
	if starts[0].WorkspaceRoot != filepath.Join(baseDir, "inst_codex") {
		t.Fatalf("Codex cwd = %q", starts[0].WorkspaceRoot)
	}
	if starts[0].InstanceID != "inst_codex" || starts[0].DeployKey != "test_deploy_key_secret" {
		t.Fatal("Codex start config did not retain the session-only instance context")
	}
	if got, want := starter.requestMethods(), []string{"initialize", "initialized", "thread/start", "turn/start"}; !sameStrings(got, want) {
		t.Fatalf("protocol methods = %v, want %v", got, want)
	}
	threadStart, ok := starter.request("thread/start")
	if !ok || threadStart.Params["cwd"] != filepath.Join(baseDir, "inst_codex") || threadStart.Params["approvalPolicy"] != "never" || threadStart.Params["sandbox"] != "workspace-write" {
		t.Fatalf("thread/start params = %#v", threadStart.Params)
	}
	turnStart, ok := starter.request("turn/start")
	if !ok || turnStart.Params["threadId"] != "thread_fake" || turnStart.Params["cwd"] != filepath.Join(baseDir, "inst_codex") {
		t.Fatalf("turn/start params = %#v", turnStart.Params)
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

func TestDevStudioCodexMapsDeltasStatusesAndAppRelativeChanges(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_events")
	createDevStudioApp(t, baseDir, "inst_events", "app-one")
	createDevStudioApp(t, baseDir, "inst_events", "app-two")
	instanceRoot := filepath.Join(baseDir, "inst_events")
	starter := newFakeDevStudioCodexStarter([]string{
		filepath.Join(instanceRoot, devStudioAppsDirectory, "app-one", "records", "one.scl"),
		filepath.Join(instanceRoot, devStudioAppsDirectory, "app-two", "records", "two.scl"),
	})
	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_events"))
	_ = readDevStudioMessage(t, connection)
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Change two apps."})

	events := readDevStudioEventsUntil(t, connection, "turn.completed")
	if !hasDevStudioEvent(events, "message.delta") {
		t.Fatalf("events missing message.delta: %v", events)
	}
	if !hasDevStudioEvent(events, "command.started") || !hasDevStudioEvent(events, "command.output") || !hasDevStudioEvent(events, "command.completed") {
		t.Fatalf("events missing command activity: %v", events)
	}
	if !hasDevStudioStatus(events, "running_command") || !hasDevStudioStatus(events, "editing") || !hasDevStudioStatus(events, "complete") {
		t.Fatalf("events missing expected statuses: %v", events)
	}
	var changedPaths []any
	for _, event := range events {
		if event["type"] == "files.changed" {
			changedPaths, _ = event["paths"].([]any)
		}
	}
	if !sameStrings(anyStrings(changedPaths), []string{"apps/app-one/records/one.scl", "apps/app-two/records/two.scl"}) {
		t.Fatalf("changed paths = %v", changedPaths)
	}
	for _, event := range events {
		if event["type"] != "command.completed" {
			continue
		}
		if event["command"] != "simple deploy apps/app-one --env dev" || event["exitCode"] != float64(0) || event["output"] != "Deployed app-one@1.0.1\n" {
			t.Fatalf("command completion = %v", event)
		}
	}
}

func TestDevStudioBridgeDoesNotOwnSimpleCLIExecution(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_no_bridge_cli")
	createDevStudioApp(t, baseDir, "inst_no_bridge_cli", "app-one")
	starter := newFakeDevStudioCodexStarter(nil)
	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_no_bridge_cli"))
	_ = readDevStudioMessage(t, connection)
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Decide whether deployment is appropriate."})
	readDevStudioEvent(t, connection, "turn.completed")

	if len(starter.starts) != 1 {
		t.Fatalf("Codex starts = %d, want 1", len(starter.starts))
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
	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_cancel"))
	_ = readDevStudioMessage(t, connection)
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Long task"})
	time.Sleep(20 * time.Millisecond)
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Second task"})
	secondTurnEvents := readDevStudioEventsUntil(t, connection, "error")
	secondTurnError := secondTurnEvents[len(secondTurnEvents)-1]
	if secondTurnError["code"] != "turn_already_active" {
		t.Fatalf("second turn error = %v, want turn_already_active", secondTurnError)
	}
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.cancel"})
	select {
	case <-starter.interruptReceived:
	case <-time.After(time.Second):
		t.Fatal("turn.cancel did not call turn/interrupt")
	}
	readDevStudioEvent(t, connection, "turn.completed")
	if !strings.Contains(strings.Join(starter.requestMethods(), ","), "turn/interrupt") {
		t.Fatalf("protocol methods = %v, missing turn/interrupt", starter.requestMethods())
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
	defer connection.Close()

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
	defer connection.Close()

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
	defer connection.Close()

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

	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_turn_del_parent"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Delete agent directory"})
	events := readDevStudioEventsUntil(t, connection, "error")
	lastEvent := events[len(events)-1]
	if lastEvent["code"] != "protected_files_modified" {
		t.Fatalf("error code = %v, want protected_files_modified", lastEvent["code"])
	}

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

	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_exit_test"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}

	start := time.Now()
	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Run a task that exits unexpectedly."})

	events := readDevStudioEventsUntil(t, connection, "error")
	elapsed := time.Since(start)

	if elapsed >= 5*time.Second {
		t.Fatalf("pending call took %v to fail; expected immediate failure without waiting for timeout", elapsed)
	}

	lastEvent := events[len(events)-1]
	if lastEvent["code"] != "codex_unavailable" {
		t.Fatalf("error code = %v, want codex_unavailable", lastEvent["code"])
	}
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

	adapter := &devStudioCodexAdapter{
		starter: starter,
		process: proc,
		encoder: json.NewEncoder(proc.Input()),
		pending: make(map[int]chan devStudioJSONRPCResponse),
	}
	go adapter.readCodexOutput(proc.Output())

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

	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_exit_recover"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn 1"})
	events := readDevStudioEventsUntil(t, connection, "error")
	lastEvent := events[len(events)-1]
	if lastEvent["code"] != "protected_files_modified" {
		t.Fatalf("error code = %v, want protected_files_modified", lastEvent["code"])
	}

	contents, err := os.ReadFile(agentsFile)
	if err != nil || string(contents) != "original instructions\n" {
		t.Fatalf("restored AGENTS.md = %q, want original instructions", string(contents))
	}

	starter.mu.Lock()
	if len(starter.processes) != 1 || !starter.processes[0].isStopped() {
		t.Fatal("expected first process to be stopped")
	}
	starter.onTurnStart = nil
	starter.mu.Unlock()

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn 2"})
	readDevStudioEvent(t, connection, "turn.completed")

	starter.mu.Lock()
	startCount := len(starter.starts)
	starter.mu.Unlock()
	if startCount != 2 {
		t.Fatalf("Codex start count = %d, want 2", startCount)
	}

	contents, err = os.ReadFile(agentsFile)
	if err != nil || string(contents) != "original instructions\n" {
		t.Fatalf("AGENTS.md after turn 2 = %q, want original instructions", string(contents))
	}
}

func TestDevStudioCodexInitializeFailureStopsProcessAndSubsequentTurnRetries(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_init_fail")
	createDevStudioApp(t, baseDir, "inst_init_fail", "app-one")

	starter := newFakeDevStudioCodexStarter(nil)
	starter.failInitialize = true

	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_init_fail"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn with init failure"})
	events := readDevStudioEventsUntil(t, connection, "error")
	if lastEvent := events[len(events)-1]; lastEvent["code"] != "codex_unavailable" {
		t.Fatalf("error code = %v, want codex_unavailable", lastEvent["code"])
	}

	starter.mu.Lock()
	if len(starter.processes) != 1 || !starter.processes[0].isStopped() {
		t.Fatal("expected first process to be stopped on initialize failure")
	}
	starter.failInitialize = false
	starter.mu.Unlock()

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn retry"})
	readDevStudioEvent(t, connection, "turn.completed")

	starter.mu.Lock()
	startCount := len(starter.starts)
	starter.mu.Unlock()
	if startCount != 2 {
		t.Fatalf("Codex start count = %d, want 2", startCount)
	}
}

func TestDevStudioCodexThreadStartFailureStopsProcessAndSubsequentTurnRetries(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_thread_fail")
	createDevStudioApp(t, baseDir, "inst_thread_fail", "app-one")

	starter := newFakeDevStudioCodexStarter(nil)
	starter.failThreadStart = true

	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_thread_fail"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn with thread failure"})
	events := readDevStudioEventsUntil(t, connection, "error")
	if lastEvent := events[len(events)-1]; lastEvent["code"] != "codex_unavailable" {
		t.Fatalf("error code = %v, want codex_unavailable", lastEvent["code"])
	}

	starter.mu.Lock()
	if len(starter.processes) != 1 || !starter.processes[0].isStopped() {
		t.Fatal("expected first process to be stopped on thread/start failure")
	}
	starter.failThreadStart = false
	starter.mu.Unlock()

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn retry"})
	readDevStudioEvent(t, connection, "turn.completed")

	starter.mu.Lock()
	startCount := len(starter.starts)
	starter.mu.Unlock()
	if startCount != 2 {
		t.Fatalf("Codex start count = %d, want 2", startCount)
	}
}

func TestDevStudioCodexThreadStartMalformedResponseStopsProcessAndSubsequentTurnRetries(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_thread_malformed")
	createDevStudioApp(t, baseDir, "inst_thread_malformed", "app-one")

	starter := newFakeDevStudioCodexStarter(nil)
	starter.malformedThreadStart = true

	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_thread_malformed"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn with malformed thread"})
	events := readDevStudioEventsUntil(t, connection, "error")
	if lastEvent := events[len(events)-1]; lastEvent["code"] != "codex_unavailable" {
		t.Fatalf("error code = %v, want codex_unavailable", lastEvent["code"])
	}

	starter.mu.Lock()
	if len(starter.processes) != 1 || !starter.processes[0].isStopped() {
		t.Fatal("expected first process to be stopped on malformed thread/start response")
	}
	starter.malformedThreadStart = false
	starter.mu.Unlock()

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Turn retry"})
	readDevStudioEvent(t, connection, "turn.completed")

	starter.mu.Lock()
	startCount := len(starter.starts)
	starter.mu.Unlock()
	if startCount != 2 {
		t.Fatalf("Codex start count = %d, want 2", startCount)
	}
}

func TestDevStudioCodexTurnStartMalformedResponseCleansUpStateForSubsequentTurn(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_turn_malformed")
	createDevStudioApp(t, baseDir, "inst_turn_malformed", "app-one")

	starter := newFakeDevStudioCodexStarter(nil)
	starter.malformedTurnStart = true

	bridge := newDevStudioBridgeWithCodexStarter(starter)
	server := startDevStudioBridgeServer(t, bridge)
	connection := dialDevStudio(t, "ws"+strings.TrimPrefix(server.URL, "http")+devStudioWebSocketPath)
	defer connection.Close()

	sendDevStudioMessage(t, connection, sessionConnectMessage("inst_turn_malformed"))
	if message := readDevStudioMessage(t, connection); message["type"] != "session.ready" {
		t.Fatalf("session type = %q, want session.ready", message["type"])
	}

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Malformed turn"})
	events := readDevStudioEventsUntil(t, connection, "error")
	if lastEvent := events[len(events)-1]; lastEvent["code"] != "codex_unavailable" {
		t.Fatalf("error code = %v, want codex_unavailable", lastEvent["code"])
	}

	starter.mu.Lock()
	starter.malformedTurnStart = false
	starter.mu.Unlock()

	sendDevStudioMessage(t, connection, map[string]any{"type": "turn.start", "text": "Normal turn"})
	readDevStudioEvent(t, connection, "turn.completed")
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

	adapter := &devStudioCodexAdapter{
		starter:   starter,
		deployKey: "secret_deploy_key_val",
		process:   proc,
		encoder:   json.NewEncoder(proc.Input()),
		pending:   make(map[int]chan devStudioJSONRPCResponse),
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

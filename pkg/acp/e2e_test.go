package acp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/message"
	"github.com/cexll/agentsdk-go/pkg/model"
	acpproto "github.com/coder/acp-go-sdk"
)

type e2eHarness struct {
	adapter    *Adapter
	client     *e2eClient
	clientConn *acpproto.ClientSideConnection
	agentConn  *acpproto.AgentSideConnection
	agentPipe  net.Conn
	clientPipe net.Conn
}

func newE2EHarness(t *testing.T, opts api.Options, client *e2eClient) *e2eHarness {
	t.Helper()
	if client == nil {
		client = newE2EClient()
	}

	agentPipe, clientPipe := net.Pipe()
	adapter := NewAdapter(opts)
	agentConn := acpproto.NewAgentSideConnection(adapter, agentPipe, agentPipe)
	adapter.SetConnection(agentConn)
	clientConn := acpproto.NewClientSideConnection(client, clientPipe, clientPipe)

	h := &e2eHarness{
		adapter:    adapter,
		client:     client,
		clientConn: clientConn,
		agentConn:  agentConn,
		agentPipe:  agentPipe,
		clientPipe: clientPipe,
	}
	t.Cleanup(func() { h.close(t) })
	return h
}

func (h *e2eHarness) close(t *testing.T) {
	t.Helper()
	if h == nil {
		return
	}
	// Close runtime sessions before tearing down transport to avoid background
	// history persisters writing into temp dirs during test cleanup.
	h.adapter.mu.RLock()
	states := make([]*sessionState, 0, len(h.adapter.sessions))
	for _, state := range h.adapter.sessions {
		states = append(states, state)
	}
	h.adapter.mu.RUnlock()
	for _, state := range states {
		if state == nil {
			continue
		}
		if rt := state.runtime(); rt != nil {
			_ = rt.Close()
		}
	}
	_ = h.agentPipe.Close()
	_ = h.clientPipe.Close()
}

func initializeACP(t *testing.T, conn *acpproto.ClientSideConnection, caps acpproto.ClientCapabilities) acpproto.InitializeResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := conn.Initialize(ctx, acpproto.InitializeRequest{
		ProtocolVersion:    acpproto.ProtocolVersionNumber,
		ClientCapabilities: caps,
	})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	return resp
}

func mustNewSession(t *testing.T, conn *acpproto.ClientSideConnection, cwd string, mcpServers []acpproto.McpServer) acpproto.NewSessionResponse {
	t.Helper()
	if mcpServers == nil {
		mcpServers = []acpproto.McpServer{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := conn.NewSession(ctx, acpproto.NewSessionRequest{
		Cwd:        cwd,
		McpServers: mcpServers,
	})
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}
	return resp
}

func requireEventually(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func TestACPInprocLifecycleAndStreaming(t *testing.T) {
	root := t.TempDir()
	client := newE2EClient()
	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, stubModel{}), client)

	initResp := initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})
	if initResp.ProtocolVersion != acpproto.ProtocolVersionNumber {
		t.Fatalf("protocolVersion=%d, want %d", initResp.ProtocolVersion, acpproto.ProtocolVersionNumber)
	}
	if !initResp.AgentCapabilities.LoadSession {
		t.Fatalf("expected loadSession capability")
	}

	sess := mustNewSession(t, h.clientConn, root, nil)
	if strings.TrimSpace(string(sess.SessionId)) == "" {
		t.Fatalf("sessionId should not be empty")
	}
	if sess.Modes == nil || len(sess.Modes.AvailableModes) == 0 {
		t.Fatalf("expected modes in new session response")
	}
	if len(sess.ConfigOptions) == 0 {
		t.Fatalf("expected config options in new session response")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	promptResp, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("hello")},
	})
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if promptResp.StopReason != acpproto.StopReasonEndTurn {
		t.Fatalf("stopReason=%q, want %q", promptResp.StopReason, acpproto.StopReasonEndTurn)
	}

	var chunkText strings.Builder
	updates := client.updatesSnapshot()
	for _, update := range updates {
		if update.SessionId != sess.SessionId {
			continue
		}
		if update.Update.AgentMessageChunk == nil || update.Update.AgentMessageChunk.Content.Text == nil {
			continue
		}
		chunkText.WriteString(update.Update.AgentMessageChunk.Content.Text.Text)
	}
	if !strings.Contains(chunkText.String(), "ok") {
		t.Fatalf("expected streamed assistant chunk containing %q, got %q", "ok", chunkText.String())
	}

	if _, err := h.clientConn.SetSessionMode(context.Background(), acpproto.SetSessionModeRequest{
		SessionId: sess.SessionId,
		ModeId:    modeCodeID,
	}); err != nil {
		t.Fatalf("set session mode failed: %v", err)
	}

	var selectedConfig *acpproto.SessionConfigOptionSelect
	for _, option := range sess.ConfigOptions {
		if option.Select != nil && option.Select.Id == configPermissionModeID {
			selectedConfig = option.Select
			break
		}
	}
	if selectedConfig == nil {
		t.Fatalf("permission_mode config option not found")
	}
	setConfigResp, err := h.clientConn.SetSessionConfigOption(context.Background(), acpproto.SetSessionConfigOptionRequest{
		SessionId: sess.SessionId,
		ConfigId:  configPermissionModeID,
		Value:     permissionModeAllowAlways,
	})
	if err != nil {
		t.Fatalf("set session config option failed: %v", err)
	}
	var updated bool
	for _, option := range setConfigResp.ConfigOptions {
		if option.Select == nil || option.Select.Id != configPermissionModeID {
			continue
		}
		updated = option.Select.CurrentValue == permissionModeAllowAlways
	}
	if !updated {
		t.Fatalf("permission_mode was not updated to %q", permissionModeAllowAlways)
	}
}

func TestACPInprocLoadSessionReplayHistory(t *testing.T) {
	root := t.TempDir()
	sessionID := acpproto.SessionId("sess-replay")
	if err := api.SavePersistedHistory(root, string(sessionID), []message.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}); err != nil {
		t.Fatalf("save persisted history: %v", err)
	}

	client := newE2EClient()
	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, stubModel{}), client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	loadResp, err := h.clientConn.LoadSession(ctx, acpproto.LoadSessionRequest{
		SessionId:  sessionID,
		Cwd:        root,
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("load session failed: %v", err)
	}
	if loadResp.Modes == nil {
		t.Fatalf("expected modes in load session response")
	}
	if len(loadResp.ConfigOptions) == 0 {
		t.Fatalf("expected config options in load session response")
	}

	requireEventually(t, 2*time.Second, func() bool {
		updates := client.updatesSnapshot()
		var sawUser bool
		var sawAgent bool
		for _, update := range updates {
			if update.SessionId != sessionID {
				continue
			}
			if update.Update.UserMessageChunk != nil {
				sawUser = true
			}
			if update.Update.AgentMessageChunk != nil {
				sawAgent = true
			}
		}
		return sawUser && sawAgent
	}, "history replay updates")
}

func TestACPInprocCancelAndConcurrentPrompt(t *testing.T) {
	root := t.TempDir()
	model := &blockingStreamModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	client := newE2EClient()
	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, model), client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})

	sess := mustNewSession(t, h.clientConn, root, nil)
	type promptResult struct {
		resp acpproto.PromptResponse
		err  error
	}
	firstPromptDone := make(chan promptResult, 1)
	go func() {
		resp, err := h.clientConn.Prompt(context.Background(), acpproto.PromptRequest{
			SessionId: sess.SessionId,
			Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("first")},
		})
		firstPromptDone <- promptResult{resp: resp, err: err}
	}()

	select {
	case <-model.started:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for first prompt to start")
	}

	_, err := h.clientConn.Prompt(context.Background(), acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("second")},
	})
	if err == nil {
		t.Fatalf("expected concurrent prompt to be rejected")
	}
	var reqErr *acpproto.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected ACP request error, got %T", err)
	}
	if reqErr.Code != -32600 {
		t.Fatalf("error code=%d, want -32600", reqErr.Code)
	}

	if err := h.clientConn.Cancel(context.Background(), acpproto.CancelNotification{
		SessionId: sess.SessionId,
	}); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}

	select {
	case result := <-firstPromptDone:
		if result.err != nil {
			t.Fatalf("first prompt failed: %v", result.err)
		}
		if result.resp.StopReason != acpproto.StopReasonCancelled {
			t.Fatalf("stopReason=%q, want %q", result.resp.StopReason, acpproto.StopReasonCancelled)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for first prompt to stop")
	}
}

func TestACPInprocPermissionRoundTrip(t *testing.T) {
	root := t.TempDir()
	client := newE2EClient()
	client.permissionOutcome = permissionOptionAllowAlways
	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, stubModel{}), client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})

	decision, handled, err := h.adapter.requestPermissionFromClient(context.Background(), acpproto.SessionId("sess-perm"), api.PermissionRequest{
		ToolName:   "Read",
		ToolParams: map[string]any{"file_path": filepath.Join(root, "a.txt")},
		Target:     filepath.Join(root, "a.txt"),
	})
	if err != nil {
		t.Fatalf("request permission failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected permission request to be handled by ACP client")
	}
	if decision != "allow" {
		t.Fatalf("decision=%q, want allow", decision)
	}

	requests := client.permissionRequestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("permission request count=%d, want 1", len(requests))
	}
	if requests[0].SessionId != "sess-perm" {
		t.Fatalf("permission request sessionId=%q, want %q", requests[0].SessionId, "sess-perm")
	}
}

func TestACPInprocCapabilityBridgeReadWriteBash(t *testing.T) {
	root := t.TempDir()
	client := newE2EClient()
	client.readContent = "from-client-read"
	client.terminalOutput = "terminal-ok"
	client.terminalExitCode = 0

	caps := acpproto.ClientCapabilities{}
	caps.Fs.ReadTextFile = true
	caps.Fs.WriteTextFile = true
	caps.Terminal = true

	model := newToolPlanModel([]toolPlanStep{
		{
			ToolName: "Read",
			Args: map[string]any{
				"file_path": filepath.Join(root, "in.txt"),
			},
		},
		{
			ToolName: "Write",
			Args: map[string]any{
				"file_path": filepath.Join(root, "out.txt"),
				"content":   "payload",
			},
		},
		{
			ToolName: "Bash",
			Args: map[string]any{
				"command": "echo hi",
				"workdir": root,
				"timeout": 1,
			},
		},
	})

	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, model), client)
	initializeACP(t, h.clientConn, caps)
	sess := mustNewSession(t, h.clientConn, root, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("run capability bridge")},
	})
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if resp.StopReason != acpproto.StopReasonEndTurn {
		t.Fatalf("stopReason=%q, want %q", resp.StopReason, acpproto.StopReasonEndTurn)
	}

	readReqs := client.readRequestsSnapshot()
	if len(readReqs) == 0 {
		t.Fatalf("expected fs/read_text_file requests")
	}
	if readReqs[0].SessionId != sess.SessionId {
		t.Fatalf("read sessionId=%q, want %q", readReqs[0].SessionId, sess.SessionId)
	}

	writeReqs := client.writeRequestsSnapshot()
	if len(writeReqs) == 0 {
		t.Fatalf("expected fs/write_text_file requests")
	}
	if writeReqs[0].Content != "payload" {
		t.Fatalf("write content=%q, want %q", writeReqs[0].Content, "payload")
	}

	creates := client.createTerminalRequestsSnapshot()
	waits := client.waitTerminalRequestsSnapshot()
	outputs := client.terminalOutputRequestsSnapshot()
	releases := client.releaseTerminalRequestsSnapshot()
	if len(creates) == 0 || len(waits) == 0 || len(outputs) == 0 || len(releases) == 0 {
		t.Fatalf("expected terminal request lifecycle calls create=%d wait=%d output=%d release=%d", len(creates), len(waits), len(outputs), len(releases))
	}
	wantCmd, wantArgs := shellInvocation("echo hi")
	if creates[0].Command != wantCmd {
		t.Fatalf("terminal command=%q, want %q", creates[0].Command, wantCmd)
	}
	if strings.Join(creates[0].Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("terminal args=%v, want %v", creates[0].Args, wantArgs)
	}
	if creates[0].Cwd == nil || filepath.Clean(*creates[0].Cwd) != filepath.Clean(root) {
		t.Fatalf("terminal cwd=%v, want %q", creates[0].Cwd, root)
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(creates[0].Command, "cmd") {
		t.Fatalf("windows terminal command=%q, want cmd", creates[0].Command)
	}
}

type toolPlanStep struct {
	ToolName string
	Args     map[string]any
}

type toolPlanModel struct {
	mu    sync.Mutex
	steps []toolPlanStep
	idx   int
}

func newToolPlanModel(steps []toolPlanStep) *toolPlanModel {
	cp := make([]toolPlanStep, 0, len(steps))
	for _, step := range steps {
		cp = append(cp, toolPlanStep{
			ToolName: step.ToolName,
			Args:     cloneMap(step.Args),
		})
	}
	return &toolPlanModel{steps: cp}
}

func (m *toolPlanModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	var final *model.Response
	err := m.CompleteStream(ctx, req, func(sr model.StreamResult) error {
		if sr.Final && sr.Response != nil {
			final = sr.Response
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if final == nil {
		return nil, errors.New("toolPlanModel: no final response")
	}
	return final, nil
}

func (m *toolPlanModel) CompleteStream(_ context.Context, _ model.Request, cb model.StreamHandler) error {
	if cb == nil {
		return nil
	}

	m.mu.Lock()
	idx := m.idx
	m.idx++
	m.mu.Unlock()

	if idx < len(m.steps) {
		step := m.steps[idx]
		return cb(model.StreamResult{
			Final: true,
			Response: &model.Response{
				Message: model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						ID:        fmt.Sprintf("tool-%d", idx+1),
						Name:      step.ToolName,
						Arguments: cloneMap(step.Args),
					}},
				},
				StopReason: "tool_use",
			},
		})
	}

	return cb(model.StreamResult{
		Delta: "done",
		Final: true,
		Response: &model.Response{
			Message: model.Message{
				Role:    "assistant",
				Content: "done",
			},
			StopReason: "end_turn",
		},
	})
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

type e2eClient struct {
	mu sync.Mutex

	updates            []acpproto.SessionNotification
	permissionRequests []acpproto.RequestPermissionRequest
	readRequests       []acpproto.ReadTextFileRequest
	writeRequests      []acpproto.WriteTextFileRequest
	createTerminalReqs []acpproto.CreateTerminalRequest
	waitTerminalReqs   []acpproto.WaitForTerminalExitRequest
	outputTerminalReqs []acpproto.TerminalOutputRequest
	releaseTerminalReq []acpproto.ReleaseTerminalRequest
	killTerminalReqs   []acpproto.KillTerminalCommandRequest

	permissionOutcome acpproto.PermissionOptionId
	readContent       string
	terminalOutput    string
	terminalExitCode  int
	terminalSignal    string
	terminalCounter   int
}

func newE2EClient() *e2eClient {
	return &e2eClient{
		readContent:      "client-content",
		terminalOutput:   "ok",
		terminalExitCode: 0,
	}
}

func (c *e2eClient) ReadTextFile(_ context.Context, params acpproto.ReadTextFileRequest) (acpproto.ReadTextFileResponse, error) {
	c.mu.Lock()
	c.readRequests = append(c.readRequests, params)
	content := c.readContent
	c.mu.Unlock()
	return acpproto.ReadTextFileResponse{Content: content}, nil
}

func (c *e2eClient) WriteTextFile(_ context.Context, params acpproto.WriteTextFileRequest) (acpproto.WriteTextFileResponse, error) {
	c.mu.Lock()
	c.writeRequests = append(c.writeRequests, params)
	c.mu.Unlock()
	return acpproto.WriteTextFileResponse{}, nil
}

func (c *e2eClient) RequestPermission(_ context.Context, params acpproto.RequestPermissionRequest) (acpproto.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.permissionRequests = append(c.permissionRequests, params)
	selected := c.permissionOutcome
	c.mu.Unlock()

	if selected == "" && len(params.Options) > 0 {
		selected = params.Options[0].OptionId
	}
	if selected == "" {
		return acpproto.RequestPermissionResponse{
			Outcome: acpproto.RequestPermissionOutcome{
				Cancelled: &acpproto.RequestPermissionOutcomeCancelled{},
			},
		}, nil
	}
	return acpproto.RequestPermissionResponse{
		Outcome: acpproto.RequestPermissionOutcome{
			Selected: &acpproto.RequestPermissionOutcomeSelected{OptionId: selected},
		},
	}, nil
}

func (c *e2eClient) SessionUpdate(_ context.Context, params acpproto.SessionNotification) error {
	c.mu.Lock()
	c.updates = append(c.updates, params)
	c.mu.Unlock()
	return nil
}

func (c *e2eClient) CreateTerminal(_ context.Context, params acpproto.CreateTerminalRequest) (acpproto.CreateTerminalResponse, error) {
	c.mu.Lock()
	c.createTerminalReqs = append(c.createTerminalReqs, params)
	c.terminalCounter++
	terminalID := fmt.Sprintf("term-%d", c.terminalCounter)
	c.mu.Unlock()
	return acpproto.CreateTerminalResponse{TerminalId: terminalID}, nil
}

func (c *e2eClient) KillTerminalCommand(_ context.Context, params acpproto.KillTerminalCommandRequest) (acpproto.KillTerminalCommandResponse, error) {
	c.mu.Lock()
	c.killTerminalReqs = append(c.killTerminalReqs, params)
	c.mu.Unlock()
	return acpproto.KillTerminalCommandResponse{}, nil
}

func (c *e2eClient) TerminalOutput(_ context.Context, params acpproto.TerminalOutputRequest) (acpproto.TerminalOutputResponse, error) {
	c.mu.Lock()
	c.outputTerminalReqs = append(c.outputTerminalReqs, params)
	output := c.terminalOutput
	c.mu.Unlock()
	return acpproto.TerminalOutputResponse{
		Output:    output,
		Truncated: false,
	}, nil
}

func (c *e2eClient) ReleaseTerminal(_ context.Context, params acpproto.ReleaseTerminalRequest) (acpproto.ReleaseTerminalResponse, error) {
	c.mu.Lock()
	c.releaseTerminalReq = append(c.releaseTerminalReq, params)
	c.mu.Unlock()
	return acpproto.ReleaseTerminalResponse{}, nil
}

func (c *e2eClient) WaitForTerminalExit(_ context.Context, params acpproto.WaitForTerminalExitRequest) (acpproto.WaitForTerminalExitResponse, error) {
	c.mu.Lock()
	c.waitTerminalReqs = append(c.waitTerminalReqs, params)
	exitCode := c.terminalExitCode
	signal := c.terminalSignal
	c.mu.Unlock()

	resp := acpproto.WaitForTerminalExitResponse{ExitCode: &exitCode}
	if strings.TrimSpace(signal) != "" {
		resp.Signal = &signal
	}
	return resp, nil
}

func (c *e2eClient) updatesSnapshot() []acpproto.SessionNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.SessionNotification(nil), c.updates...)
}

func (c *e2eClient) permissionRequestsSnapshot() []acpproto.RequestPermissionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.RequestPermissionRequest(nil), c.permissionRequests...)
}

func (c *e2eClient) readRequestsSnapshot() []acpproto.ReadTextFileRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.ReadTextFileRequest(nil), c.readRequests...)
}

func (c *e2eClient) writeRequestsSnapshot() []acpproto.WriteTextFileRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.WriteTextFileRequest(nil), c.writeRequests...)
}

func (c *e2eClient) createTerminalRequestsSnapshot() []acpproto.CreateTerminalRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.CreateTerminalRequest(nil), c.createTerminalReqs...)
}

func (c *e2eClient) waitTerminalRequestsSnapshot() []acpproto.WaitForTerminalExitRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.WaitForTerminalExitRequest(nil), c.waitTerminalReqs...)
}

func (c *e2eClient) terminalOutputRequestsSnapshot() []acpproto.TerminalOutputRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.TerminalOutputRequest(nil), c.outputTerminalReqs...)
}

func (c *e2eClient) releaseTerminalRequestsSnapshot() []acpproto.ReleaseTerminalRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]acpproto.ReleaseTerminalRequest(nil), c.releaseTerminalReq...)
}

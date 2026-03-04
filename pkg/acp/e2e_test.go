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
	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
	"github.com/cexll/agentsdk-go/pkg/runtime/tasks"
	"github.com/cexll/agentsdk-go/pkg/tool"
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

	if _, err := h.clientConn.SetSessionMode(ctx, acpproto.SetSessionModeRequest{
		SessionId: sess.SessionId,
		ModeId:    modeCodeID,
	}); err != nil {
		t.Fatalf("set session mode failed: %v", err)
	}

	var selectedConfig *acpproto.SessionConfigOptionSelect
	for _, option := range sess.ConfigOptions {
		if option.Select != nil && option.Select.Id == configSessionModeID {
			selectedConfig = option.Select
			break
		}
	}
	if selectedConfig == nil {
		t.Fatalf("mode config option not found")
	}
	setConfigResp, err := h.clientConn.SetSessionConfigOption(ctx, acpproto.SetSessionConfigOptionRequest{
		SessionId: sess.SessionId,
		ConfigId:  configSessionModeID,
		Value:     modeConfigValue(modeArchitectID),
	})
	if err != nil {
		t.Fatalf("set session config option failed: %v", err)
	}
	var modeConfigUpdated bool
	for _, option := range setConfigResp.ConfigOptions {
		if option.Select == nil || option.Select.Id != configSessionModeID {
			continue
		}
		modeConfigUpdated = option.Select.CurrentValue == modeConfigValue(modeArchitectID)
	}
	if !modeConfigUpdated {
		t.Fatalf("mode config option was not updated to %q", modeConfigValue(modeArchitectID))
	}

	updates = client.updatesSnapshot()
	var sawModeCode bool
	var sawModeArchitect bool
	var sawConfigCode bool
	var sawConfigArchitect bool
	for _, update := range updates {
		if update.SessionId != sess.SessionId {
			continue
		}
		if update.Update.CurrentModeUpdate != nil {
			switch update.Update.CurrentModeUpdate.CurrentModeId {
			case modeCodeID:
				sawModeCode = true
			case modeArchitectID:
				sawModeArchitect = true
			}
		}
		if update.Update.ConfigOptionUpdate != nil {
			for _, option := range update.Update.ConfigOptionUpdate.ConfigOptions {
				if option.Select == nil || option.Select.Id != configSessionModeID {
					continue
				}
				switch option.Select.CurrentValue {
				case modeConfigValue(modeCodeID):
					sawConfigCode = true
				case modeConfigValue(modeArchitectID):
					sawConfigArchitect = true
				}
			}
		}
	}
	if !sawModeCode || !sawModeArchitect {
		t.Fatalf("expected current_mode_update notifications for code and architect; got code=%v architect=%v", sawModeCode, sawModeArchitect)
	}
	if !sawConfigCode || !sawConfigArchitect {
		t.Fatalf("expected config_option_update notifications for code and architect; got code=%v architect=%v", sawConfigCode, sawConfigArchitect)
	}
}

func TestACPInprocAuthenticateRoundTrip(t *testing.T) {
	root := t.TempDir()
	client := newE2EClient()
	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, stubModel{}), client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.clientConn.Authenticate(ctx, acpproto.AuthenticateRequest{MethodId: "none"}); err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
}

func TestACPInprocPromptSupportsProtocolContentBlocks(t *testing.T) {
	root := t.TempDir()
	capture := &capturingModel{}
	client := newE2EClient()
	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, capture), client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})
	sess := mustNewSession(t, h.clientConn, root, nil)

	markdown := "text/markdown"
	pdfMime := "application/pdf"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt: []acpproto.ContentBlock{
			acpproto.TextBlock("hello from text"),
			acpproto.ImageBlock("aGVsbG8=", "image/png"),
			acpproto.ResourceLinkBlock("doc-link", "file:///repo/doc.md"),
			acpproto.ResourceBlock(acpproto.EmbeddedResourceResource{
				TextResourceContents: &acpproto.TextResourceContents{
					Uri:      "file:///repo/embedded.md",
					Text:     "embedded context",
					MimeType: &markdown,
				},
			}),
			acpproto.ResourceBlock(acpproto.EmbeddedResourceResource{
				BlobResourceContents: &acpproto.BlobResourceContents{
					Uri:      "file:///repo/blob.pdf",
					Blob:     "UEZERGF0YQ==",
					MimeType: &pdfMime,
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if resp.StopReason != acpproto.StopReasonEndTurn {
		t.Fatalf("stopReason=%q, want %q", resp.StopReason, acpproto.StopReasonEndTurn)
	}

	req, ok := capture.lastRequest()
	if !ok {
		t.Fatalf("model did not receive a request")
	}
	if len(req.Messages) == 0 {
		t.Fatalf("request has no messages")
	}
	msg := req.Messages[len(req.Messages)-1]

	if !strings.Contains(msg.Content, "hello from text") {
		t.Fatalf("message content missing text block, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "embedded context") {
		t.Fatalf("message content missing embedded text resource, got %q", msg.Content)
	}
	for _, want := range []string{
		"Resource: file:///repo/doc.md",
		"Resource: file:///repo/embedded.md",
		"Resource: file:///repo/blob.pdf",
	} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("message content missing %q, got %q", want, msg.Content)
		}
	}

	var sawTextBlock bool
	var sawImageBlock bool
	var sawResourceLinkDoc bool
	var sawBlobDoc bool
	for _, block := range msg.ContentBlocks {
		switch block.Type {
		case model.ContentBlockText:
			if block.Text == "hello from text" {
				sawTextBlock = true
			}
		case model.ContentBlockImage:
			if block.MediaType == "image/png" && block.Data == "aGVsbG8=" {
				sawImageBlock = true
			}
		case model.ContentBlockDocument:
			if block.URL == "file:///repo/doc.md" {
				sawResourceLinkDoc = true
			}
			if block.Data == "UEZERGF0YQ==" && block.MediaType == "application/pdf" {
				sawBlobDoc = true
			}
		}
	}
	if !sawTextBlock || !sawImageBlock || !sawResourceLinkDoc || !sawBlobDoc {
		t.Fatalf(
			"unexpected content block coverage text=%v image=%v resource_link_doc=%v blob_doc=%v blocks=%+v",
			sawTextBlock,
			sawImageBlock,
			sawResourceLinkDoc,
			sawBlobDoc,
			msg.ContentBlocks,
		)
	}
}

func TestACPInprocToolCallUpdatesIncludeRawInputRawOutputAndLocations(t *testing.T) {
	root := t.TempDir()
	model := newToolPlanModel([]toolPlanStep{
		{
			ToolName: "OutputRefTool",
			Args: map[string]any{
				"file_path": filepath.Join(root, "payload.txt"),
				"offset":    int64(2),
			},
		},
	})

	opts := testOptionsForRootWithModel(t, root, model)
	opts.CustomTools = []tool.Tool{
		&scriptedTool{
			name: "OutputRefTool",
			exec: func(context.Context, map[string]interface{}) (*tool.ToolResult, error) {
				return &tool.ToolResult{
					Success: true,
					Output:  strings.Repeat("x", 70*1024), // trigger output_ref persistence
				}, nil
			},
		},
	}

	client := newE2EClient()
	h := newE2EHarness(t, opts, client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})
	sess := mustNewSession(t, h.clientConn, root, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("run output ref tool")},
	})
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if resp.StopReason != acpproto.StopReasonEndTurn {
		t.Fatalf("stopReason=%q, want %q", resp.StopReason, acpproto.StopReasonEndTurn)
	}

	updates := client.updatesSnapshot()
	var sawRawInput bool
	var sawCompleted bool
	var sawRawOutputRef bool
	var sawLocation bool

	for _, update := range updates {
		if update.SessionId != sess.SessionId || update.Update.ToolCallUpdate == nil {
			continue
		}
		tu := update.Update.ToolCallUpdate

		if tu.RawInput != nil {
			if input, ok := tu.RawInput.(map[string]any); ok {
				if strings.TrimSpace(fmt.Sprint(input["file_path"])) != "" {
					sawRawInput = true
				}
			}
		}
		if tu.Status != nil && *tu.Status == acpproto.ToolCallStatusCompleted {
			sawCompleted = true
		}
		if tu.RawOutput != nil {
			if strings.Contains(fmt.Sprint(tu.RawOutput), "Output saved to:") {
				sawRawOutputRef = true
			}
		}
		for _, location := range tu.Locations {
			if filepath.IsAbs(strings.TrimSpace(location.Path)) {
				sawLocation = true
			}
		}
	}

	if !sawRawInput || !sawCompleted || !sawRawOutputRef || !sawLocation {
		t.Fatalf(
			"missing expected tool_call_update fields rawInput=%v completed=%v rawOutputRef=%v location=%v",
			sawRawInput,
			sawCompleted,
			sawRawOutputRef,
			sawLocation,
		)
	}
}

func TestACPInprocToolCallFailureMapsToFailedStatus(t *testing.T) {
	root := t.TempDir()
	model := newToolPlanModel([]toolPlanStep{
		{
			ToolName: "FailTool",
			Args: map[string]any{
				"reason": "boom",
			},
		},
	})

	opts := testOptionsForRootWithModel(t, root, model)
	opts.CustomTools = []tool.Tool{
		&scriptedTool{
			name: "FailTool",
			exec: func(context.Context, map[string]interface{}) (*tool.ToolResult, error) {
				return &tool.ToolResult{
					Success: false,
					Output:  "tool failed",
				}, errors.New("boom")
			},
		},
	}

	client := newE2EClient()
	h := newE2EHarness(t, opts, client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})
	sess := mustNewSession(t, h.clientConn, root, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("run failing tool")},
	}); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	updates := client.updatesSnapshot()
	var sawFailed bool
	for _, update := range updates {
		if update.SessionId != sess.SessionId || update.Update.ToolCallUpdate == nil {
			continue
		}
		status := update.Update.ToolCallUpdate.Status
		if status != nil && *status == acpproto.ToolCallStatusFailed {
			sawFailed = true
			break
		}
	}
	if !sawFailed {
		t.Fatalf("expected failed tool_call_update status, got %+v", updates)
	}
}

func TestACPInprocSessionAdvertisesAvailableCommands(t *testing.T) {
	root := t.TempDir()
	opts := testOptionsForRootWithModel(t, root, stubModel{})
	opts.Commands = []api.CommandRegistration{
		{
			Definition: commands.Definition{
				Name:        "plan",
				Description: "Create an implementation plan",
			},
			Handler: commands.HandlerFunc(func(context.Context, commands.Invocation) (commands.Result, error) {
				return commands.Result{Output: "ok"}, nil
			}),
		},
	}

	client := newE2EClient()
	h := newE2EHarness(t, opts, client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})

	sess := mustNewSession(t, h.clientConn, root, nil)
	requireEventually(t, 2*time.Second, func() bool {
		updates := client.updatesSnapshot()
		for _, update := range updates {
			if update.SessionId != sess.SessionId {
				continue
			}
			available := update.Update.AvailableCommandsUpdate
			if available == nil {
				continue
			}
			for _, cmd := range available.AvailableCommands {
				if cmd.Name == "plan" {
					return true
				}
			}
		}
		return false
	}, "available_commands_update notification")
}

func TestACPInprocLoadSessionForExistingSessionEmitsCommandsUpdate(t *testing.T) {
	root := t.TempDir()
	opts := testOptionsForRootWithModel(t, root, stubModel{})
	opts.Commands = []api.CommandRegistration{
		{
			Definition: commands.Definition{
				Name:        "plan",
				Description: "Create an implementation plan",
			},
			Handler: commands.HandlerFunc(func(context.Context, commands.Invocation) (commands.Result, error) {
				return commands.Result{Output: "ok"}, nil
			}),
		},
	}

	client := newE2EClient()
	h := newE2EHarness(t, opts, client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})
	sess := mustNewSession(t, h.clientConn, root, nil)
	before := len(client.updatesSnapshot())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	loadResp, err := h.clientConn.LoadSession(ctx, acpproto.LoadSessionRequest{
		SessionId:  sess.SessionId,
		Cwd:        root,
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("load existing session failed: %v", err)
	}
	if loadResp.Modes == nil || len(loadResp.ConfigOptions) == 0 {
		t.Fatalf("load response missing modes/config options")
	}

	requireEventually(t, 2*time.Second, func() bool {
		updates := client.updatesSnapshot()
		if len(updates) <= before {
			return false
		}
		for _, update := range updates[before:] {
			if update.SessionId != sess.SessionId || update.Update.AvailableCommandsUpdate == nil {
				continue
			}
			for _, cmd := range update.Update.AvailableCommandsUpdate.AvailableCommands {
				if cmd.Name == "plan" {
					return true
				}
			}
		}
		return false
	}, "available_commands_update after loading existing session")
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

func TestACPServeStdioEndToEnd(t *testing.T) {
	root := t.TempDir()
	serverSide, clientSide := net.Pipe()
	client := newE2EClient()
	opts := testOptionsForRootWithModel(t, root, stubModel{})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- ServeStdio(ctx, opts, serverSide, serverSide)
	}()

	conn := acpproto.NewClientSideConnection(client, clientSide, clientSide)
	initResp := initializeACP(t, conn, acpproto.ClientCapabilities{})
	if initResp.ProtocolVersion != acpproto.ProtocolVersionNumber {
		t.Fatalf("protocolVersion=%d, want %d", initResp.ProtocolVersion, acpproto.ProtocolVersionNumber)
	}

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer rpcCancel()

	if _, err := conn.Authenticate(rpcCtx, acpproto.AuthenticateRequest{MethodId: "none"}); err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
	sess := mustNewSession(t, conn, root, nil)
	if _, err := conn.Prompt(rpcCtx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("hello")},
	}); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	_ = clientSide.Close()
	_ = serverSide.Close()
	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("serve stdio failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for ServeStdio to exit")
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type promptResult struct {
		resp acpproto.PromptResponse
		err  error
	}
	firstPromptDone := make(chan promptResult, 1)
	go func() {
		resp, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
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

	_, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
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

	if err := h.clientConn.Cancel(ctx, acpproto.CancelNotification{
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	decision, handled, err := h.adapter.requestPermissionFromClient(ctx, acpproto.SessionId("sess-perm"), api.PermissionRequest{
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

func TestACPInprocModePermissionPolicies(t *testing.T) {
	root := t.TempDir()
	client := newE2EClient()
	client.permissionOutcome = permissionOptionAllowOnce
	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, stubModel{}), client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})
	sess := mustNewSession(t, h.clientConn, root, nil)

	state, ok := h.adapter.sessionByID(sess.SessionId)
	if !ok {
		t.Fatalf("session state not found")
	}
	bridge := h.adapter.newPermissionBridge(state, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	decision, err := bridge(ctx, api.PermissionRequest{ToolName: "Read"})
	if err != nil {
		t.Fatalf("ask mode permission bridge failed: %v", err)
	}
	if decision != "allow" {
		t.Fatalf("ask mode decision=%q, want allow from client response", decision)
	}
	if count := len(client.permissionRequestsSnapshot()); count != 1 {
		t.Fatalf("ask mode permission requests=%d, want 1", count)
	}

	if _, err := h.clientConn.SetSessionMode(ctx, acpproto.SetSessionModeRequest{
		SessionId: sess.SessionId,
		ModeId:    modeCodeID,
	}); err != nil {
		t.Fatalf("set mode code failed: %v", err)
	}
	decision, err = bridge(ctx, api.PermissionRequest{ToolName: "Write"})
	if err != nil {
		t.Fatalf("code mode permission bridge failed: %v", err)
	}
	if decision != "allow" {
		t.Fatalf("code mode decision=%q, want allow", decision)
	}
	if count := len(client.permissionRequestsSnapshot()); count != 1 {
		t.Fatalf("code mode should not request client permission; got %d requests", count)
	}

	if _, err := h.clientConn.SetSessionMode(ctx, acpproto.SetSessionModeRequest{
		SessionId: sess.SessionId,
		ModeId:    modeArchitectID,
	}); err != nil {
		t.Fatalf("set mode architect failed: %v", err)
	}
	decision, err = bridge(ctx, api.PermissionRequest{ToolName: "Write"})
	if err != nil {
		t.Fatalf("architect mode write check failed: %v", err)
	}
	if decision != "deny" {
		t.Fatalf("architect mode write decision=%q, want deny", decision)
	}
	decision, err = bridge(ctx, api.PermissionRequest{ToolName: "Read"})
	if err != nil {
		t.Fatalf("architect mode read check failed: %v", err)
	}
	if decision != "allow" {
		t.Fatalf("architect mode read decision=%q, want allow", decision)
	}
	if count := len(client.permissionRequestsSnapshot()); count != 1 {
		t.Fatalf("architect mode should not request client permission; got %d requests", count)
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

	updates := client.updatesSnapshot()
	var sawToolCall bool
	var sawInProgress bool
	var sawCompleted bool
	var sawTerminalContent bool
	for _, update := range updates {
		if update.SessionId != sess.SessionId {
			continue
		}
		if update.Update.ToolCall != nil {
			sawToolCall = true
		}
		if update.Update.ToolCallUpdate == nil {
			continue
		}
		if update.Update.ToolCallUpdate.Status != nil {
			switch *update.Update.ToolCallUpdate.Status {
			case acpproto.ToolCallStatusInProgress:
				sawInProgress = true
			case acpproto.ToolCallStatusCompleted:
				sawCompleted = true
			}
		}
		for _, content := range update.Update.ToolCallUpdate.Content {
			if content.Terminal != nil && strings.TrimSpace(content.Terminal.TerminalId) != "" {
				sawTerminalContent = true
			}
		}
	}
	if !sawToolCall || !sawInProgress || !sawCompleted {
		t.Fatalf("expected tool lifecycle updates; got start=%v in_progress=%v completed=%v", sawToolCall, sawInProgress, sawCompleted)
	}
	if !sawTerminalContent {
		t.Fatalf("expected terminal content in tool_call_update")
	}
}

func TestACPInprocCapabilityBridgeEditUsesClientFs(t *testing.T) {
	root := t.TempDir()
	client := newE2EClient()
	client.readContent = "alpha beta beta"

	caps := acpproto.ClientCapabilities{}
	caps.Fs.ReadTextFile = true
	caps.Fs.WriteTextFile = true

	model := newToolPlanModel([]toolPlanStep{
		{
			ToolName: "Edit",
			Args: map[string]any{
				"file_path":   filepath.Join(root, "edit.txt"),
				"old_string":  "beta",
				"new_string":  "gamma",
				"replace_all": true,
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
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("edit through client fs capability")},
	})
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if resp.StopReason != acpproto.StopReasonEndTurn {
		t.Fatalf("stopReason=%q, want %q", resp.StopReason, acpproto.StopReasonEndTurn)
	}

	readReqs := client.readRequestsSnapshot()
	if len(readReqs) == 0 {
		t.Fatalf("expected fs/read_text_file requests for Edit")
	}
	if readReqs[0].SessionId != sess.SessionId {
		t.Fatalf("read sessionId=%q, want %q", readReqs[0].SessionId, sess.SessionId)
	}

	writeReqs := client.writeRequestsSnapshot()
	if len(writeReqs) == 0 {
		t.Fatalf("expected fs/write_text_file requests for Edit")
	}
	if got, want := writeReqs[0].Content, "alpha gamma gamma"; got != want {
		t.Fatalf("write content=%q, want %q", got, want)
	}
}

func TestACPInprocTaskToolsEmitPlanUpdates(t *testing.T) {
	root := t.TempDir()
	taskStore := tasks.NewTaskStore()
	seedTask, err := taskStore.Create("Seed task", "seed description", "seed-form")
	if err != nil {
		t.Fatalf("seed task create failed: %v", err)
	}

	model := newToolPlanModel([]toolPlanStep{
		{
			ToolName: "TaskCreate",
			Args: map[string]any{
				"subject":    "Generated task",
				"activeForm": "generated-form",
			},
		},
		{
			ToolName: "TaskUpdate",
			Args: map[string]any{
				"taskId": seedTask.ID,
				"status": "in_progress",
			},
		},
		{
			ToolName: "TaskGet",
			Args: map[string]any{
				"taskId": seedTask.ID,
			},
		},
		{
			ToolName: "TaskList",
			Args: map[string]any{
				"status": "in_progress",
			},
		},
	})

	opts := testOptionsForRootWithModel(t, root, model)
	opts.TaskStore = taskStore
	client := newE2EClient()
	h := newE2EHarness(t, opts, client)
	initializeACP(t, h.clientConn, acpproto.ClientCapabilities{})
	sess := mustNewSession(t, h.clientConn, root, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("manage tasks and keep plan updated")},
	})
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if resp.StopReason != acpproto.StopReasonEndTurn {
		t.Fatalf("stopReason=%q, want %q", resp.StopReason, acpproto.StopReasonEndTurn)
	}

	updates := client.updatesSnapshot()
	var sawPlanUpdate bool
	var sawSeedTaskEntry bool
	var sawGeneratedTaskEntry bool
	for _, update := range updates {
		if update.SessionId != sess.SessionId || update.Update.Plan == nil {
			continue
		}
		sawPlanUpdate = true
		for _, entry := range update.Update.Plan.Entries {
			if entry.Content == "Seed task" && entry.Status == acpproto.PlanEntryStatusInProgress {
				sawSeedTaskEntry = true
			}
			if strings.Contains(entry.Content, "Task ") {
				sawGeneratedTaskEntry = true
			}
		}
	}
	if !sawPlanUpdate {
		t.Fatalf("expected plan session updates from task tools")
	}
	if !sawSeedTaskEntry {
		t.Fatalf("expected in_progress plan entry for seed task, got %+v", updates)
	}
	if !sawGeneratedTaskEntry {
		t.Fatalf("expected generated task plan entry from TaskCreate, got %+v", updates)
	}
}

func TestACPInprocArchitectModeBlocksMutatingCapabilityTools(t *testing.T) {
	root := t.TempDir()
	client := newE2EClient()
	caps := acpproto.ClientCapabilities{}
	caps.Fs.WriteTextFile = true
	caps.Terminal = true

	model := newToolPlanModel([]toolPlanStep{
		{
			ToolName: "Write",
			Args: map[string]any{
				"file_path": filepath.Join(root, "out.txt"),
				"content":   "blocked",
			},
		},
		{
			ToolName: "Bash",
			Args: map[string]any{
				"command": "echo should-not-run",
				"workdir": root,
			},
		},
	})

	h := newE2EHarness(t, testOptionsForRootWithModel(t, root, model), client)
	initializeACP(t, h.clientConn, caps)
	sess := mustNewSession(t, h.clientConn, root, nil)

	modeCtx, modeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer modeCancel()

	if _, err := h.clientConn.SetSessionMode(modeCtx, acpproto.SetSessionModeRequest{
		SessionId: sess.SessionId,
		ModeId:    modeArchitectID,
	}); err != nil {
		t.Fatalf("set mode architect failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := h.clientConn.Prompt(ctx, acpproto.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("attempt mutations")},
	}); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	if writes := client.writeRequestsSnapshot(); len(writes) != 0 {
		t.Fatalf("architect mode should block write capability; got %d write requests", len(writes))
	}
	if creates := client.createTerminalRequestsSnapshot(); len(creates) != 0 {
		t.Fatalf("architect mode should block terminal execution; got %d create_terminal requests", len(creates))
	}
	if perms := client.permissionRequestsSnapshot(); len(perms) != 0 {
		t.Fatalf("architect mode should not prompt for mutating tools; got %d permission requests", len(perms))
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

type capturingModel struct {
	mu   sync.Mutex
	reqs []model.Request
}

func (m *capturingModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
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
		return nil, errors.New("capturingModel: no final response")
	}
	return final, nil
}

func (m *capturingModel) CompleteStream(_ context.Context, req model.Request, cb model.StreamHandler) error {
	m.mu.Lock()
	m.reqs = append(m.reqs, cloneModelRequest(req))
	m.mu.Unlock()

	if cb == nil {
		return nil
	}
	return cb(model.StreamResult{
		Delta: "ok",
		Final: true,
		Response: &model.Response{
			Message: model.Message{
				Role:    "assistant",
				Content: "ok",
			},
			StopReason: "end_turn",
		},
	})
}

func (m *capturingModel) lastRequest() (model.Request, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.reqs) == 0 {
		return model.Request{}, false
	}
	return cloneModelRequest(m.reqs[len(m.reqs)-1]), true
}

func cloneModelRequest(req model.Request) model.Request {
	cp := req
	if len(req.Messages) > 0 {
		cp.Messages = make([]model.Message, len(req.Messages))
		for i, msg := range req.Messages {
			cp.Messages[i] = msg
			if len(msg.ContentBlocks) > 0 {
				cp.Messages[i].ContentBlocks = append([]model.ContentBlock(nil), msg.ContentBlocks...)
			}
			if len(msg.ToolCalls) > 0 {
				cp.Messages[i].ToolCalls = append([]model.ToolCall(nil), msg.ToolCalls...)
			}
		}
	}
	if len(req.Tools) > 0 {
		cp.Tools = append([]model.ToolDefinition(nil), req.Tools...)
	}
	return cp
}

type scriptedTool struct {
	name string
	exec func(context.Context, map[string]interface{}) (*tool.ToolResult, error)
}

func (t *scriptedTool) Name() string {
	if t == nil {
		return ""
	}
	return t.name
}

func (t *scriptedTool) Description() string {
	if t == nil {
		return ""
	}
	return "scripted e2e tool"
}

func (t *scriptedTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
	}
}

func (t *scriptedTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if t == nil || t.exec == nil {
		return &tool.ToolResult{Success: true, Output: "ok"}, nil
	}
	return t.exec(ctx, params)
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

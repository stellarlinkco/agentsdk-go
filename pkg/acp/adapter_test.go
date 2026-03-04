package acp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/message"
	"github.com/cexll/agentsdk-go/pkg/model"
	acpproto "github.com/coder/acp-go-sdk"
)

type stubModel struct{}

func (stubModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return &model.Response{
		Message: model.Message{
			Role:    "assistant",
			Content: "ok",
		},
		StopReason: "end_turn",
	}, nil
}

func (stubModel) CompleteStream(_ context.Context, _ model.Request, cb model.StreamHandler) error {
	if cb == nil {
		return nil
	}
	if err := cb(model.StreamResult{Delta: "o"}); err != nil {
		return err
	}
	if err := cb(model.StreamResult{Delta: "k"}); err != nil {
		return err
	}
	return cb(model.StreamResult{
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

type blockingStreamModel struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *blockingStreamModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return &model.Response{
		Message: model.Message{
			Role:    "assistant",
			Content: "ok",
		},
		StopReason: "end_turn",
	}, nil
}

func (m *blockingStreamModel) CompleteStream(ctx context.Context, _ model.Request, cb model.StreamHandler) error {
	m.once.Do(func() { close(m.started) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.release:
	}

	if cb == nil {
		return nil
	}
	return cb(model.StreamResult{
		Final: true,
		Response: &model.Response{
			Message: model.Message{
				Role:    "assistant",
				Content: "released",
			},
			StopReason: "end_turn",
		},
	})
}

func testOptions(t *testing.T) api.Options {
	t.Helper()
	return testOptionsForRoot(t, t.TempDir())
}

func testOptionsForRoot(t *testing.T, root string) api.Options {
	t.Helper()
	return testOptionsForRootWithModel(t, root, stubModel{})
}

func testOptionsForRootWithModel(t *testing.T, root string, mdl model.Model) api.Options {
	t.Helper()
	return api.Options{
		ProjectRoot: root,
		ModelFactory: api.ModelFactoryFunc(func(context.Context) (model.Model, error) {
			return mdl, nil
		}),
	}
}

func TestAdapterImplementsInterfaces(t *testing.T) {
	t.Parallel()

	var _ acpproto.Agent = (*Adapter)(nil)
	var _ acpproto.AgentLoader = (*Adapter)(nil)
}

func TestInitializeNegotiatesBaseFields(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter(testOptions(t))
	resp, err := adapter.Initialize(context.Background(), acpproto.InitializeRequest{
		ProtocolVersion: acpproto.ProtocolVersionNumber,
	})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if resp.ProtocolVersion != acpproto.ProtocolVersionNumber {
		t.Fatalf("protocolVersion=%d, want %d", resp.ProtocolVersion, acpproto.ProtocolVersionNumber)
	}
	if !resp.AgentCapabilities.LoadSession {
		t.Fatalf("expected loadSession capability")
	}
	if resp.AuthMethods == nil {
		t.Fatalf("expected authMethods to be initialized")
	}
}

func TestAuthenticateIsNoOpSuccess(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter(testOptions(t))
	if _, err := adapter.Authenticate(context.Background(), acpproto.AuthenticateRequest{MethodId: "noop"}); err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
}

func TestNewSessionRequiresAbsoluteCWD(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter(testOptions(t))
	_, err := adapter.NewSession(context.Background(), acpproto.NewSessionRequest{
		Cwd:        "relative/path",
		McpServers: []acpproto.McpServer{},
	})
	if err == nil {
		t.Fatalf("expected relative cwd to be rejected")
	}

	var reqErr *acpproto.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected ACP request error, got %T", err)
	}
	if reqErr.Code != -32602 {
		t.Fatalf("error code=%d, want -32602", reqErr.Code)
	}
}

func TestSetSessionConfigOptionUpdatesSnapshot(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter(testOptions(t))
	cwd := t.TempDir()

	newSession, err := adapter.NewSession(context.Background(), acpproto.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}
	if len(newSession.ConfigOptions) == 0 {
		t.Fatalf("expected default config options")
	}

	option := newSession.ConfigOptions[0].Select
	if option == nil {
		t.Fatalf("expected select config option")
	}

	targetValue := modeConfigValue(modeCodeID)
	if option.CurrentValue == targetValue {
		targetValue = modeConfigValue(modeArchitectID)
	}

	resp, err := adapter.SetSessionConfigOption(context.Background(), acpproto.SetSessionConfigOptionRequest{
		SessionId: newSession.SessionId,
		ConfigId:  option.Id,
		Value:     targetValue,
	})
	if err != nil {
		t.Fatalf("set session config option failed: %v", err)
	}

	if len(resp.ConfigOptions) != len(newSession.ConfigOptions) {
		t.Fatalf("config options len=%d, want %d", len(resp.ConfigOptions), len(newSession.ConfigOptions))
	}

	updated := resp.ConfigOptions[0].Select
	if updated == nil {
		t.Fatalf("expected updated select option")
	}
	if updated.CurrentValue != targetValue {
		t.Fatalf("currentValue=%q, want %q", updated.CurrentValue, targetValue)
	}

	state, ok := adapter.sessionByID(newSession.SessionId)
	if !ok {
		t.Fatalf("session not found after config update")
	}
	if got := state.currentMode(); got != configValueToMode(targetValue) {
		t.Fatalf("currentMode=%q, want %q", got, configValueToMode(targetValue))
	}
}

func TestSetSessionModeSynchronizesConfigOption(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter(testOptions(t))
	newSession, err := adapter.NewSession(context.Background(), acpproto.NewSessionRequest{
		Cwd:        t.TempDir(),
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}

	if _, err := adapter.SetSessionMode(context.Background(), acpproto.SetSessionModeRequest{
		SessionId: newSession.SessionId,
		ModeId:    modeArchitectID,
	}); err != nil {
		t.Fatalf("set session mode failed: %v", err)
	}

	state, ok := adapter.sessionByID(newSession.SessionId)
	if !ok {
		t.Fatalf("session not found after mode update")
	}
	options := state.snapshotConfigOptions()
	if len(options) == 0 || options[0].Select == nil {
		t.Fatalf("expected config options after mode update")
	}
	if got := options[0].Select.CurrentValue; got != modeConfigValue(modeArchitectID) {
		t.Fatalf("currentValue=%q, want %q", got, modeConfigValue(modeArchitectID))
	}
}

func TestNewSessionExposesProtocolModeSet(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter(testOptions(t))
	newSession, err := adapter.NewSession(context.Background(), acpproto.NewSessionRequest{
		Cwd:        t.TempDir(),
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}
	if newSession.Modes == nil {
		t.Fatalf("expected modes")
	}
	if got := newSession.Modes.CurrentModeId; got != modeAskID {
		t.Fatalf("currentModeId=%q, want %q", got, modeAskID)
	}
	if len(newSession.Modes.AvailableModes) != 3 {
		t.Fatalf("availableModes len=%d, want 3", len(newSession.Modes.AvailableModes))
	}

	modeIDs := map[acpproto.SessionModeId]bool{}
	for _, mode := range newSession.Modes.AvailableModes {
		modeIDs[mode.Id] = true
	}
	for _, want := range []acpproto.SessionModeId{modeAskID, modeArchitectID, modeCodeID} {
		if !modeIDs[want] {
			t.Fatalf("missing mode %q in availableModes", want)
		}
	}

	if len(newSession.ConfigOptions) == 0 || newSession.ConfigOptions[0].Select == nil {
		t.Fatalf("expected session mode config option")
	}
	selectOpt := newSession.ConfigOptions[0].Select
	if selectOpt.Id != configSessionModeID {
		t.Fatalf("config option id=%q, want %q", selectOpt.Id, configSessionModeID)
	}
	if selectOpt.CurrentValue != modeConfigValue(modeAskID) {
		t.Fatalf("current config value=%q, want %q", selectOpt.CurrentValue, modeConfigValue(modeAskID))
	}
	if selectOpt.Category == nil || selectOpt.Category.Other == nil || *selectOpt.Category.Other != "mode" {
		t.Fatalf("expected config option category mode")
	}
}

func TestSetSessionModeRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter(testOptions(t))
	newSession, err := adapter.NewSession(context.Background(), acpproto.NewSessionRequest{
		Cwd:        t.TempDir(),
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}

	_, err = adapter.SetSessionMode(context.Background(), acpproto.SetSessionModeRequest{
		SessionId: newSession.SessionId,
		ModeId:    acpproto.SessionModeId("not-a-real-mode"),
	})
	if err == nil {
		t.Fatalf("expected set mode to fail for unknown mode")
	}

	var reqErr *acpproto.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected ACP request error, got %T", err)
	}
	if reqErr.Code != -32602 {
		t.Fatalf("error code=%d, want -32602", reqErr.Code)
	}
}

func TestLoadSessionRejectsMissingPersistedSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	adapter := NewAdapter(testOptionsForRoot(t, root))
	_, err := adapter.LoadSession(context.Background(), acpproto.LoadSessionRequest{
		SessionId:  "missing",
		Cwd:        root,
		McpServers: []acpproto.McpServer{},
	})
	if err == nil {
		t.Fatalf("expected missing session to be rejected")
	}

	var reqErr *acpproto.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected ACP request error, got %T", err)
	}
	if reqErr.Code != -32602 {
		t.Fatalf("error code=%d, want -32602", reqErr.Code)
	}
}

func TestLoadSessionFromPersistedHistory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionID := acpproto.SessionId("persisted-session")
	if err := api.SavePersistedHistory(root, string(sessionID), []message.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}); err != nil {
		t.Fatalf("save persisted history: %v", err)
	}

	adapter := NewAdapter(testOptionsForRoot(t, root))
	resp, err := adapter.LoadSession(context.Background(), acpproto.LoadSessionRequest{
		SessionId:  sessionID,
		Cwd:        root,
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("load session failed: %v", err)
	}
	if resp.Modes == nil {
		t.Fatalf("expected modes in load response")
	}
	if len(resp.ConfigOptions) == 0 {
		t.Fatalf("expected config options in load response")
	}
}

func TestPromptRejectsConcurrentPromptForSameSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	model := &blockingStreamModel{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	adapter := NewAdapter(testOptionsForRootWithModel(t, root, model))
	newSession, err := adapter.NewSession(context.Background(), acpproto.NewSessionRequest{
		Cwd:        root,
		McpServers: []acpproto.McpServer{},
	})
	if err != nil {
		t.Fatalf("new session failed: %v", err)
	}
	t.Cleanup(func() {
		state, ok := adapter.sessionByID(newSession.SessionId)
		if !ok {
			return
		}
		if rt := state.runtime(); rt != nil {
			_ = rt.Close()
		}
	})

	type promptOutcome struct {
		resp acpproto.PromptResponse
		err  error
	}
	firstPromptDone := make(chan promptOutcome, 1)
	go func() {
		resp, promptErr := adapter.Prompt(context.Background(), acpproto.PromptRequest{
			SessionId: newSession.SessionId,
			Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("first")},
		})
		firstPromptDone <- promptOutcome{resp: resp, err: promptErr}
	}()

	select {
	case <-model.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first prompt to start")
	}

	_, err = adapter.Prompt(context.Background(), acpproto.PromptRequest{
		SessionId: newSession.SessionId,
		Prompt:    []acpproto.ContentBlock{acpproto.TextBlock("second")},
	})
	if err == nil {
		t.Fatalf("expected second concurrent prompt to be rejected")
	}
	var reqErr *acpproto.RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected ACP request error, got %T", err)
	}
	if reqErr.Code != -32600 {
		t.Fatalf("error code=%d, want -32600", reqErr.Code)
	}

	if err := adapter.Cancel(context.Background(), acpproto.CancelNotification{
		SessionId: newSession.SessionId,
	}); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}

	select {
	case outcome := <-firstPromptDone:
		if outcome.err != nil {
			t.Fatalf("first prompt failed: %v", outcome.err)
		}
		if outcome.resp.StopReason != acpproto.StopReasonCancelled {
			t.Fatalf("stopReason=%q, want %q", outcome.resp.StopReason, acpproto.StopReasonCancelled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first prompt to exit")
	}
}

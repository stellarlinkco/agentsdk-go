package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cexll/agentsdk-go/pkg/api"
	acpproto "github.com/coder/acp-go-sdk"
	"github.com/google/uuid"
)

// Adapter wires agentsdk-go runtime into ACP request/response methods.
type Adapter struct {
	opts api.Options

	mu       sync.RWMutex
	conn     *acpproto.AgentSideConnection
	client   acpproto.ClientCapabilities
	sessions map[string]*sessionState
}

// NewAdapter creates an ACP adapter backed by api.Runtime sessions.
func NewAdapter(opts api.Options) *Adapter {
	return &Adapter{
		opts:     opts,
		sessions: make(map[string]*sessionState),
	}
}

// SetConnection injects the live ACP connection so Prompt/updates can call back into the client.
func (a *Adapter) SetConnection(conn *acpproto.AgentSideConnection) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
}

// ServeStdio serves ACP over stdio using the provided runtime options.
func ServeStdio(ctx context.Context, opts api.Options, in io.Reader, out io.Writer) error {
	if in == nil || out == nil {
		return fmt.Errorf("acp: stdio streams are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	adapter := NewAdapter(opts)
	conn := acpproto.NewAgentSideConnection(adapter, out, in)
	adapter.SetConnection(conn)

	select {
	case <-conn.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Authenticate is a no-op success path.
func (a *Adapter) Authenticate(context.Context, acpproto.AuthenticateRequest) (acpproto.AuthenticateResponse, error) {
	return acpproto.AuthenticateResponse{}, nil
}

// Initialize negotiates ACP protocol/capabilities/auth methods.
func (a *Adapter) Initialize(_ context.Context, params acpproto.InitializeRequest) (acpproto.InitializeResponse, error) {
	a.setClientCapabilities(params.ClientCapabilities)
	return acpproto.InitializeResponse{
		ProtocolVersion: acpproto.ProtocolVersionNumber,
		AgentCapabilities: acpproto.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: acpproto.McpCapabilities{
				Http: true,
				Sse:  true,
			},
			PromptCapabilities: acpproto.PromptCapabilities{
				Image:           true,
				EmbeddedContext: true,
			},
		},
		AuthMethods: []acpproto.AuthMethod{},
	}, nil
}

// NewSession creates a runtime session for the provided absolute working directory.
func (a *Adapter) NewSession(ctx context.Context, params acpproto.NewSessionRequest) (acpproto.NewSessionResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cwd, err := validateSessionCWD(params.Cwd)
	if err != nil {
		return acpproto.NewSessionResponse{}, err
	}

	sessionID := acpproto.SessionId("sess_" + uuid.NewString())
	state, err := a.createSession(ctx, sessionID, cwd, params.McpServers)
	if err != nil {
		return acpproto.NewSessionResponse{}, err
	}
	a.registerSession(state)

	return acpproto.NewSessionResponse{
		SessionId:     sessionID,
		Modes:         state.snapshotModes(),
		ConfigOptions: state.snapshotConfigOptions(),
	}, nil
}

// LoadSession restores a persisted session and replays prior conversation updates.
func (a *Adapter) LoadSession(ctx context.Context, params acpproto.LoadSessionRequest) (acpproto.LoadSessionResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(string(params.SessionId)) == "" {
		return acpproto.LoadSessionResponse{}, acpproto.NewInvalidParams(map[string]any{
			"sessionId": "sessionId is required",
		})
	}

	cwd, err := validateSessionCWD(params.Cwd)
	if err != nil {
		return acpproto.LoadSessionResponse{}, err
	}

	if existing, ok := a.sessionByID(params.SessionId); ok {
		return acpproto.LoadSessionResponse{
			Modes:         existing.snapshotModes(),
			ConfigOptions: existing.snapshotConfigOptions(),
		}, nil
	}

	history, found, err := loadPersistedHistory(cwd, params.SessionId)
	if err != nil {
		return acpproto.LoadSessionResponse{}, acpproto.NewInternalError(map[string]any{
			"sessionId": string(params.SessionId),
			"error":     err.Error(),
		})
	}
	if !found {
		return acpproto.LoadSessionResponse{}, acpproto.NewInvalidParams(map[string]any{
			"sessionId": "session not found",
		})
	}

	state, err := a.createSession(ctx, params.SessionId, cwd, params.McpServers)
	if err != nil {
		return acpproto.LoadSessionResponse{}, err
	}
	a.registerSession(state)

	for _, update := range historyMessagesToSessionUpdates(history) {
		if err := a.emitSessionUpdate(ctx, params.SessionId, update); err != nil {
			return acpproto.LoadSessionResponse{}, err
		}
	}

	return acpproto.LoadSessionResponse{
		Modes:         state.snapshotModes(),
		ConfigOptions: state.snapshotConfigOptions(),
	}, nil
}

// Prompt streams runtime text deltas as ACP agent_message_chunk updates.
func (a *Adapter) Prompt(ctx context.Context, params acpproto.PromptRequest) (acpproto.PromptResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	state, ok := a.sessionByID(params.SessionId)
	if !ok {
		return acpproto.PromptResponse{}, acpproto.NewInvalidParams(map[string]any{
			"sessionId": "unknown session",
		})
	}

	promptText, contentBlocks := convertPromptBlocks(params.Prompt)
	if strings.TrimSpace(promptText) == "" && len(contentBlocks) == 0 {
		return acpproto.PromptResponse{}, acpproto.NewInvalidParams(map[string]any{
			"prompt": "prompt is empty",
		})
	}

	turnCtx, turnCancel := context.WithCancel(ctx)
	generation, ok := state.beginTurn(turnCancel)
	if !ok {
		turnCancel()
		return acpproto.PromptResponse{}, acpproto.NewInvalidRequest(map[string]any{
			"sessionId": "prompt is already running for this session",
		})
	}
	defer state.endTurn(generation)

	rt := state.runtime()
	if rt == nil {
		return acpproto.PromptResponse{}, acpproto.NewInternalError(map[string]any{
			"error": "runtime is not initialized",
		})
	}

	stream, err := rt.RunStream(turnCtx, api.Request{
		SessionID:     string(params.SessionId),
		Prompt:        promptText,
		ContentBlocks: contentBlocks,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(turnCtx.Err(), context.Canceled) {
			return acpproto.PromptResponse{StopReason: acpproto.StopReasonCancelled}, nil
		}
		return acpproto.PromptResponse{}, err
	}

	stopReason := acpproto.StopReasonEndTurn
	for evt := range stream {
		if delta := extractTextDelta(evt); delta != "" {
			err := a.emitSessionUpdate(turnCtx, params.SessionId, acpproto.SessionUpdate{
				AgentMessageChunk: &acpproto.SessionUpdateAgentMessageChunk{
					SessionUpdate: "agent_message_chunk",
					Content:       acpproto.TextBlock(delta),
				},
			})
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(turnCtx.Err(), context.Canceled) {
					return acpproto.PromptResponse{StopReason: acpproto.StopReasonCancelled}, nil
				}
				return acpproto.PromptResponse{}, err
			}
		}

		if reason := extractStopReason(evt); reason != "" {
			stopReason = mapStopReason(reason)
		}

		if evt.Type == api.EventError {
			if errors.Is(turnCtx.Err(), context.Canceled) || isCancelledStreamError(evt.Output) {
				return acpproto.PromptResponse{StopReason: acpproto.StopReasonCancelled}, nil
			}
			return acpproto.PromptResponse{}, streamEventError(evt)
		}
	}

	if errors.Is(turnCtx.Err(), context.Canceled) {
		stopReason = acpproto.StopReasonCancelled
	}
	return acpproto.PromptResponse{StopReason: stopReason}, nil
}

// Cancel aborts the active turn for the target session when one exists.
func (a *Adapter) Cancel(_ context.Context, params acpproto.CancelNotification) error {
	state, ok := a.sessionByID(params.SessionId)
	if !ok {
		return nil
	}
	// NOTE: AgentSideConnection already cancels Prompt request contexts before
	// invoking this method; this call keeps adapter-managed session state in sync.
	state.cancelTurn()
	return nil
}

// SetSessionMode validates and updates current mode, then emits current_mode_update.
func (a *Adapter) SetSessionMode(ctx context.Context, params acpproto.SetSessionModeRequest) (acpproto.SetSessionModeResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	state, ok := a.sessionByID(params.SessionId)
	if !ok {
		return acpproto.SetSessionModeResponse{}, acpproto.NewInvalidParams(map[string]any{
			"sessionId": "unknown session",
		})
	}
	if !state.hasMode(params.ModeId) {
		return acpproto.SetSessionModeResponse{}, acpproto.NewInvalidParams(map[string]any{
			"modeId": fmt.Sprintf("unsupported mode %q", params.ModeId),
		})
	}

	state.setMode(params.ModeId)
	if err := a.emitSessionUpdate(ctx, params.SessionId, acpproto.SessionUpdate{
		CurrentModeUpdate: &acpproto.SessionCurrentModeUpdate{
			SessionUpdate: "current_mode_update",
			CurrentModeId: params.ModeId,
		},
	}); err != nil {
		return acpproto.SetSessionModeResponse{}, err
	}
	return acpproto.SetSessionModeResponse{}, nil
}

// SetSessionConfigOption validates and updates config value, returns full config snapshot.
func (a *Adapter) SetSessionConfigOption(ctx context.Context, params acpproto.SetSessionConfigOptionRequest) (acpproto.SetSessionConfigOptionResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	state, ok := a.sessionByID(params.SessionId)
	if !ok {
		return acpproto.SetSessionConfigOptionResponse{}, acpproto.NewInvalidParams(map[string]any{
			"sessionId": "unknown session",
		})
	}

	options, err := state.setConfigOption(params.ConfigId, params.Value)
	if err != nil {
		return acpproto.SetSessionConfigOptionResponse{}, acpproto.NewInvalidParams(map[string]any{
			"configId": string(params.ConfigId),
			"value":    string(params.Value),
			"error":    err.Error(),
		})
	}

	if err := a.emitSessionUpdate(ctx, params.SessionId, acpproto.SessionUpdate{
		ConfigOptionUpdate: &acpproto.SessionConfigOptionUpdate{
			SessionUpdate: "config_option_update",
			ConfigOptions: options,
		},
	}); err != nil {
		return acpproto.SetSessionConfigOptionResponse{}, err
	}

	return acpproto.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
}

func (a *Adapter) createSession(ctx context.Context, sessionID acpproto.SessionId, cwd string, requestedMCPServers []acpproto.McpServer) (*sessionState, error) {
	state := newSessionState(sessionID, cwd)

	opts := a.opts
	opts.ProjectRoot = cwd
	mergedMCPSpecs, err := mergeMCPServerSpecs(a.opts.MCPServers, requestedMCPServers)
	if err != nil {
		return nil, acpproto.NewInvalidParams(map[string]any{
			"mcpServers": err.Error(),
		})
	}
	opts.MCPServers = mergedMCPSpecs
	bridgeTools, disallowed := buildClientCapabilityTools(sessionID, a.connection, a.clientCapabilities())
	if len(bridgeTools) > 0 {
		opts.CustomTools = append(opts.CustomTools, bridgeTools...)
		opts.DisallowedTools = append(opts.DisallowedTools, disallowed...)
	}
	basePermissionHandler := opts.PermissionRequestHandler
	opts.PermissionRequestHandler = a.newPermissionBridge(state, basePermissionHandler)

	rt, err := api.New(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("acp: create runtime: %w", err)
	}
	state.setRuntime(rt)
	return state, nil
}

func (a *Adapter) registerSession(state *sessionState) {
	if a == nil || state == nil {
		return
	}
	a.mu.Lock()
	a.sessions[sessionKey(state.id)] = state
	a.mu.Unlock()
}

func (a *Adapter) sessionByID(id acpproto.SessionId) (*sessionState, bool) {
	if a == nil {
		return nil, false
	}
	key := sessionKey(id)
	if key == "" {
		return nil, false
	}
	a.mu.RLock()
	state, ok := a.sessions[key]
	a.mu.RUnlock()
	return state, ok
}

func (a *Adapter) emitSessionUpdate(ctx context.Context, sessionID acpproto.SessionId, update acpproto.SessionUpdate) error {
	conn := a.connection()
	if conn == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return conn.SessionUpdate(ctx, acpproto.SessionNotification{
		SessionId: sessionID,
		Update:    update,
	})
}

func (a *Adapter) connection() *acpproto.AgentSideConnection {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	conn := a.conn
	a.mu.RUnlock()
	return conn
}

func (a *Adapter) setClientCapabilities(caps acpproto.ClientCapabilities) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.client = caps
	a.mu.Unlock()
}

func (a *Adapter) clientCapabilities() acpproto.ClientCapabilities {
	if a == nil {
		return acpproto.ClientCapabilities{}
	}
	a.mu.RLock()
	caps := a.client
	a.mu.RUnlock()
	return caps
}

func validateSessionCWD(cwd string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(cwd))
	if cleaned == "" || cleaned == "." {
		return "", acpproto.NewInvalidParams(map[string]any{
			"cwd": "cwd is required",
		})
	}
	if !filepath.IsAbs(cleaned) {
		return "", acpproto.NewInvalidParams(map[string]any{
			"cwd": "cwd must be an absolute path",
		})
	}
	return cleaned, nil
}

func sessionKey(id acpproto.SessionId) string {
	return strings.TrimSpace(string(id))
}

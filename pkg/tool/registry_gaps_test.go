package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/mcp"
)

func TestRegistryCloseSkipsNilSessions(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	session, err := server.newSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	r := NewRegistry()
	r.mcpSessions = []*mcpSessionInfo{
		nil,
		{serverID: "x", session: nil},
		{serverID: "y", session: session},
	}

	r.Close()

	if !server.Closed() {
		t.Fatalf("expected session to be closed")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.mcpSessions != nil {
		t.Fatalf("expected session list cleared")
	}
}

func TestConnectMCPClientWithOptionsNilTransport(t *testing.T) {
	restore := withStubMCPTransport(t, func(context.Context, string) (mcp.Transport, error) {
		return nil, nil
	})
	defer restore()

	if _, err := connectMCPClientWithOptions(context.Background(), "fake", MCPServerOptions{}, nil); err == nil || !strings.Contains(err.Error(), "transport is nil") {
		t.Fatalf("expected nil transport error, got %v", err)
	}
}

func TestConnectMCPClientWithOptionsHonorsContextCancel(t *testing.T) {
	restore := withStubMCPTransport(t, func(context.Context, string) (mcp.Transport, error) {
		return cancelOnlyTransport{}, nil
	})
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := connectMCPClientWithOptions(ctx, "fake", MCPServerOptions{}, nil)
	if err == nil {
		t.Fatalf("expected connect error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

type cancelOnlyTransport struct{}

func (cancelOnlyTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestMCPToolFilterMatchesCoverage(t *testing.T) {
	if got := normalizeMCPToolNameSet([]string{"", "  "}); got != nil {
		t.Fatalf("expected nil normalized set, got %v", got)
	}
	if got := toNameSet(nil); got != nil {
		t.Fatalf("expected nil name set for empty input, got %v", got)
	}

	filter := mcpToolFilter{}
	if filter.matches(nil, "a") {
		t.Fatalf("expected empty set mismatch")
	}
	set := map[string]struct{}{"a": {}}
	if filter.matches(set, " ") {
		t.Fatalf("expected whitespace-only mismatch")
	}
	if !filter.matches(set, " ", "a") {
		t.Fatalf("expected match after skipping blanks")
	}
}

func TestRegisterMCPServerWithOptionsNilSession(t *testing.T) {
	orig := newMCPClientWithOptions
	newMCPClientWithOptions = func(context.Context, string, MCPServerOptions, mcpListChangedHandler) (*mcp.ClientSession, error) {
		return nil, nil
	}
	t.Cleanup(func() { newMCPClientWithOptions = orig })

	err := NewRegistry().RegisterMCPServerWithOptions(context.Background(), "fake", "svc", MCPServerOptions{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "session is nil") {
		t.Fatalf("expected nil session error, got %v", err)
	}
}

func TestRegisterMCPServerWithOptionsListToolsError(t *testing.T) {
	server := &stubMCPServer{listErr: errors.New("list failed")}
	orig := newMCPClientWithOptions
	newMCPClientWithOptions = func(context.Context, string, MCPServerOptions, mcpListChangedHandler) (*mcp.ClientSession, error) {
		return server.newSession()
	}
	t.Cleanup(func() { newMCPClientWithOptions = orig })

	err := NewRegistry().RegisterMCPServerWithOptions(context.Background(), "fake", "svc", MCPServerOptions{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "list MCP tools") {
		t.Fatalf("expected list error, got %v", err)
	}
}

func TestRegisterMCPServerWithOptionsNoTools(t *testing.T) {
	server := &stubMCPServer{tools: nil}
	orig := newMCPClientWithOptions
	newMCPClientWithOptions = func(context.Context, string, MCPServerOptions, mcpListChangedHandler) (*mcp.ClientSession, error) {
		return server.newSession()
	}
	t.Cleanup(func() { newMCPClientWithOptions = orig })

	err := NewRegistry().RegisterMCPServerWithOptions(context.Background(), "fake", "svc", MCPServerOptions{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "returned no tools") {
		t.Fatalf("expected no tools error, got %v", err)
	}
}

func TestRegisterMCPServerWithOptionsRegisterMCPSessionError(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	orig := newMCPClientWithOptions
	newMCPClientWithOptions = func(context.Context, string, MCPServerOptions, mcpListChangedHandler) (*mcp.ClientSession, error) {
		return server.newSession()
	}
	t.Cleanup(func() { newMCPClientWithOptions = orig })

	r := NewRegistry()
	if err := r.Register(&spyTool{name: "svc__echo"}); err != nil {
		t.Fatalf("register preexisting tool: %v", err)
	}

	err := r.RegisterMCPServerWithOptions(context.Background(), "fake", "svc", MCPServerOptions{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected already registered error, got %v", err)
	}
}

func TestRegistryMCPToolListChangedHandlerNilRegistry(t *testing.T) {
	if handler := (*Registry)(nil).mcpToolsChangedHandler("fake"); handler != nil {
		t.Fatalf("expected nil handler for nil registry")
	}
}

func TestRemoteToolExecuteNilResultAndMarshalFallback(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "remote", InputSchema: map[string]any{"type": "object"}}}}
	session, err := server.newSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	tool := &remoteTool{name: "remote", session: session}

	server.callFn = func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		return nil, nil
	}
	res, err := tool.Execute(context.Background(), map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if res == nil || !res.Success || strings.TrimSpace(res.Output) == "" {
		t.Fatalf("expected successful fallback output, got %#v", res)
	}

	server.callFn = func(_ context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		if params == nil || params.Name != "remote" {
			return nil, errors.New("unexpected tool name")
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.ImageContent{MIMEType: "image/png"}},
		}, nil
	}
	res, err = tool.Execute(context.Background(), map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if res == nil || !res.Success {
		t.Fatalf("expected success result")
	}
	if !strings.Contains(res.Output, `"type":"image"`) {
		t.Fatalf("expected marshaled content fallback, got %q", res.Output)
	}
}

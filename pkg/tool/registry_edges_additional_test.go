package tool

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/mcp"
)

func TestRegisterMCPServerConnectContextErrorWins(t *testing.T) {
	restore := withStubMCPClient(t, func(ctx context.Context, _ string, _ mcpListChangedHandler) (*mcp.ClientSession, error) {
		<-ctx.Done()
		return nil, errors.New("dial failed")
	})
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewRegistry().RegisterMCPServer(ctx, "fake", "")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled wrapped, got %v", err)
	}
}

func TestRegisterMCPServerNilSessionRejected(t *testing.T) {
	restore := withStubMCPClient(t, func(context.Context, string, mcpListChangedHandler) (*mcp.ClientSession, error) {
		return nil, nil
	})
	defer restore()

	if err := NewRegistry().RegisterMCPServer(context.Background(), "fake", ""); err == nil || !strings.Contains(err.Error(), "session is nil") {
		t.Fatalf("expected nil session error, got %v", err)
	}
}

func TestRegisterMCPServerWithOptionsConnectContextErrorWins(t *testing.T) {
	orig := newMCPClientWithOptions
	newMCPClientWithOptions = func(ctx context.Context, _ string, _ MCPServerOptions, _ mcpListChangedHandler) (*mcp.ClientSession, error) {
		<-ctx.Done()
		return nil, errors.New("dial failed")
	}
	t.Cleanup(func() { newMCPClientWithOptions = orig })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewRegistry().RegisterMCPServerWithOptions(ctx, "fake", "", MCPServerOptions{Timeout: time.Second})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled wrapped, got %v", err)
	}
}

func TestToolListChangedHandlerNilInputs(t *testing.T) {
	t.Parallel()

	calls := 0
	h := toolListChangedHandler(func(context.Context, *mcp.ClientSession) { calls++ })
	h(context.Background(), nil)
	h(context.Background(), &mcp.ToolListChangedRequest{Session: nil})
	if calls != 0 {
		t.Fatalf("expected nil inputs to be ignored, calls=%d", calls)
	}

	var nilHandler mcpListChangedHandler
	h = toolListChangedHandler(nilHandler)
	h(context.Background(), &mcp.ToolListChangedRequest{})
}

func TestApplyMCPTransportOptionsEarlyReturns(t *testing.T) {
	t.Parallel()

	transport := &mcp.CommandTransport{Command: nil}
	if err := applyMCPTransportOptions(transport, MCPServerOptions{Headers: map[string]string{"x": "1"}}); err != nil {
		t.Fatalf("expected headers-only no-op for stdio transport, got %v", err)
	}

	if err := applyMCPTransportOptions(&mcp.SSEClientTransport{}, MCPServerOptions{Env: map[string]string{"A": "1"}}); err != nil {
		t.Fatalf("expected env-only no-op for sse transport, got %v", err)
	}

	if err := applyMCPTransportOptions(&mcp.StreamableClientTransport{}, MCPServerOptions{Env: map[string]string{"A": "1"}}); err != nil {
		t.Fatalf("expected env-only no-op for streamable transport, got %v", err)
	}
}

type roundTripRecorder struct {
	req *http.Request
}

func (r *roundTripRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestHeaderRoundTripperNilRequestAndBlankValues(t *testing.T) {
	t.Parallel()

	rt := &headerRoundTripper{
		base:    &roundTripRecorder{},
		headers: http.Header{"X-Test": []string{"", "ok"}},
	}
	if _, err := rt.RoundTrip(nil); err == nil {
		t.Fatalf("expected nil request error")
	}

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	rec, ok := rt.base.(*roundTripRecorder)
	if !ok {
		t.Fatalf("expected roundTripRecorder, got %T", rt.base)
	}
	if rec.req == nil {
		t.Fatalf("expected recorded request")
	}
	if got := rec.req.Header.Values("X-Test"); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("expected blank header value to be skipped, got %v", got)
	}
}

func TestMergeEnvEmptyExtraAndDuplicateBaseKeys(t *testing.T) {
	t.Parallel()

	base := []string{"A=1", "A=2", "B=1"}
	if got := mergeEnv(base, nil); strings.Join(got, ",") != strings.Join(base, ",") {
		t.Fatalf("expected base returned for nil extra, got %v", got)
	}

	env := mergeEnv([]string{"A=1", "A=2", "BAD", "B=1"}, map[string]string{"C": "3"})
	joined := strings.Join(env, ";")
	if strings.Contains(joined, "BAD") {
		t.Fatalf("expected invalid entry to be skipped, got %v", env)
	}
	if strings.Count(joined, "A=") != 1 {
		t.Fatalf("expected duplicate base keys to be deduped, got %v", env)
	}
}

func TestConvertMCPSchemaMarshalError(t *testing.T) {
	t.Parallel()

	_, err := convertMCPSchema(map[string]any{"bad": func() {}})
	if err == nil {
		t.Fatalf("expected marshal error")
	}
}

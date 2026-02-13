package api

import (
	"context"
	"errors"
	"testing"
	"time"
	_ "unsafe"

	"github.com/cexll/agentsdk-go/pkg/mcp"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

//go:linkname patchedNewMCPClient github.com/cexll/agentsdk-go/pkg/tool.newMCPClient
var patchedNewMCPClient func(ctx context.Context, spec string, handler func(context.Context, *mcp.ClientSession)) (*mcp.ClientSession, error)

//go:linkname patchedNewMCPClientWithOptions github.com/cexll/agentsdk-go/pkg/tool.newMCPClientWithOptions
var patchedNewMCPClientWithOptions func(ctx context.Context, spec string, opts tool.MCPServerOptions, handler func(context.Context, *mcp.ClientSession)) (*mcp.ClientSession, error)

type mcpCallCounter struct {
	calls int
	err   error
}

func (c *mcpCallCounter) dial(ctx context.Context, spec string, _ func(context.Context, *mcp.ClientSession)) (*mcp.ClientSession, error) {
	c.calls++
	return nil, c.err
}

type mcpCallCounterWithOptions struct {
	calls   int
	err     error
	lastOps tool.MCPServerOptions
}

func (c *mcpCallCounterWithOptions) dial(ctx context.Context, spec string, opts tool.MCPServerOptions, _ func(context.Context, *mcp.ClientSession)) (*mcp.ClientSession, error) {
	c.calls++
	c.lastOps = opts
	return nil, c.err
}

func TestRegisterMCPServersNotBlockedByBuiltinWhitelist(t *testing.T) {
	orig := patchedNewMCPClient
	counter := &mcpCallCounter{err: errors.New("dial error")}
	patchedNewMCPClient = counter.dial
	defer func() { patchedNewMCPClient = orig }()

	reg := tool.NewRegistry()
	// Builtins disabled; MCP should still attempt registration.
	if _, err := registerTools(reg, Options{ProjectRoot: t.TempDir(), EnabledBuiltinTools: []string{}}, nil, nil, nil); err != nil {
		t.Fatalf("register tools: %v", err)
	}

	err := registerMCPServers(context.Background(), reg, nil, []mcpServer{{Spec: "stdio://dummy"}})
	if err == nil || !errors.Is(err, counter.err) {
		t.Fatalf("expected dial error propagated, got %v", err)
	}
	if counter.calls != 1 {
		t.Fatalf("expected MCP dial invoked once, got %d", counter.calls)
	}
}

func TestRegisterMCPServersWithToolOptionsUsesOptionsDialer(t *testing.T) {
	origBase := patchedNewMCPClient
	origOpts := patchedNewMCPClientWithOptions
	baseCounter := &mcpCallCounter{err: errors.New("base dial should not be called")}
	optCounter := &mcpCallCounterWithOptions{err: errors.New("dial error")}
	patchedNewMCPClient = baseCounter.dial
	patchedNewMCPClientWithOptions = optCounter.dial
	defer func() {
		patchedNewMCPClient = origBase
		patchedNewMCPClientWithOptions = origOpts
	}()

	reg := tool.NewRegistry()
	err := registerMCPServers(context.Background(), reg, nil, []mcpServer{{
		Name:               "svc",
		Spec:               "stdio://dummy",
		EnabledTools:       []string{"echo"},
		DisabledTools:      []string{"sum"},
		ToolTimeoutSeconds: 3,
	}})
	if err == nil || !errors.Is(err, optCounter.err) {
		t.Fatalf("expected options dial error propagated, got %v", err)
	}
	if baseCounter.calls != 0 {
		t.Fatalf("expected default dialer unused, got %d", baseCounter.calls)
	}
	if optCounter.calls != 1 {
		t.Fatalf("expected options dialer called once, got %d", optCounter.calls)
	}
	if optCounter.lastOps.ToolTimeout != 3*time.Second {
		t.Fatalf("expected tool timeout propagated, got %v", optCounter.lastOps.ToolTimeout)
	}
	if len(optCounter.lastOps.EnabledTools) != 1 || optCounter.lastOps.EnabledTools[0] != "echo" {
		t.Fatalf("expected enabled tools propagated, got %+v", optCounter.lastOps.EnabledTools)
	}
	if len(optCounter.lastOps.DisabledTools) != 1 || optCounter.lastOps.DisabledTools[0] != "sum" {
		t.Fatalf("expected disabled tools propagated, got %+v", optCounter.lastOps.DisabledTools)
	}
}

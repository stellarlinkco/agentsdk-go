package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/mcp"
)

func TestRegistryRefreshMCPToolsToolNameMismatchErrors(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	restore := withStubMCPClient(t, sessionFactory(server))
	defer restore()

	r := NewRegistry()
	if err := r.RegisterMCPServer(context.Background(), "fake", "svc"); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(r.Close)

	if len(r.mcpSessions) != 1 || r.mcpSessions[0] == nil {
		t.Fatalf("expected tracked session")
	}
	info := r.mcpSessions[0]
	info.toolNames = map[string]struct{}{"svc__other": {}}

	err := r.refreshMCPTools(context.Background(), "fake", info.sessionID)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected already registered error, got %v", err)
	}
}

func TestRegistryRefreshMCPToolsPopulatesMissingInfoFields(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	restore := withStubMCPClient(t, sessionFactory(server))
	defer restore()

	r := NewRegistry()
	if err := r.RegisterMCPServer(context.Background(), "fake", "svc"); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(r.Close)

	info := r.mcpSessions[0]

	info.sessionID = ""
	if err := r.refreshMCPTools(context.Background(), "fake", ""); err != nil {
		t.Fatalf("refresh (fill sessionID): %v", err)
	}
	// The in-memory stub transport reports an empty session ID; this primarily
	// asserts the "missing sessionID" branch doesn't break refresh flows.
	if strings.TrimSpace(info.sessionID) != "" {
		t.Fatalf("expected stub sessionID to remain empty, got %q", info.sessionID)
	}

	info.sessionID = "sess"
	info.serverID = ""
	if err := r.refreshMCPTools(context.Background(), "srv-new", info.sessionID); err != nil {
		t.Fatalf("refresh (fill serverID): %v", err)
	}
	if info.serverID != "srv-new" {
		t.Fatalf("expected serverID to be filled, got %q", info.serverID)
	}

	info.serverName = ""
	if err := r.refreshMCPTools(context.Background(), info.serverID, info.sessionID); err != nil {
		t.Fatalf("refresh (empty serverName): %v", err)
	}
	if _, err := r.Get("echo"); err != nil {
		t.Fatalf("expected refreshed unnamespaced tool: %v", err)
	}
}

func TestRegistryRefreshMCPTools_ListErrNoToolsAndWrapperError(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	restore := withStubMCPClient(t, sessionFactory(server))
	defer restore()

	r := NewRegistry()
	if err := r.RegisterMCPServer(context.Background(), "fake", "svc"); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(r.Close)

	info := r.mcpSessions[0]
	r.mcpSessions = append([]*mcpSessionInfo{nil}, r.mcpSessions...)
	if got := r.findMCPSessionLocked("fake", info.sessionID); got == nil {
		t.Fatalf("expected session to be findable even with nil entries")
	}

	server.listErr = errors.New("list failed")
	if err := r.refreshMCPTools(context.Background(), "fake", info.sessionID); err == nil || !strings.Contains(err.Error(), "list MCP tools") {
		t.Fatalf("expected list error, got %v", err)
	}

	server.listErr = nil
	server.tools = nil
	if err := r.refreshMCPTools(context.Background(), "fake", info.sessionID); err == nil || !strings.Contains(err.Error(), "returned no tools") {
		t.Fatalf("expected no tools error, got %v", err)
	}

	server.tools = []*mcp.Tool{{Name: "", InputSchema: map[string]any{"type": "object"}}}
	if err := r.refreshMCPTools(context.Background(), "fake", info.sessionID); err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("expected wrapper build error, got %v", err)
	}
}

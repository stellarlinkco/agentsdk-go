package acp

import (
	"testing"

	"github.com/cexll/agentsdk-go/pkg/message"
	acpproto "github.com/coder/acp-go-sdk"
)

func TestMergeMCPServerSpecs(t *testing.T) {
	t.Parallel()

	base := []string{"http://already.example"}
	requested := []acpproto.McpServer{
		{Stdio: &acpproto.McpServerStdio{Command: "echo", Args: []string{"hi"}}},
		{Http: &acpproto.McpServerHttpInline{Url: "https://mcp.example"}},
		{Sse: &acpproto.McpServerSseInline{Url: "https://events.example"}},
		{Http: &acpproto.McpServerHttpInline{Url: "https://mcp.example"}}, // duplicate
	}

	specs, err := mergeMCPServerSpecs(base, requested)
	if err != nil {
		t.Fatalf("merge mcp servers: %v", err)
	}
	if len(specs) != 4 {
		t.Fatalf("spec count=%d, want 4 (%v)", len(specs), specs)
	}
	if specs[0] != "http://already.example" {
		t.Fatalf("spec[0]=%q, want %q", specs[0], "http://already.example")
	}
	if specs[1] != "stdio://echo hi" {
		t.Fatalf("spec[1]=%q, want %q", specs[1], "stdio://echo hi")
	}
	if specs[2] != "https://mcp.example" {
		t.Fatalf("spec[2]=%q, want %q", specs[2], "https://mcp.example")
	}
	if specs[3] != "https://events.example" {
		t.Fatalf("spec[3]=%q, want %q", specs[3], "https://events.example")
	}
}

func TestMergeMCPServerSpecsRejectsInvalidStdio(t *testing.T) {
	t.Parallel()

	_, err := mergeMCPServerSpecs(nil, []acpproto.McpServer{
		{Stdio: &acpproto.McpServerStdio{Command: "   "}},
	})
	if err == nil {
		t.Fatalf("expected invalid stdio command to fail")
	}
}

func TestHistoryMessagesToSessionUpdates(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "working",
			ToolCalls: []message.ToolCall{{
				ID:        "tool-1",
				Name:      "echo",
				Arguments: map[string]any{"text": "hi"},
			}},
		},
		{
			Role: "tool",
			ToolCalls: []message.ToolCall{{
				ID:     "tool-1",
				Name:   "echo",
				Result: "done",
			}},
		},
	}

	updates := historyMessagesToSessionUpdates(msgs)
	if len(updates) < 4 {
		t.Fatalf("expected at least 4 updates, got %d", len(updates))
	}

	var sawUser, sawAgent, sawToolStart, sawToolUpdate bool
	for _, update := range updates {
		if update.UserMessageChunk != nil {
			sawUser = true
		}
		if update.AgentMessageChunk != nil {
			sawAgent = true
		}
		if update.ToolCall != nil {
			sawToolStart = true
		}
		if update.ToolCallUpdate != nil {
			sawToolUpdate = true
		}
	}

	if !sawUser || !sawAgent || !sawToolStart || !sawToolUpdate {
		t.Fatalf(
			"unexpected replay flags user=%v agent=%v start=%v update=%v",
			sawUser,
			sawAgent,
			sawToolStart,
			sawToolUpdate,
		)
	}
}

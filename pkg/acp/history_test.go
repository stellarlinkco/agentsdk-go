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

func TestHistoryMessagesToSessionUpdatesIncludesImageContent(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{
			Role: "assistant",
			ContentBlocks: []message.ContentBlock{
				{
					Type:      message.ContentBlockImage,
					MediaType: "image/png",
					Data:      "aGVsbG8=",
				},
			},
		},
	}

	updates := historyMessagesToSessionUpdates(msgs)
	if len(updates) != 1 {
		t.Fatalf("updates len=%d, want 1", len(updates))
	}
	if updates[0].AgentMessageChunk == nil {
		t.Fatalf("expected agent message update, got %+v", updates[0])
	}
	if updates[0].AgentMessageChunk.Content.Image == nil {
		t.Fatalf("expected image content block, got %+v", updates[0].AgentMessageChunk.Content)
	}
	if updates[0].AgentMessageChunk.Content.Image.MimeType != "image/png" {
		t.Fatalf("image mimeType=%q, want %q", updates[0].AgentMessageChunk.Content.Image.MimeType, "image/png")
	}
}

func TestMessageBlockToContentBlock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		block message.ContentBlock
		valid bool
		check func(t *testing.T, content acpproto.ContentBlock)
	}{
		{
			name:  "text",
			block: message.ContentBlock{Type: message.ContentBlockText, Text: "hello"},
			valid: true,
			check: func(t *testing.T, content acpproto.ContentBlock) {
				t.Helper()
				if content.Text == nil || content.Text.Text != "hello" {
					t.Fatalf("unexpected text content: %+v", content)
				}
			},
		},
		{
			name:  "image_data",
			block: message.ContentBlock{Type: message.ContentBlockImage, Data: "aGVsbG8=", MediaType: "image/png"},
			valid: true,
			check: func(t *testing.T, content acpproto.ContentBlock) {
				t.Helper()
				if content.Image == nil || content.Image.Data != "aGVsbG8=" {
					t.Fatalf("unexpected image content: %+v", content)
				}
			},
		},
		{
			name:  "image_url",
			block: message.ContentBlock{Type: message.ContentBlockImage, URL: "file:///tmp/a.png"},
			valid: true,
			check: func(t *testing.T, content acpproto.ContentBlock) {
				t.Helper()
				if content.ResourceLink == nil || content.ResourceLink.Uri != "file:///tmp/a.png" {
					t.Fatalf("unexpected image url content: %+v", content)
				}
			},
		},
		{
			name:  "document_url",
			block: message.ContentBlock{Type: message.ContentBlockDocument, URL: "file:///tmp/a.pdf"},
			valid: true,
			check: func(t *testing.T, content acpproto.ContentBlock) {
				t.Helper()
				if content.ResourceLink == nil || content.ResourceLink.Uri != "file:///tmp/a.pdf" {
					t.Fatalf("unexpected document url content: %+v", content)
				}
			},
		},
		{
			name:  "document_text_fallback",
			block: message.ContentBlock{Type: message.ContentBlockDocument, Text: "doc text"},
			valid: true,
			check: func(t *testing.T, content acpproto.ContentBlock) {
				t.Helper()
				if content.Text == nil || content.Text.Text != "doc text" {
					t.Fatalf("unexpected document text fallback: %+v", content)
				}
			},
		},
		{
			name:  "unknown_type_text_fallback",
			block: message.ContentBlock{Type: "unknown", Text: "fallback"},
			valid: true,
			check: func(t *testing.T, content acpproto.ContentBlock) {
				t.Helper()
				if content.Text == nil || content.Text.Text != "fallback" {
					t.Fatalf("unexpected unknown-type fallback: %+v", content)
				}
			},
		},
		{
			name:  "invalid_empty",
			block: message.ContentBlock{Type: message.ContentBlockImage},
			valid: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content, ok := messageBlockToContentBlock(tc.block)
			if ok != tc.valid {
				t.Fatalf("valid=%v, want %v content=%+v", ok, tc.valid, content)
			}
			if tc.valid && tc.check != nil {
				tc.check(t, content)
			}
		})
	}
}

func TestNormalizedToolCallID(t *testing.T) {
	t.Parallel()

	counter := 0
	if got := normalizedToolCallID("tool-x", &counter); got != "tool-x" {
		t.Fatalf("explicit id=%q, want %q", got, "tool-x")
	}
	if got := normalizedToolCallID("  ", &counter); got != "tool_call_1" {
		t.Fatalf("generated id=%q, want %q", got, "tool_call_1")
	}
	if got := normalizedToolCallID("", nil); got != "tool_call" {
		t.Fatalf("nil counter id=%q, want %q", got, "tool_call")
	}
}

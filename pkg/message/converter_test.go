package message

import "testing"

func TestCloneMessageDeepCopiesToolCallArguments(t *testing.T) {
	msg := Message{Role: "assistant", Content: "call", ToolCalls: []ToolCall{{Name: "sum", Arguments: map[string]any{"a": 1}}}}
	cloned := CloneMessage(msg)

	cloned.ToolCalls[0].Arguments["a"] = 42
	if v, ok := msg.ToolCalls[0].Arguments["a"].(int); !ok || v != 1 {
		t.Fatalf("original mutated: %+v", msg.ToolCalls[0].Arguments)
	}
}

func TestCloneMessagePreservesReasoningContent(t *testing.T) {
	msg := Message{
		Role:             "assistant",
		Content:          "answer",
		ReasoningContent: "let me think step by step...",
		ToolCalls:        []ToolCall{{Name: "sum", Arguments: map[string]any{"a": 1}}},
	}
	cloned := CloneMessage(msg)

	if cloned.ReasoningContent != "let me think step by step..." {
		t.Fatalf("ReasoningContent not preserved: got %q", cloned.ReasoningContent)
	}
	// Verify independence
	cloned.ReasoningContent = "modified"
	if msg.ReasoningContent != "let me think step by step..." {
		t.Fatalf("original ReasoningContent mutated")
	}
}

func TestCloneMessageEmptyReasoningContent(t *testing.T) {
	msg := Message{Role: "assistant", Content: "hello"}
	cloned := CloneMessage(msg)
	if cloned.ReasoningContent != "" {
		t.Fatalf("expected empty ReasoningContent, got %q", cloned.ReasoningContent)
	}
}

func TestCloneMessagesPreservesReasoningContent(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "answer", ReasoningContent: "thinking..."},
	}
	cloned := CloneMessages(msgs)
	if cloned[1].ReasoningContent != "thinking..." {
		t.Fatalf("ReasoningContent not preserved in CloneMessages: got %q", cloned[1].ReasoningContent)
	}
}

func TestCloneMessagesEmpty(t *testing.T) {
	msgs := CloneMessages(nil)
	if len(msgs) != 0 {
		t.Fatalf("expected empty slice, got %d", len(msgs))
	}
}

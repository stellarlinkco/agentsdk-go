package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

func TestAnthropicCompleteStream_CallbackErrorOnDelta(t *testing.T) {
	t.Parallel()

	events := []string{
		`{"type":"message_start","message":{}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`,
	}
	msgs := &fakeMessages{stream: buildStream(t, events)}
	m := &anthropicModel{msgs: msgs, maxTokens: 16, configuredAPIKey: "key"}

	want := errors.New("cb delta")
	err := m.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}}, func(res StreamResult) error {
		if res.Delta == "" {
			return nil
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected delta callback error, got %v", err)
	}
}

func TestAnthropicCompleteStream_CallbackErrorOnToolCall(t *testing.T) {
	t.Parallel()

	events := []string{
		`{"type":"message_start","message":{}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"calc","input":{"a":1}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`,
	}
	msgs := &fakeMessages{stream: buildStream(t, events)}
	m := &anthropicModel{msgs: msgs, maxTokens: 16, configuredAPIKey: "key"}

	want := errors.New("cb tool")
	err := m.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}}, func(res StreamResult) error {
		if res.ToolCall == nil {
			return nil
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected tool callback error, got %v", err)
	}
}

func TestAnthropicDoWithRetry_CtxDoneDuringBackoff(t *testing.T) {
	t.Parallel()

	model := &anthropicModel{maxRetries: 10}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempts := 0
	err := model.doWithRetry(ctx, func(context.Context) error {
		attempts++
		if attempts == 1 {
			time.AfterFunc(5*time.Millisecond, cancel)
		}
		return errors.New("retry")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected single attempt before backoff cancel, got %d", attempts)
	}
}

func TestAnthropicConvertMessages_FallbackUserMessage(t *testing.T) {
	t.Parallel()

	systemBlocks, params := convertMessages([]Message{
		{Role: "system", Content: "sys"},
	}, false)
	if len(systemBlocks) == 0 {
		t.Fatalf("expected system blocks")
	}
	if len(params) != 1 {
		t.Fatalf("expected fallback message, got %d", len(params))
	}
	if got := params[0].Content[0].GetText(); got == nil || *got != "." {
		t.Fatalf("expected '.' fallback, got %v", got)
	}
}

func TestAnthropicAssistantContent_ReasoningBlock(t *testing.T) {
	t.Parallel()

	blocks := buildAssistantContent(Message{ReasoningContent: "why", Content: ""})
	if len(blocks) == 0 {
		t.Fatalf("expected blocks")
	}
	if blocks[0].OfThinking == nil || blocks[0].OfThinking.Thinking != "why" {
		t.Fatalf("expected thinking block first, got %#v", blocks[0])
	}
}

func TestAnthropicConvertResponseMessage_Thinking(t *testing.T) {
	t.Parallel()

	msg := anthropicsdk.Message{
		Role: constant.Assistant("assistant"),
		Content: []anthropicsdk.ContentBlockUnion{
			{Type: "thinking", Thinking: "plan"},
			{Type: "text", Text: "ok"},
		},
	}
	got := convertResponseMessage(msg)
	if strings.TrimSpace(got.ReasoningContent) != "plan" {
		t.Fatalf("expected thinking content, got %q", got.ReasoningContent)
	}
	if got.Content != "ok" {
		t.Fatalf("expected text content, got %q", got.Content)
	}
}

func TestAnthropicToolResultIsError_NoErrorKey(t *testing.T) {
	t.Parallel()

	if toolResultIsError(`{"x":1}`) {
		t.Fatalf("expected false")
	}
}

func TestAnthropicConvertContentBlocks_UnknownTypeFallsBack(t *testing.T) {
	t.Parallel()

	out := convertContentBlocks([]ContentBlock{{Type: ContentBlockType("weird")}})
	if len(out) != 1 {
		t.Fatalf("expected fallback block, got %d", len(out))
	}
	text := out[0].GetText()
	if text == nil || *text != "." {
		b, err := json.Marshal(out[0])
		if err != nil {
			t.Fatalf("marshal output: %v", err)
		}
		t.Fatalf("expected '.' fallback, got %v (%s)", text, string(b))
	}
}

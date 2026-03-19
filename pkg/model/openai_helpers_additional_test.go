package model

import (
	"testing"
)

type stubNetError struct {
	timeout   bool
	temporary bool
}

func (e stubNetError) Error() string   { return "net" }
func (e stubNetError) Timeout() bool   { return e.timeout }
func (e stubNetError) Temporary() bool { return e.temporary }

func TestIsOpenAIRetryableNetErrors(t *testing.T) {
	if !isOpenAIRetryable(stubNetError{timeout: true}) {
		t.Fatalf("timeout net errors should be retryable")
	}
	if !isOpenAIRetryable(stubNetError{temporary: true}) {
		t.Fatalf("temporary net errors should be retryable")
	}
}

func TestOpenAIImageURL(t *testing.T) {
	if got := openAIImageURL(ContentBlock{Type: ContentBlockImage, URL: " https://x "}); got != "https://x" {
		t.Fatalf("unexpected url %q", got)
	}
	if got := openAIImageURL(ContentBlock{Type: ContentBlockImage}); got != "" {
		t.Fatalf("expected empty url, got %q", got)
	}
	if got := openAIImageURL(ContentBlock{Type: ContentBlockImage, Data: "Zg=="}); got != "data:image/jpeg;base64,Zg==" {
		t.Fatalf("unexpected default mediaType data-uri %q", got)
	}
	if got := openAIImageURL(ContentBlock{Type: ContentBlockImage, Data: "Zg==", MediaType: "image/png"}); got != "data:image/png;base64,Zg==" {
		t.Fatalf("unexpected mediaType data-uri %q", got)
	}
}

func TestBuildOpenAIToolResultsEdgeCases(t *testing.T) {
	out := buildOpenAIToolResults(Message{Role: "tool", Content: "x"})
	if len(out) != 1 || out[0].OfTool == nil {
		t.Fatalf("expected single tool message, got %#v", out)
	}

	out = buildOpenAIToolResults(Message{Role: "tool", Content: "x", ToolCalls: []ToolCall{{ID: " "}}})
	if len(out) != 1 || out[0].OfTool == nil {
		t.Fatalf("expected fallback tool message for empty ids")
	}

	out = buildOpenAIToolResults(Message{Role: "tool", Content: "fallback", ToolCalls: []ToolCall{{ID: "t1"}}})
	if len(out) != 1 || out[0].OfTool == nil || out[0].OfTool.ToolCallID != "t1" {
		t.Fatalf("unexpected tool result %#v", out[0].OfTool)
	}
	content, ok := out[0].GetContent().AsAny().(*string)
	if !ok || content == nil || *content != "fallback" {
		t.Fatalf("unexpected tool result content %#v", out[0].GetContent().AsAny())
	}

	out = buildOpenAIToolResults(Message{Role: "tool", Content: "fallback", ToolCalls: []ToolCall{{ID: "t1", Result: "ok"}}})
	content, ok = out[0].GetContent().AsAny().(*string)
	if len(out) != 1 || out[0].OfTool == nil || !ok || content == nil || *content != "ok" {
		t.Fatalf("unexpected tool result content %#v", out[0].OfTool)
	}
}

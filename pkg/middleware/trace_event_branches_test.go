package middleware

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

func TestTraceEventMoreBranches(t *testing.T) {
	t.Run("captureModelRequest metadata and stage mismatch", func(t *testing.T) {
		st := &State{
			Values: map[string]any{
				"model.metadata": map[string]any{"k": "v"},
			},
			ModelInput: model.Request{
				Messages: []model.Message{{Role: "user", Content: "hi"}},
				Model:    "m",
			},
		}
		if got := captureModelRequest(StageBeforeTool, st); got != nil {
			t.Fatalf("expected nil for non-model stage, got %#v", got)
		}
		got := captureModelRequest(StageBeforeAgent, st)
		if got == nil || got["metadata"] == nil {
			t.Fatalf("expected metadata injected, got %#v", got)
		}
	})

	t.Run("captureModelResponse enriches even when model output missing", func(t *testing.T) {
		st := &State{
			Values: map[string]any{
				"model.usage":       model.Usage{TotalTokens: 1},
				"model.stop_reason": "done",
				"model.stream":      []byte("x"),
			},
		}
		if got := captureModelResponse(StageBeforeAgent, st); got != nil {
			t.Fatalf("expected nil for non-response stage, got %#v", got)
		}
		got := captureModelResponse(StageAfterAgent, st)
		if got == nil || got["usage"] == nil || got["stop_reason"] != "done" || got["stream"] != "x" {
			t.Fatalf("unexpected payload: %#v", got)
		}
	})

	t.Run("toolCallPayload and toolResultPayload bytes/pointer/nil variants", func(t *testing.T) {
		call := toolCallPayload([]byte(`{"id":"1","name":"n"}`))
		if call["id"] != "1" || call["name"] != "n" {
			t.Fatalf("unexpected tool call payload: %#v", call)
		}

		var nilAny any
		if got := toolResultPayload(nilAny); got != nil {
			t.Fatalf("expected nil tool result payload, got %#v", got)
		}
		gotBytes := toolResultPayload([]byte(`{"name":"b"}`))
		if gotBytes["name"] != "b" {
			t.Fatalf("unexpected bytes tool result payload: %#v", gotBytes)
		}
		cr := &tool.CallResult{
			Call:   tool.Call{Name: "p"},
			Result: &tool.ToolResult{Output: "ok"},
		}
		gotPtr := toolResultPayload(cr)
		if gotPtr["name"] != "p" || gotPtr["content"] == nil {
			t.Fatalf("unexpected pointer call result payload: %#v", gotPtr)
		}
	})

	t.Run("snapshotModelRequest includes all fields", func(t *testing.T) {
		temp := 0.7
		req := &model.Request{
			Messages:    []model.Message{{Role: "user", Content: "hi"}},
			Tools:       []model.ToolDefinition{{Name: "t"}},
			System:      "sys",
			Model:       "m",
			MaxTokens:   9,
			Temperature: &temp,
		}
		got := snapshotModelRequest(req)
		for _, key := range []string{"messages", "tools", "system", "model", "max_tokens", "temperature"} {
			if got[key] == nil {
				t.Fatalf("missing %s in payload: %#v", key, got)
			}
		}
	})

	t.Run("valueErrorString pointer and non-struct cases", func(t *testing.T) {
		var nilPtr *struct{ Err error }
		if got := valueErrorString(nilPtr); got != "" {
			t.Fatalf("expected empty for nil pointer, got %q", got)
		}
		ptr := &struct{ Err error }{Err: errors.New("e")}
		if got := valueErrorString(ptr); got != "e" {
			t.Fatalf("expected error for pointer struct, got %q", got)
		}
		if got := valueErrorString(struct{ X int }{X: 1}); got != "" {
			t.Fatalf("expected empty for struct without Err, got %q", got)
		}
		if got := valueErrorString(123); got != "" {
			t.Fatalf("expected empty for non-struct, got %q", got)
		}
	})

	t.Run("modelResponsePayload raw fallback for non-serializable input", func(t *testing.T) {
		got := modelResponsePayload(func() {})
		if got == nil || got["raw"] == nil {
			t.Fatalf("expected raw fallback, got %#v", got)
		}
	})

	t.Run("modelResponsePayload bytes invalid JSON falls back to raw", func(t *testing.T) {
		got := modelResponsePayload([]byte("{"))
		if got == nil || got["raw"] != "{" {
			t.Fatalf("expected raw invalid JSON fallback, got %#v", got)
		}
	})

	t.Run("toolCallPayload and toolResultPayload nil pointer guards", func(t *testing.T) {
		var nilCall *tool.Call
		if got := toolCallPayload(nilCall); got != nil {
			t.Fatalf("expected nil tool call payload, got %#v", got)
		}

		var nilToolResult *tool.ToolResult
		if got := toolResultPayload(nilToolResult); got != nil {
			t.Fatalf("expected nil tool result payload, got %#v", got)
		}
	})

	t.Run("captureTraceError and sanitizePayload extra branches", func(t *testing.T) {
		st := &State{Values: map[string]any{"error": "  oops  "}}
		if got := captureTraceError(StageBeforeTool, st, nil); got != "oops" {
			t.Fatalf("expected trimmed error string, got %q", got)
		}
		if got := sanitizePayload(complex128(1 + 2i)); got != "complex128" {
			t.Fatalf("expected type-name fallback, got %#v", got)
		}
	})

	t.Run("decodeJSONMap and cloneMap empty inputs", func(t *testing.T) {
		if got := decodeJSONMap(json.RawMessage(nil)); got != nil {
			t.Fatalf("expected nil for empty raw, got %#v", got)
		}
		if got := cloneMap(map[string]any{}); got != nil {
			t.Fatalf("expected nil for empty map, got %#v", got)
		}
	})

	t.Run("structToMap skips unexported fields from other packages", func(t *testing.T) {
		if got := structToMap(time.Unix(0, 0), map[string]string{"wall": "wall"}); got != nil {
			t.Fatalf("expected nil when fields not interfaceable, got %#v", got)
		}
	})
}

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

func TestModelRequestPayloadVariants(t *testing.T) {
	fromMap := modelRequestPayload(map[string]any{"k": "v"})
	if fromMap["k"] != "v" {
		t.Fatalf("map payload lost: %#v", fromMap)
	}

	raw := modelRequestPayload(json.RawMessage(`{"foo":2}`))
	if raw["foo"] != float64(2) {
		t.Fatalf("raw message not decoded: %#v", raw)
	}

	rawInvalid := modelRequestPayload([]byte("oops"))
	if rawInvalid["raw"] != "oops" {
		t.Fatalf("invalid bytes not preserved: %#v", rawInvalid)
	}

	type customReq struct {
		Messages    []model.Message
		Tools       []model.ToolDefinition
		System      string
		MaxTokens   int
		Model       string
		Temperature float64
	}
	payload := modelRequestPayload(customReq{
		Messages:    []model.Message{{Role: "user", Content: "hi"}},
		System:      "sys",
		Model:       "claude-3",
		MaxTokens:   99,
		Temperature: 0.5,
	})
	for _, key := range []string{"messages", "system", "model", "max_tokens", "temperature"} {
		if payload[key] == nil {
			t.Fatalf("missing %s in payload: %#v", key, payload)
		}
	}

	weird := modelRequestPayload(func() {})
	if _, ok := weird["raw"]; !ok {
		t.Fatalf("expected raw payload for non serializable: %#v", weird)
	}
}

func TestToolCallPayloadVariants(t *testing.T) {
	call := tool.Call{Name: "run", Params: map[string]any{"x": 1}, Path: "/tmp"}
	payload := toolCallPayload(call)
	if payload["name"] != "run" || payload["input"] == nil || payload["path"] != "/tmp" {
		t.Fatalf("struct payload mismatch: %#v", payload)
	}

	if res := toolCallPayload((*tool.Call)(nil)); res != nil {
		t.Fatalf("nil pointer should return nil, got %#v", res)
	}

	fromMap := toolCallPayload(map[string]any{"id": "123"})
	if fromMap["id"] != "123" {
		t.Fatalf("map payload lost: %#v", fromMap)
	}

	raw := toolCallPayload(json.RawMessage(`{"name":"raw"}`))
	if raw["name"] != "raw" {
		t.Fatalf("raw payload mismatch: %#v", raw)
	}

	type callStruct struct {
		ID    string
		Name  string
		Input any
	}
	custom := toolCallPayload(callStruct{ID: "abc", Name: "custom", Input: "param"})
	if custom["id"] != "abc" || custom["name"] != "custom" || custom["input"] != "param" {
		t.Fatalf("struct mapping failed: %#v", custom)
	}
}

func TestToolResultPayloadVariants(t *testing.T) {
	res := tool.CallResult{
		Call:   tool.Call{Name: "demo"},
		Result: &tool.ToolResult{Success: false, Error: fmt.Errorf("fail")},
		Err:    errors.New("wrapped"),
	}
	payload := toolResultPayload(res)
	if payload["name"] != "demo" || payload["is_error"] != true {
		t.Fatalf("call result error not captured: %#v", payload)
	}

	ptr := &tool.ToolResult{Success: true, Output: "ok"}
	ptrPayload := toolResultPayload(ptr)
	if ptrPayload["content"] == nil || ptrPayload["error"] != nil {
		t.Fatalf("tool result content missing: %#v", ptrPayload)
	}

	mapPayload := toolResultPayload(map[string]any{"name": "map"})
	if mapPayload["name"] != "map" {
		t.Fatalf("map payload mismatch: %#v", mapPayload)
	}

	rawPayload := toolResultPayload(json.RawMessage(`{"name":"raw"}`))
	if rawPayload["name"] != "raw" {
		t.Fatalf("raw payload mismatch: %#v", rawPayload)
	}

	type customRes struct {
		Name     string
		Output   string
		Metadata map[string]any
	}
	structPayload := toolResultPayload(customRes{Name: "struct", Output: "out", Metadata: map[string]any{"a": 1}})
	if structPayload["name"] != "struct" || structPayload["content"] != "out" {
		t.Fatalf("struct mapping failed: %#v", structPayload)
	}
}

func TestCaptureTraceErrorPriority(t *testing.T) {
	st := &State{Values: map[string]any{}}

	if msg := captureTraceError(StageAfterTool, st, map[string]any{"error": "tool"}); msg != "tool" {
		t.Fatalf("tool error not preferred: %s", msg)
	}
	if msg := captureTraceError(StageAfterTool, st, map[string]any{"is_error": true}); msg != "tool execution failed" {
		t.Fatalf("is_error flag not handled: %s", msg)
	}

	st.Values["trace.error"] = errors.New("trace-level")
	if msg := captureTraceError(StageAfterAgent, st, nil); msg != "trace-level" {
		t.Fatalf("trace error not surfaced: %s", msg)
	}

	st.Values = map[string]any{}
	st.ModelOutput = struct{ Err error }{Err: errors.New("model failed")}
	if msg := captureTraceError(StageAfterAgent, st, nil); msg != "model failed" {
		t.Fatalf("model output error missed: %s", msg)
	}

	st.ModelOutput = nil
	st.ToolResult = struct{ Err error }{Err: errors.New("tool failed")}
	if msg := captureTraceError(StageAfterTool, st, nil); msg != "tool failed" {
		t.Fatalf("tool result error missed: %s", msg)
	}
}

func TestUsageFromValuesAndToInt(t *testing.T) {
	usage := model.Usage{TotalTokens: 10}
	ptrUsage := &model.Usage{TotalTokens: 20}

	if got := usageFromValues(map[string]any{"model.usage": usage}); got["total_tokens"] != 10 {
		t.Fatalf("struct usage not captured: %#v", got)
	}
	if got := usageFromValues(map[string]any{"model.usage": ptrUsage}); got["total_tokens"] != 20 {
		t.Fatalf("pointer usage not captured: %#v", got)
	}
	if got := usageFromValues(map[string]any{"model.usage": map[string]any{"total_tokens": 5}}); got["total_tokens"] != 5 {
		t.Fatalf("map usage not captured: %#v", got)
	}
	if usageFromValues(nil) != nil {
		t.Fatalf("nil values should yield nil usage")
	}

	if toInt(json.Number("12")) != 12 {
		t.Fatalf("json number parse failed")
	}
	if toInt(json.Number("bad")) != 0 {
		t.Fatalf("invalid json number should return zero")
	}
	if toInt(" 15 ") != 15 {
		t.Fatalf("string parse failed")
	}
	if toInt(float32(3.9)) != 3 {
		t.Fatalf("float cast failed")
	}
	if toInt(float64(6.7)) != 6 {
		t.Fatalf("float64 cast failed")
	}
	if toInt(int64(21)) != 21 || toInt(int32(7)) != 7 || toInt(11) != 11 {
		t.Fatalf("integer conversions failed")
	}
	if toInt("") != 0 {
		t.Fatalf("blank string should yield zero")
	}
}

func TestModelResponsePayloadVariants(t *testing.T) {
	resp := model.Response{
		Message:    model.Message{Content: "ok", ToolCalls: []model.ToolCall{{Name: "t"}}},
		Usage:      model.Usage{TotalTokens: 3},
		StopReason: "stop",
	}
	payload := modelResponsePayload(resp)
	if payload["content"] != "ok" || payload["stop_reason"] != "stop" {
		t.Fatalf("response payload missing fields: %#v", payload)
	}
	if usage, ok := payload["usage"].(map[string]any); !ok || usage["total_tokens"] != 3 {
		t.Fatalf("usage missing: %#v", payload)
	}

	raw := modelResponsePayload([]byte("bad-json"))
	if raw["raw"] != "bad-json" {
		t.Fatalf("raw bytes not preserved: %#v", raw)
	}

	type custom struct {
		Content   string
		ToolCalls []model.ToolCall
		Done      bool
	}
	customPayload := modelResponsePayload(custom{Content: "c", ToolCalls: []model.ToolCall{{Name: "x"}}, Done: true})
	if customPayload["content"] != "c" || customPayload["tool_calls"] == nil {
		t.Fatalf("struct mapping failed: %#v", customPayload)
	}
}

func TestMetadataAndUsageTotal(t *testing.T) {
	meta := metadataFromValues(map[string]any{"model.metadata": map[string]any{"a": 1}})
	if meta["a"] != 1 {
		t.Fatalf("metadata clone failed: %#v", meta)
	}
	if metadataFromValues(map[string]any{"trace.metadata": map[string]any{}}) != nil {
		t.Fatalf("empty metadata should be nil")
	}

	if usageTotal(map[string]any{"usage": model.Usage{TotalTokens: 7}}) != 7 {
		t.Fatalf("usage total from struct failed")
	}
	if usageTotal(map[string]any{"usage": &model.Usage{TotalTokens: 8}}) != 8 {
		t.Fatalf("usage total from pointer failed")
	}
	if usageTotal(map[string]any{"usage": map[string]any{"total_tokens": json.Number("9")}}) != 9 {
		t.Fatalf("usage total from map failed")
	}
	if usageTotal(map[string]any{"usage": "11"}) != 11 {
		t.Fatalf("usage total from string failed")
	}
}

func TestContextStringAndSanitizeSession(t *testing.T) {
	if got := contextString(context.TODO(), "key"); got != "" {
		t.Fatalf("missing key should return empty")
	}
	ctx := context.WithValue(context.Background(), TraceSessionIDContextKey, "ctx-id")
	if got := contextString(ctx, TraceSessionIDContextKey); got != "ctx-id" {
		t.Fatalf("context value mismatch: %s", got)
	}
	if got := contextString(ctx, nil); got != "" {
		t.Fatalf("nil key should be empty")
	}

	if sanitizeSessionComponent("   ") != "session" {
		t.Fatalf("blank session should fallback")
	}
	if sanitizeSessionComponent("***X***") != "x" && sanitizeSessionComponent("***X***") != "X" {
		// sanitized should drop asterisks but keep letter
		t.Fatalf("sanitize failed")
	}
}

package middleware

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

// TraceEvent captures a single middleware hook invocation and its payloads.
type TraceEvent struct {
	Timestamp     time.Time      `json:"timestamp"`
	Stage         string         `json:"stage"`
	Iteration     int            `json:"iteration"`
	SessionID     string         `json:"session_id"`
	Input         any            `json:"input,omitempty"`
	Output        any            `json:"output,omitempty"`
	ModelRequest  map[string]any `json:"model_request,omitempty"`
	ModelResponse map[string]any `json:"model_response,omitempty"`
	ToolCall      map[string]any `json:"tool_call,omitempty"`
	ToolResult    map[string]any `json:"tool_result,omitempty"`
	Error         string         `json:"error,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
}

func captureModelRequest(stage Stage, st *State) map[string]any {
	if st == nil || stage != StageBeforeAgent {
		return nil
	}
	payload := modelRequestPayload(st.ModelInput)
	if len(payload) == 0 {
		return nil
	}
	if meta := metadataFromValues(st.Values); len(meta) > 0 {
		payload["metadata"] = meta
	}
	return payload
}

func captureModelResponse(stage Stage, st *State) map[string]any {
	if st == nil || stage != StageAfterAgent {
		return nil
	}
	payload := modelResponsePayload(st.ModelOutput)
	if len(payload) == 0 {
		payload = map[string]any{}
	}
	if usage := usageFromValues(st.Values); len(usage) > 0 {
		payload["usage"] = usage
	}
	if reason := firstString(st.Values, "model.stop_reason", "stop_reason"); reason != "" {
		payload["stop_reason"] = reason
	}
	if stream := st.Values["model.stream"]; stream != nil {
		payload["stream"] = sanitizePayload(stream)
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func captureToolCall(stage Stage, st *State) map[string]any {
	if st == nil || (stage != StageBeforeTool && stage != StageAfterTool) {
		return nil
	}
	return toolCallPayload(st.ToolCall)
}

func captureToolResult(stage Stage, st *State, call map[string]any) map[string]any {
	if st == nil || stage != StageAfterTool {
		return nil
	}
	payload := toolResultPayload(st.ToolResult)
	if payload == nil {
		return nil
	}
	if call != nil {
		if id, ok := call["id"].(string); ok && id != "" {
			payload["id"] = id
		}
		if name, ok := call["name"].(string); ok && name != "" && payload["name"] == nil {
			payload["name"] = name
		}
	}
	return payload
}

func captureTraceError(stage Stage, st *State, toolRes map[string]any) string {
	if toolRes != nil {
		if errStr, ok := toolRes["error"].(string); ok && strings.TrimSpace(errStr) != "" {
			return strings.TrimSpace(errStr)
		}
		if isErr, ok := toolRes["is_error"].(bool); ok && isErr {
			return "tool execution failed"
		}
	}
	for _, key := range []string{"trace.error", "error", "last_error"} {
		if val, ok := st.Values[key]; ok {
			if err, ok := val.(error); ok && err != nil {
				return err.Error()
			}
			if text, ok := val.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	switch stage {
	case StageAfterAgent:
		if msg := valueErrorString(st.ModelOutput); msg != "" {
			return msg
		}
	case StageAfterTool:
		if msg := valueErrorString(st.ToolResult); msg != "" {
			return msg
		}
	}
	return ""
}

func modelRequestPayload(src any) map[string]any {
	switch req := src.(type) {
	case model.Request:
		return snapshotModelRequest(&req)
	case *model.Request:
		return snapshotModelRequest(req)
	case map[string]any:
		return cloneMap(req)
	case json.RawMessage:
		return decodeJSONMap(req)
	case []byte:
		dup := append([]byte(nil), req...)
		return decodeJSONMap(json.RawMessage(dup))
	default:
		if payload := structToMap(req, map[string]string{
			"Messages":    "messages",
			"Tools":       "tools",
			"System":      "system",
			"MaxTokens":   "max_tokens",
			"Model":       "model",
			"Temperature": "temperature",
		}); len(payload) > 0 {
			return payload
		}
	}
	if val := sanitizePayload(src); val != nil {
		return map[string]any{"raw": val}
	}
	return nil
}

func modelResponsePayload(src any) map[string]any {
	switch resp := src.(type) {
	case model.Response:
		return snapshotModelResponse(&resp)
	case *model.Response:
		return snapshotModelResponse(resp)
	case map[string]any:
		return cloneMap(resp)
	case json.RawMessage:
		return decodeJSONMap(resp)
	case []byte:
		return decodeJSONMap(json.RawMessage(resp))
	default:
		if payload := structToMap(resp, map[string]string{
			"Content":   "content",
			"ToolCalls": "tool_calls",
			"Done":      "done",
		}); len(payload) > 0 {
			return payload
		}
	}
	if val := sanitizePayload(src); val != nil {
		return map[string]any{"raw": val}
	}
	return nil
}

func toolCallPayload(src any) map[string]any {
	switch call := src.(type) {
	case tool.Call:
		return map[string]any{
			"name":  strings.TrimSpace(call.Name),
			"input": sanitizePayload(call.Params),
			"path":  strings.TrimSpace(call.Path),
		}
	case *tool.Call:
		if call == nil {
			return nil
		}
		return map[string]any{
			"name":  strings.TrimSpace(call.Name),
			"input": sanitizePayload(call.Params),
			"path":  strings.TrimSpace(call.Path),
		}
	case map[string]any:
		return cloneMap(call)
	case json.RawMessage:
		return decodeJSONMap(call)
	case []byte:
		dup := append([]byte(nil), call...)
		return decodeJSONMap(json.RawMessage(dup))
	default:
		payload := structToMap(call, map[string]string{
			"ID":    "id",
			"Name":  "name",
			"Input": "input",
		})
		if len(payload) > 0 {
			return payload
		}
	}
	if val := sanitizePayload(src); val != nil {
		return map[string]any{"raw": val}
	}
	return nil
}

func toolResultPayload(src any) map[string]any {
	switch res := src.(type) {
	case tool.CallResult:
		return snapshotToolCallResult(&res)
	case *tool.CallResult:
		return snapshotToolCallResult(res)
	case *tool.ToolResult:
		if res == nil {
			return nil
		}
		payload := map[string]any{
			"content": sanitizePayload(res),
		}
		if res.Error != nil {
			payload["error"] = res.Error.Error()
			payload["is_error"] = true
		}
		return payload
	case map[string]any:
		return cloneMap(res)
	case json.RawMessage:
		return decodeJSONMap(res)
	case []byte:
		dup := append([]byte(nil), res...)
		return decodeJSONMap(json.RawMessage(dup))
	default:
		payload := structToMap(res, map[string]string{
			"Name":     "name",
			"Output":   "content",
			"Metadata": "metadata",
		})
		if payload == nil {
			if val := sanitizePayload(src); val != nil {
				return map[string]any{"raw": val}
			}
			return nil
		}
		return payload
	}
}

func snapshotModelRequest(req *model.Request) map[string]any {
	if req == nil {
		return nil
	}
	payload := map[string]any{}
	if len(req.Messages) > 0 {
		payload["messages"] = sanitizePayload(req.Messages)
	}
	if len(req.Tools) > 0 {
		payload["tools"] = sanitizePayload(req.Tools)
	}
	if strings.TrimSpace(req.System) != "" {
		payload["system"] = req.System
	}
	if req.MaxTokens != 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Model != "" {
		payload["model"] = req.Model
	}
	if req.Temperature != nil {
		payload["temperature"] = req.Temperature
	}
	return payload
}

func snapshotModelResponse(resp *model.Response) map[string]any {
	if resp == nil {
		return nil
	}
	payload := map[string]any{
		"content": resp.Message.Content,
	}
	if len(resp.Message.ToolCalls) > 0 {
		payload["tool_calls"] = resp.Message.ToolCalls
	}
	payload["usage"] = usageToMap(resp.Usage)
	if strings.TrimSpace(resp.StopReason) != "" {
		payload["stop_reason"] = resp.StopReason
	}
	return payload
}

func snapshotToolCallResult(res *tool.CallResult) map[string]any {
	if res == nil {
		return nil
	}
	payload := map[string]any{
		"name":    strings.TrimSpace(res.Call.Name),
		"content": sanitizePayload(res.Result),
	}
	if res.Err != nil {
		payload["error"] = res.Err.Error()
		payload["is_error"] = true
	}
	if res.Result != nil && res.Result.Error != nil {
		payload["error"] = res.Result.Error.Error()
		payload["is_error"] = true
	}
	return payload
}

func metadataFromValues(values map[string]any) map[string]any {
	for _, key := range []string{"model.metadata", "model_metadata", "trace.metadata"} {
		if raw, ok := values[key]; ok {
			if meta, ok := raw.(map[string]any); ok && len(meta) > 0 {
				return cloneMap(meta)
			}
		}
	}
	return nil
}

func usageFromValues(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	switch raw := values["model.usage"].(type) {
	case model.Usage:
		return usageToMap(raw)
	case *model.Usage:
		if raw != nil {
			return usageToMap(*raw)
		}
	case map[string]any:
		return cloneMap(raw)
	}
	return nil
}

func usageToMap(u model.Usage) map[string]any {
	return map[string]any{
		"input_tokens":          u.InputTokens,
		"output_tokens":         u.OutputTokens,
		"total_tokens":          u.TotalTokens,
		"cache_read_tokens":     u.CacheReadTokens,
		"cache_creation_tokens": u.CacheCreationTokens,
	}
}

func decodeJSONMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return map[string]any{"raw": string(raw)}
	}
	return cloneMap(data)
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	clone := make(map[string]any, len(src))
	for k, v := range src {
		clone[k] = sanitizePayload(v)
	}
	return clone
}

func structToMap(src any, mapping map[string]string) map[string]any {
	val := reflect.ValueOf(src)
	if !val.IsValid() {
		return nil
	}
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}
	out := map[string]any{}
	for field, key := range mapping {
		f := val.FieldByName(field)
		if !f.IsValid() || !f.CanInterface() {
			continue
		}
		out[key] = sanitizePayload(f.Interface())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func valueErrorString(v any) string {
	if v == nil {
		return ""
	}
	if err, ok := v.(error); ok && err != nil {
		return err.Error()
	}
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return ""
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return ""
	}
	field := val.FieldByName("Err")
	if field.IsValid() && field.CanInterface() {
		if err, ok := field.Interface().(error); ok && err != nil {
			return err.Error()
		}
	}
	return ""
}

func sanitizePayload(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case error:
		return val.Error()
	case json.RawMessage:
		dup := make([]byte, len(val))
		copy(dup, val)
		return json.RawMessage(dup)
	case []byte:
		copyBytes := append([]byte(nil), val...)
		if json.Valid(copyBytes) {
			return json.RawMessage(copyBytes)
		}
		return string(copyBytes)
	}
	if _, err := json.Marshal(v); err == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Func || rv.Kind() == reflect.Chan {
		return fmt.Sprintf("<non-serializable:%T>", v)
	}
	return fmt.Sprintf("%T", v)
}

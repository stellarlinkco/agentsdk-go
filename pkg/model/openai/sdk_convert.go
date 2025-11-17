package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	openaisdk "github.com/openai/openai-go/v3"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

// Helper functions

func normalizeRole(role string) string {
	return strings.ToLower(role)
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	clone := make(map[string]any, len(m))
	for k, v := range m {
		clone[k] = v
	}
	return clone
}

func encodeArguments(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func decodeArguments(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("decode arguments: %w", err)
	}
	return args, nil
}

func convertMessagesToOpenAI(messages []modelpkg.Message) ([]openaisdk.ChatCompletionMessageParamUnion, error) {
	if len(messages) == 0 {
		return []openaisdk.ChatCompletionMessageParamUnion{buildUserMessage("")}, nil
	}

	params := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(messages))
	for idx, msg := range messages {
		role := normalizeRole(msg.Role)
		switch role {
		case "system":
			params = append(params, buildSystemMessage(msg.Content))
		case "user":
			params = append(params, buildUserMessage(msg.Content))
		case "assistant":
			union, err := buildAssistantMessage(msg)
			if err != nil {
				return nil, fmt.Errorf("messages[%d]: %w", idx, err)
			}
			params = append(params, union)
		case "tool":
			union, err := buildToolMessage(msg)
			if err != nil {
				return nil, fmt.Errorf("messages[%d]: %w", idx, err)
			}
			params = append(params, union)
		default:
			params = append(params, buildUserMessage(msg.Content))
		}
	}

	if len(params) == 0 {
		return []openaisdk.ChatCompletionMessageParamUnion{buildUserMessage("")}, nil
	}
	return params, nil
}

func buildSystemMessage(content string) openaisdk.ChatCompletionMessageParamUnion {
	msg := openaisdk.ChatCompletionSystemMessageParam{}
	msg.Content.OfString = openaisdk.String(content)
	return openaisdk.ChatCompletionMessageParamUnion{OfSystem: &msg}
}

func buildUserMessage(content string) openaisdk.ChatCompletionMessageParamUnion {
	msg := openaisdk.ChatCompletionUserMessageParam{}
	msg.Content.OfString = openaisdk.String(content)
	return openaisdk.ChatCompletionMessageParamUnion{OfUser: &msg}
}

func buildAssistantMessage(msg modelpkg.Message) (openaisdk.ChatCompletionMessageParamUnion, error) {
	asst := openaisdk.ChatCompletionAssistantMessageParam{}
	if msg.Content != "" || len(msg.ToolCalls) == 0 {
		asst.Content.OfString = openaisdk.String(msg.Content)
	}
	if len(msg.ToolCalls) > 0 {
		calls, err := convertToolCallsToOpenAI(msg.ToolCalls)
		if err != nil {
			return openaisdk.ChatCompletionMessageParamUnion{}, err
		}
		asst.ToolCalls = calls
	}
	return openaisdk.ChatCompletionMessageParamUnion{OfAssistant: &asst}, nil
}

func buildToolMessage(msg modelpkg.Message) (openaisdk.ChatCompletionMessageParamUnion, error) {
	id := firstNonEmptyToolCallID(msg.ToolCalls)
	if id == "" {
		return openaisdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool message missing tool_call_id")
	}
	return openaisdk.ToolMessage(msg.Content, id), nil
}

func convertToolCallsToOpenAI(calls []modelpkg.ToolCall) ([]openaisdk.ChatCompletionMessageToolCallUnionParam, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]openaisdk.ChatCompletionMessageToolCallUnionParam, 0, len(calls))
	for idx, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			return nil, fmt.Errorf("tool_calls[%d]: missing name", idx)
		}
		args := encodeArguments(call.Arguments)
		out = append(out, openaisdk.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openaisdk.ChatCompletionMessageFunctionToolCallParam{
				ID: call.ID,
				Function: openaisdk.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      name,
					Arguments: args,
				},
			},
		})
	}
	return out, nil
}

func convertToolsToOpenAI(tools []map[string]any) ([]openaisdk.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openaisdk.ChatCompletionToolUnionParam, 0, len(tools))
	for idx, tool := range tools {
		typ := strings.ToLower(strings.TrimSpace(toString(tool["type"])))
		if typ != "" && typ != "function" {
			return nil, fmt.Errorf("tools[%d]: unsupported type %q", idx, typ)
		}
		fn, err := toMap(tool["function"])
		if err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", idx, err)
		}
		if len(fn) == 0 {
			return nil, fmt.Errorf("tools[%d]: missing function definition", idx)
		}
		name := strings.TrimSpace(toString(fn["name"]))
		if name == "" {
			return nil, fmt.Errorf("tools[%d]: missing function name", idx)
		}
		def := openaisdk.FunctionDefinitionParam{
			Name: name,
		}
		if desc := strings.TrimSpace(toString(fn["description"])); desc != "" {
			def.Description = openaisdk.String(desc)
		}
		if strict, ok := fn["strict"].(bool); ok {
			def.Strict = openaisdk.Bool(strict)
		}
		if paramsVal, ok := fn["parameters"]; ok && paramsVal != nil {
			paramsMap, err := toMap(paramsVal)
			if err != nil {
				return nil, fmt.Errorf("tools[%d]: parameters: %w", idx, err)
			}
			if len(paramsMap) > 0 {
				def.Parameters = openaisdk.FunctionParameters(paramsMap)
			}
		}
		out = append(out, openaisdk.ChatCompletionToolUnionParam{
			OfFunction: &openaisdk.ChatCompletionFunctionToolParam{
				Function: def,
			},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid tools provided")
	}
	return out, nil
}

func convertMessageFromOpenAI(msg openaisdk.ChatCompletionMessage) (modelpkg.Message, error) {
	role := strings.TrimSpace(string(msg.Role))
	if role == "" {
		role = "assistant"
	}
	content := msg.Content
	if content == "" && strings.TrimSpace(msg.Refusal) != "" {
		content = msg.Refusal
	}
	result := modelpkg.Message{
		Role:    role,
		Content: content,
	}

	if len(msg.ToolCalls) > 0 {
		calls := make([]modelpkg.ToolCall, 0, len(msg.ToolCalls))
		for idx, call := range msg.ToolCalls {
			tc, err := convertSDKToolCall(call)
			if err != nil {
				return modelpkg.Message{}, fmt.Errorf("tool_calls[%d]: %w", idx, err)
			}
			calls = append(calls, tc)
		}
		result.ToolCalls = calls
	} else if name := strings.TrimSpace(msg.FunctionCall.Name); name != "" {
		args, err := decodeArguments(msg.FunctionCall.Arguments)
		if err != nil {
			return modelpkg.Message{}, fmt.Errorf("function_call: %w", err)
		}
		result.ToolCalls = []modelpkg.ToolCall{{
			Name:      name,
			Arguments: args,
		}}
	}
	return result, nil
}

func convertSDKToolCall(call openaisdk.ChatCompletionMessageToolCallUnion) (modelpkg.ToolCall, error) {
	typ := strings.TrimSpace(call.Type)
	if typ == "" {
		typ = "function"
	}
	switch typ {
	case "function":
		fn := call.AsFunction()
		if strings.TrimSpace(fn.Function.Name) == "" {
			return modelpkg.ToolCall{}, fmt.Errorf("missing function name")
		}
		args, err := decodeArguments(fn.Function.Arguments)
		if err != nil {
			return modelpkg.ToolCall{}, fmt.Errorf("decode function arguments: %w", err)
		}
		return modelpkg.ToolCall{
			ID:        fn.ID,
			Name:      fn.Function.Name,
			Arguments: args,
		}, nil
	case "custom":
		custom := call.AsCustom()
		name := strings.TrimSpace(custom.Custom.Name)
		if name == "" {
			return modelpkg.ToolCall{}, fmt.Errorf("missing custom tool name")
		}
		args := map[string]any{}
		if trimmed := strings.TrimSpace(custom.Custom.Input); trimmed != "" {
			if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
				args["input"] = custom.Custom.Input
			}
		}
		return modelpkg.ToolCall{
			ID:        custom.ID,
			Name:      name,
			Arguments: args,
		}, nil
	default:
		return modelpkg.ToolCall{}, fmt.Errorf("unsupported tool_call type %q", typ)
	}
}

func firstNonEmptyToolCallID(calls []modelpkg.ToolCall) string {
	for _, call := range calls {
		if id := strings.TrimSpace(call.ID); id != "" {
			return id
		}
	}
	return ""
}

func toMap(val any) (map[string]any, error) {
	if val == nil {
		return nil, nil
	}
	switch typed := val.(type) {
	case map[string]any:
		return cloneMap(typed), nil
	case json.RawMessage:
		return coerceJSONMap([]byte(typed))
	case []byte:
		return coerceJSONMap(typed)
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return coerceJSONMap([]byte(typed))
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("marshal map value: %w", err)
		}
		return coerceJSONMap(data)
	}
}

func coerceJSONMap(data []byte) (map[string]any, error) {
	data = bytesTrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal map: %w", err)
	}
	return out, nil
}

func toString(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func bytesTrimSpace(data []byte) []byte {
	start := 0
	for start < len(data) && isSpace(data[start]) {
		start++
	}
	end := len(data)
	for end > start && isSpace(data[end-1]) {
		end--
	}
	return data[start:end]
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\n', '\t', '\r':
		return true
	default:
		return false
	}
}

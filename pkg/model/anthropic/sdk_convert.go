package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

func convertMessagesToAnthropic(messages []modelpkg.Message, systemPrompt string) ([]anthropicsdk.TextBlockParam, []anthropicsdk.MessageParam) {
	var systemBlocks []anthropicsdk.TextBlockParam
	if trimmed := strings.TrimSpace(systemPrompt); trimmed != "" {
		systemBlocks = append(systemBlocks, anthropicsdk.TextBlockParam{Text: systemPrompt})
	}

	messageParams := make([]anthropicsdk.MessageParam, 0, len(messages))

	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "system" {
			if trimmed := strings.TrimSpace(msg.Content); trimmed != "" {
				systemBlocks = append(systemBlocks, anthropicsdk.TextBlockParam{Text: msg.Content})
			}
			continue
		}

		contentBlocks := buildContentBlocks(role, msg)
		if len(contentBlocks) == 0 {
			contentBlocks = []anthropicsdk.ContentBlockParamUnion{anthropicsdk.NewTextBlock("")}
		}

		messageParams = append(messageParams, anthropicsdk.MessageParam{
			Role:    normalizeRoleForAnthropic(role),
			Content: contentBlocks,
		})
	}

	if len(messageParams) == 0 {
		messageParams = append(messageParams, anthropicsdk.MessageParam{
			Role: anthropicsdk.MessageParamRoleUser,
			Content: []anthropicsdk.ContentBlockParamUnion{
				anthropicsdk.NewTextBlock(""),
			},
		})
	}

	return systemBlocks, messageParams
}

func buildContentBlocks(role string, msg modelpkg.Message) []anthropicsdk.ContentBlockParamUnion {
	switch role {
	case "tool":
		if blocks, ok := buildToolResultBlocks(msg); ok {
			return blocks
		}
	case "assistant":
		return buildAssistantBlocks(msg)
	}

	var blocks []anthropicsdk.ContentBlockParamUnion
	if msg.Content != "" {
		blocks = append(blocks, anthropicsdk.NewTextBlock(msg.Content))
	} else {
		// Anthropic API requires non-empty content, use placeholder
		blocks = append(blocks, anthropicsdk.NewTextBlock("."))
	}
	return blocks
}

func buildAssistantBlocks(msg modelpkg.Message) []anthropicsdk.ContentBlockParamUnion {
	var blocks []anthropicsdk.ContentBlockParamUnion
	if msg.Content != "" {
		blocks = append(blocks, anthropicsdk.NewTextBlock(msg.Content))
	}
	if len(msg.ToolCalls) == 0 {
		return blocks
	}

	for _, call := range msg.ToolCalls {
		name := strings.TrimSpace(call.Name)
		id := strings.TrimSpace(call.ID)
		if name == "" || id == "" {
			continue
		}
		args := cloneMap(call.Arguments)
		if args == nil {
			args = map[string]any{}
		}
		blocks = append(blocks, anthropicsdk.NewToolUseBlock(id, args, name))
	}
	return blocks
}

func buildToolResultBlocks(msg modelpkg.Message) ([]anthropicsdk.ContentBlockParamUnion, bool) {
	if len(msg.ToolCalls) == 0 {
		return nil, false
	}
	text := msg.Content
	isError := detectToolError(text)

	blocks := make([]anthropicsdk.ContentBlockParamUnion, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		id := strings.TrimSpace(call.ID)
		if id == "" {
			return nil, false
		}
		resultBlock := anthropicsdk.ToolResultBlockParam{
			ToolUseID: id,
			Content: []anthropicsdk.ToolResultBlockParamContentUnion{
				{OfText: &anthropicsdk.TextBlockParam{Text: text}},
			},
		}
		if isError {
			resultBlock.IsError = anthropicsdk.Bool(true)
		}
		blocks = append(blocks, anthropicsdk.ContentBlockParamUnion{OfToolResult: &resultBlock})
	}
	return blocks, true
}

func detectToolError(content string) bool {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return false
	}
	errVal, ok := payload["error"]
	if !ok {
		return false
	}
	switch v := errVal.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case bool:
		return v
	default:
		return v != nil
	}
}

func convertToolsToAnthropic(tools []map[string]any) ([]anthropicsdk.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	toolParams := make([]anthropicsdk.ToolUnionParam, 0, len(tools))

	for _, def := range tools {
		funcDef, ok := def["function"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := funcDef["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		inputSchema, err := convertToolParameters(funcDef["parameters"])
		if err != nil {
			return nil, fmt.Errorf("convert parameters for %s: %w", name, err)
		}

		tool := anthropicsdk.ToolParam{
			Name:        name,
			InputSchema: inputSchema,
		}
		if desc, _ := funcDef["description"].(string); strings.TrimSpace(desc) != "" {
			tool.Description = anthropicsdk.String(desc)
		}

		toolParams = append(toolParams, anthropicsdk.ToolUnionParam{OfTool: &tool})
	}

	return toolParams, nil
}

func convertToolParameters(raw any) (anthropicsdk.ToolInputSchemaParam, error) {
	params, _ := raw.(map[string]any)
	if len(params) == 0 {
		// Default to an object schema when no explicit parameters are provided.
		return anthropicsdk.ToolInputSchemaParam{Type: "object"}, nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return anthropicsdk.ToolInputSchemaParam{}, fmt.Errorf("marshal schema: %w", err)
	}
	var schema anthropicsdk.ToolInputSchemaParam
	if err := json.Unmarshal(data, &schema); err != nil {
		return anthropicsdk.ToolInputSchemaParam{}, fmt.Errorf("unmarshal schema: %w", err)
	}
	if schema.Type == "" {
		schema.Type = "object"
	}
	return schema, nil
}

func convertMessageFromAnthropic(msg anthropicsdk.Message) modelpkg.Message {
	result := modelpkg.Message{
		Role: string(msg.Role),
	}

	var textParts []string
	var toolCalls []modelpkg.ToolCall

	for _, block := range msg.Content {
		switch content := block.AsAny().(type) {
		case anthropicsdk.TextBlock:
			textParts = append(textParts, content.Text)
		case anthropicsdk.ToolUseBlock:
			toolCalls = append(toolCalls, modelpkg.ToolCall{
				ID:        content.ID,
				Name:      content.Name,
				Arguments: decodeToolInput(content.Input),
			})
		}
	}

	result.Content = strings.Join(textParts, "\n")
	result.ToolCalls = toolCalls
	if strings.TrimSpace(result.Role) == "" {
		result.Role = "assistant"
	}
	return result
}

func decodeToolInput(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{"value": typed}
	}
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		if src == nil {
			return nil
		}
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneValue(v)
	}
	return dst
}

func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, elem := range typed {
			out[i] = cloneValue(elem)
		}
		return out
	default:
		return typed
	}
}

func normalizeRoleForAnthropic(role string) anthropicsdk.MessageParamRole {
	switch role {
	case "assistant", "model":
		return anthropicsdk.MessageParamRoleAssistant
	case "tool":
		return anthropicsdk.MessageParamRoleUser
	default:
		return anthropicsdk.MessageParamRoleUser
	}
}

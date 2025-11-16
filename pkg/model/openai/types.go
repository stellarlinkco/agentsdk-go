package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	defaultBaseURL       = "https://api.openai.com"
	chatCompletionsPath  = "/v1/chat/completions"
	defaultHTTPTimeout   = 60 // seconds
	userAgent            = "agentsdk-go/openai"
	maxStreamLineBytes   = 1024 * 1024
	initialStreamBufSize = 64 * 1024
)

// ChatCompletionRequest models the OpenAI Chat Completions payload.
type ChatCompletionRequest struct {
	Model            string             `json:"model"`
	Messages         []ChatMessageParam `json:"messages"`
	Temperature      *float64           `json:"temperature,omitempty"`
	TopP             *float64           `json:"top_p,omitempty"`
	MaxTokens        int                `json:"max_tokens,omitempty"`
	PresencePenalty  *float64           `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64           `json:"frequency_penalty,omitempty"`
	Stop             []string           `json:"stop,omitempty"`
	Tools            []ToolDefinition   `json:"tools,omitempty"`
	ToolChoice       json.RawMessage    `json:"tool_choice,omitempty"`
	ResponseFormat   json.RawMessage    `json:"response_format,omitempty"`
	Stream           bool               `json:"stream"`
	Seed             *int               `json:"seed,omitempty"`
}

// ChatMessageParam describes a single request message.
type ChatMessageParam struct {
	Role       string                   `json:"role"`
	Content    *string                  `json:"content"`
	Name       string                   `json:"name,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
	ToolCalls  []AssistantToolCallParam `json:"tool_calls,omitempty"`
}

// AssistantToolCallParam serializes prior assistant tool calls.
type AssistantToolCallParam struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Function *FunctionCallParam `json:"function,omitempty"`
}

// FunctionCallParam is the request representation of a function call.
type FunctionCallParam struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition describes a function definition for function calling.
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition contains the schema for a callable function.
type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ChatCompletionResponse captures the non-streaming response schema subset.
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Object  string                 `json:"object"`
	Choices []ChatCompletionChoice `json:"choices"`
}

// ChatCompletionChoice wraps a single assistant message.
type ChatCompletionChoice struct {
	Index   int                           `json:"index"`
	Message ChatCompletionResponseMessage `json:"message"`
}

// ChatCompletionResponseMessage is the assistant payload.
type ChatCompletionResponseMessage struct {
	Role      string              `json:"role"`
	Content   MessageContent      `json:"content"`
	ToolCalls []AssistantToolCall `json:"tool_calls,omitempty"`
}

// AssistantToolCall represents a full tool call emitted by the assistant.
type AssistantToolCall struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function *FunctionCallBody `json:"function,omitempty"`
}

// FunctionCallBody contains the executable details of a function call.
type FunctionCallBody struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// MessageContent normalizes string vs array payloads.
type MessageContent []MessageContentPart

// MessageContentPart is a single segment of assistant output.
type MessageContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Text collapses all text parts into a single string.
func (c MessageContent) Text() string {
	if len(c) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range c {
		if part.Type == "text" && part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// UnmarshalJSON accepts either a simple string or array payload.
func (c *MessageContent) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*c = nil
		return nil
	}
	if data[0] == '[' {
		var parts []MessageContentPart
		if err := json.Unmarshal(data, &parts); err != nil {
			return err
		}
		*c = MessageContent(parts)
		return nil
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*c = MessageContent{{Type: "text", Text: text}}
		return nil
	}
	return fmt.Errorf("unsupported openai content payload: %s", string(data))
}

// ChatCompletionStreamChunk represents a streaming delta envelope.
type ChatCompletionStreamChunk struct {
	Choices []ChatCompletionStreamChoice `json:"choices"`
}

// ChatCompletionStreamChoice carries delta updates.
type ChatCompletionStreamChoice struct {
	Index        int                 `json:"index"`
	Delta        ChatCompletionDelta `json:"delta"`
	FinishReason string              `json:"finish_reason"`
}

// ChatCompletionDelta provides incremental tokens or tool call deltas.
type ChatCompletionDelta struct {
	Role      string                   `json:"role"`
	Content   DeltaContent             `json:"content"`
	ToolCalls []AssistantToolCallDelta `json:"tool_calls"`
}

// DeltaContent mirrors MessageContent for streaming.
type DeltaContent []MessageContentPart

// UnmarshalJSON supports both string and array payloads for streaming.
func (c *DeltaContent) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*c = nil
		return nil
	}
	if data[0] == '[' {
		var parts []MessageContentPart
		if err := json.Unmarshal(data, &parts); err != nil {
			return err
		}
		*c = DeltaContent(parts)
		return nil
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*c = DeltaContent{{Type: "text", Text: text}}
		return nil
	}
	return fmt.Errorf("unsupported openai delta content: %s", string(data))
}

// Text collapses streaming content to a single string.
func (c DeltaContent) Text() string {
	if len(c) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range c {
		if part.Type == "text" && part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// AssistantToolCallDelta accumulates partial function call data.
type AssistantToolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function *FunctionCallDelta `json:"function,omitempty"`
}

// FunctionCallDelta carries partial name/arguments.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ErrorResponse models OpenAI error payloads.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody contains the API error details.
type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// APIError surfaces HTTP metadata along with API error info.
type APIError struct {
	StatusCode int
	Type       string
	Code       string
	Message    string
}

func (e APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "openai API error (%d", e.StatusCode)
	if e.Type != "" {
		b.WriteString(", ")
		b.WriteString(e.Type)
	}
	b.WriteString(")")
	if e.Code != "" {
		b.WriteString(" code=")
		b.WriteString(e.Code)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

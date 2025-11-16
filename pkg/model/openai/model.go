package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

// Ensure Model implements modelpkg.Model.
var _ modelpkg.Model = (*Model)(nil)

// Model talks to OpenAI's Chat Completions API.
type Model struct {
	client  *http.Client
	baseURL string
	model   string
	headers map[string]string
	opts    modelOptions
}

// Generate performs a blocking chat completion request.
func (m *Model) Generate(ctx context.Context, messages []modelpkg.Message) (modelpkg.Message, error) {
	payload := m.buildPayload(messages, false)
	resp, err := m.doRequest(ctx, payload)
	if err != nil {
		return modelpkg.Message{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return modelpkg.Message{}, readAPIError(resp)
	}

	var completion ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return modelpkg.Message{}, fmt.Errorf("decode openai response: %w", err)
	}

	if len(completion.Choices) == 0 {
		return modelpkg.Message{}, errors.New("openai response contains no choices")
	}

	return convertChoice(completion.Choices[0])
}

// GenerateStream invokes the streaming endpoint and relays partial chunks.
func (m *Model) GenerateStream(ctx context.Context, messages []modelpkg.Message, cb modelpkg.StreamCallback) error {
	if cb == nil {
		return errors.New("openai stream callback is required")
	}

	payload := m.buildPayload(messages, true)
	resp, err := m.doRequest(ctx, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return readAPIError(resp)
	}

	stream := newChunkStream(cb)
	if err := stream.consume(ctx, resp.Body); err != nil {
		return err
	}
	return stream.finalize()
}

func (m *Model) buildPayload(messages []modelpkg.Message, stream bool) ChatCompletionRequest {
	payload := ChatCompletionRequest{
		Model:    m.model,
		Messages: toOpenAIMessages(messages),
		Stream:   stream,
	}
	if len(payload.Messages) == 0 {
		empty := ""
		payload.Messages = []ChatMessageParam{{Role: "user", Content: &empty}}
	}
	if m.opts.MaxTokens > 0 {
		payload.MaxTokens = m.opts.MaxTokens
	}
	if m.opts.Temperature != nil {
		payload.Temperature = m.opts.Temperature
	}
	if m.opts.TopP != nil {
		payload.TopP = m.opts.TopP
	}
	if m.opts.PresencePenalty != nil {
		payload.PresencePenalty = m.opts.PresencePenalty
	}
	if m.opts.FrequencyPenalty != nil {
		payload.FrequencyPenalty = m.opts.FrequencyPenalty
	}
	if len(m.opts.Stop) > 0 {
		payload.Stop = append([]string(nil), m.opts.Stop...)
	}
	if len(m.opts.Tools) > 0 {
		payload.Tools = cloneTools(m.opts.Tools)
	}
	if len(m.opts.ToolChoice) > 0 {
		payload.ToolChoice = append([]byte(nil), m.opts.ToolChoice...)
	}
	if len(m.opts.ResponseFormat) > 0 {
		payload.ResponseFormat = append([]byte(nil), m.opts.ResponseFormat...)
	}
	if m.opts.Seed != nil {
		payload.Seed = m.opts.Seed
	}
	return payload
}

func (m *Model) doRequest(ctx context.Context, payload ChatCompletionRequest) (*http.Response, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		return nil, fmt.Errorf("encode openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+chatCompletionsPath, &buf)
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}

	for k, v := range m.headers {
		if v == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	return m.client.Do(req)
}

func convertChoice(choice ChatCompletionChoice) (modelpkg.Message, error) {
	role := choice.Message.Role
	if role == "" {
		role = "assistant"
	}
	text := choice.Message.Content.Text()
	toolCalls, err := convertToolCalls(choice.Message.ToolCalls)
	if err != nil {
		return modelpkg.Message{}, err
	}
	return modelpkg.Message{
		Role:      role,
		Content:   text,
		ToolCalls: toolCalls,
	}, nil
}

func convertToolCalls(calls []AssistantToolCall) ([]modelpkg.ToolCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]modelpkg.ToolCall, 0, len(calls))
	for _, call := range calls {
		if call.Type != "function" || call.Function == nil {
			continue
		}
		args, err := decodeArguments(call.Function.Arguments)
		if err != nil {
			return nil, err
		}
		out = append(out, modelpkg.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
		})
	}
	return out, nil
}

func toOpenAIMessages(messages []modelpkg.Message) []ChatMessageParam {
	if len(messages) == 0 {
		return nil
	}
	out := make([]ChatMessageParam, 0, len(messages))
	for _, msg := range messages {
		role := normalizeRole(msg.Role)
		if role == "" {
			role = "user"
		}
		content := msg.Content
		var contentPtr *string
		if content != "" {
			contentPtr = stringPtr(content)
		}
		switch role {
		case "assistant":
			param := ChatMessageParam{Role: role, Content: contentPtr}
			if len(msg.ToolCalls) > 0 {
				param.ToolCalls = encodeToolCalls(msg.ToolCalls)
				if msg.Content == "" {
					param.Content = nil
				}
			}
			out = append(out, param)
		case "tool":
			param := ChatMessageParam{Role: role}
			if contentPtr != nil {
				param.Content = contentPtr
			} else {
				param.Content = stringPtr("")
			}
			if len(msg.ToolCalls) > 0 {
				param.ToolCallID = msg.ToolCalls[0].ID
				if name := strings.TrimSpace(msg.ToolCalls[0].Name); name != "" {
					param.Name = name
				}
			}
			out = append(out, param)
		case "system", "user":
			if contentPtr == nil {
				contentPtr = stringPtr("")
			}
			out = append(out, ChatMessageParam{Role: role, Content: contentPtr})
		default:
			if contentPtr == nil {
				contentPtr = stringPtr("")
			}
			out = append(out, ChatMessageParam{Role: "user", Content: contentPtr})
		}
	}
	return out
}

func encodeToolCalls(calls []modelpkg.ToolCall) []AssistantToolCallParam {
	if len(calls) == 0 {
		return nil
	}
	out := make([]AssistantToolCallParam, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		args := encodeArguments(call.Arguments)
		out = append(out, AssistantToolCallParam{
			ID:   call.ID,
			Type: "function",
			Function: &FunctionCallParam{
				Name:      name,
				Arguments: args,
			},
		})
	}
	return out
}

func stringPtr(s string) *string {
	ptr := new(string)
	*ptr = s
	return ptr
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
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("decode tool arguments: %w", err)
	}
	return out, nil
}

func normalizeRole(role string) string {
	trimmed := strings.ToLower(strings.TrimSpace(role))
	switch trimmed {
	case "assistant", "user", "system", "tool":
		return trimmed
	default:
		return "user"
	}
}

func readAPIError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("openai api status %d: %w", resp.StatusCode, err)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return APIError{StatusCode: resp.StatusCode, Message: resp.Status}
	}
	var apiErr ErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return APIError{
			StatusCode: resp.StatusCode,
			Type:       apiErr.Error.Type,
			Code:       apiErr.Error.Code,
			Message:    apiErr.Error.Message,
		}
	}
	return APIError{StatusCode: resp.StatusCode, Message: string(body)}
}

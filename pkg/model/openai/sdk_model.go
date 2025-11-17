package openai

import (
	"context"
	"fmt"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/telemetry"
)

// Ensure SDKModel implements the ModelWithTools interface.
var _ modelpkg.ModelWithTools = (*SDKModel)(nil)

// SDKModel wraps the official OpenAI SDK to implement our Model interface.
type SDKModel struct {
	client    openaisdk.Client
	model     openaisdk.ChatModel
	maxTokens int
}

// NewSDKModel creates a model backed by the official OpenAI SDK.
func NewSDKModel(apiKey, model string, maxTokens int) *SDKModel {
	return NewSDKModelWithBaseURL(apiKey, model, "", maxTokens)
}

// NewSDKModelWithBaseURL creates a model with custom base URL support.
func NewSDKModelWithBaseURL(apiKey, model, baseURL string, maxTokens int) *SDKModel {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openaisdk.NewClient(opts...)

	// Map model string to SDK constant
	sdkModel := mapToSDKModel(model)

	return &SDKModel{
		client:    client,
		model:     sdkModel,
		maxTokens: maxTokens,
	}
}

// Generate performs a blocking call without tools.
func (m *SDKModel) Generate(ctx context.Context, messages []modelpkg.Message) (_ modelpkg.Message, err error) {
	ctx, span := telemetry.StartSpan(ctx, "model.openai.sdk.generate",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(telemetry.SanitizeAttributes(
			attribute.String("llm.provider", "openai"),
			attribute.String("llm.model", string(m.model)),
			attribute.Bool("llm.stream", false),
		)...),
	)
	defer telemetry.EndSpan(span, err)

	return m.GenerateWithTools(ctx, messages, nil)
}

// GenerateWithTools performs a blocking call with tool definitions.
func (m *SDKModel) GenerateWithTools(ctx context.Context, messages []modelpkg.Message, tools []map[string]any) (_ modelpkg.Message, err error) {
	ctx, span := telemetry.StartSpan(ctx, "model.openai.sdk.generate_with_tools",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(telemetry.SanitizeAttributes(
			attribute.String("llm.provider", "openai"),
			attribute.String("llm.model", string(m.model)),
			attribute.Bool("llm.stream", false),
			attribute.Int("llm.tools_count", len(tools)),
		)...),
	)
	defer telemetry.EndSpan(span, err)

	// Convert messages
	messageParams, err := convertMessagesToOpenAI(messages)
	if err != nil {
		return modelpkg.Message{}, err
	}

	// Build request params
	params := openaisdk.ChatCompletionNewParams{
		Messages: messageParams,
		Model:    m.model,
	}

	if m.maxTokens > 0 {
		params.MaxTokens = openaisdk.Int(int64(m.maxTokens))
	}

	// Add tools if present
	if len(tools) > 0 {
		toolParams, err := convertToolsToOpenAI(tools)
		if err != nil {
			return modelpkg.Message{}, fmt.Errorf("convert tools: %w", err)
		}
		params.Tools = toolParams
	}

	// Call SDK
	completion, err := m.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return modelpkg.Message{}, fmt.Errorf("openai sdk call: %w", err)
	}

	if len(completion.Choices) == 0 {
		return modelpkg.Message{}, fmt.Errorf("no choices in response")
	}

	// Convert response
	return convertMessageFromOpenAI(completion.Choices[0].Message)
}

// GenerateStream implements streaming with callback (required by Model interface).
func (m *SDKModel) GenerateStream(ctx context.Context, messages []modelpkg.Message, cb modelpkg.StreamCallback) (err error) {
	if cb == nil {
		return fmt.Errorf("openai stream callback is required")
	}

	ctx, span := telemetry.StartSpan(ctx, "model.openai.sdk.generate_stream",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(telemetry.SanitizeAttributes(
			attribute.String("llm.provider", "openai"),
			attribute.String("llm.model", string(m.model)),
			attribute.Bool("llm.stream", true),
		)...),
	)
	defer telemetry.EndSpan(span, err)

	// Convert messages
	messageParams, err := convertMessagesToOpenAI(messages)
	if err != nil {
		return err
	}

	// Build request params
	params := openaisdk.ChatCompletionNewParams{
		Messages: messageParams,
		Model:    m.model,
	}

	if m.maxTokens > 0 {
		params.MaxTokens = openaisdk.Int(int64(m.maxTokens))
	}

	// Create streaming request
	stream := m.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	// Use SDK accumulator
	acc := openaisdk.ChatCompletionAccumulator{}

	for stream.Next() {
		chunk := stream.Current()
		if !acc.AddChunk(chunk) {
			return fmt.Errorf("accumulate stream chunk failed")
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				if err := cb(modelpkg.StreamResult{
					Message: modelpkg.Message{
						Role:    "assistant",
						Content: delta.Content,
					},
					Final: false,
				}); err != nil {
					return err
				}
			}
		}

		if finishedTool, ok := acc.JustFinishedToolCall(); ok {
			args, err := decodeArguments(finishedTool.Arguments)
			if err != nil {
				return fmt.Errorf("decode streaming tool call: %w", err)
			}
			if err := cb(modelpkg.StreamResult{
				Message: modelpkg.Message{
					Role: "assistant",
					ToolCalls: []modelpkg.ToolCall{{
						ID:        finishedTool.ID,
						Name:      finishedTool.Name,
						Arguments: args,
					}},
				},
				Final: false,
			}); err != nil {
				return err
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("stream error: %w", err)
	}

	// Send final message
	if len(acc.Choices) == 0 {
		return fmt.Errorf("openai stream produced no choices")
	}

	finalMsg, err := convertMessageFromOpenAI(acc.Choices[0].Message)
	if err != nil {
		return err
	}

	return cb(modelpkg.StreamResult{
		Message: finalMsg,
		Final:   true,
	})
}

// mapToSDKModel maps model string to SDK constant.
func mapToSDKModel(model string) openaisdk.ChatModel {
	switch model {
	case "gpt-4o", "gpt-4o-latest":
		return openaisdk.ChatModelGPT4o
	case "gpt-4o-mini":
		return openaisdk.ChatModelGPT4oMini
	case "gpt-4-turbo", "gpt-4-turbo-preview":
		return openaisdk.ChatModelGPT4Turbo
	case "gpt-4":
		return openaisdk.ChatModelGPT4
	case "gpt-3.5-turbo":
		return openaisdk.ChatModelGPT3_5Turbo
	default:
		// Default to gpt-4o
		return openaisdk.ChatModelGPT4o
	}
}

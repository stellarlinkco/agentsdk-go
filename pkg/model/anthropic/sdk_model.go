package anthropic

import (
	"context"
	"errors"
	"fmt"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/telemetry"
)

const (
	defaultMaxTokens = 4096
)

// Ensure SDKModel implements the ModelWithTools interface.
var _ modelpkg.ModelWithTools = (*SDKModel)(nil)

// SDKModel wraps the official Anthropic SDK to implement our Model interface.
type SDKModel struct {
	client    *anthropicsdk.Client
	model     anthropicsdk.Model
	maxTokens int
	system    string
}

// NewSDKModel creates a model backed by the official Anthropic SDK.
func NewSDKModel(apiKey, model string, maxTokens int) *SDKModel {
	return NewSDKModelWithBaseURL(apiKey, model, "", maxTokens)
}

// NewSDKModelWithBaseURL creates a model with custom base URL support.
func NewSDKModelWithBaseURL(apiKey, model, baseURL string, maxTokens int) *SDKModel {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := anthropicsdk.NewClient(opts...)

	// Map model string to SDK constant
	sdkModel := mapToSDKModel(model)

	return &SDKModel{
		client:    &client,
		model:     sdkModel,
		maxTokens: maxTokens,
	}
}

// Generate performs a blocking call without tools.
func (m *SDKModel) Generate(ctx context.Context, messages []modelpkg.Message) (_ modelpkg.Message, err error) {
	ctx, span := telemetry.StartSpan(ctx, "model.anthropic.sdk.generate",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(telemetry.SanitizeAttributes(
			attribute.String("llm.provider", "anthropic"),
			attribute.String("llm.model", string(m.model)),
			attribute.Bool("llm.stream", false),
		)...),
	)
	defer telemetry.EndSpan(span, err)

	return m.GenerateWithTools(ctx, messages, nil)
}

// GenerateWithTools performs a blocking call with tool definitions.
func (m *SDKModel) GenerateWithTools(ctx context.Context, messages []modelpkg.Message, tools []map[string]any) (_ modelpkg.Message, err error) {
	ctx, span := telemetry.StartSpan(ctx, "model.anthropic.sdk.generate_with_tools",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(telemetry.SanitizeAttributes(
			attribute.String("llm.provider", "anthropic"),
			attribute.String("llm.model", string(m.model)),
			attribute.Bool("llm.stream", false),
			attribute.Int("llm.tools_count", len(tools)),
		)...),
	)
	defer telemetry.EndSpan(span, err)

	// Convert messages
	systemBlocks, messageParams := convertMessagesToAnthropic(messages, m.system)
	maxTokens := m.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// Build request params
	params := anthropicsdk.MessageNewParams{
		Model:     m.model,
		MaxTokens: int64(maxTokens),
		Messages:  messageParams,
	}

	// Add system prompt if present
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	// Add tools if present
	if len(tools) > 0 {
		toolParams, err := convertToolsToAnthropic(tools)
		if err != nil {
			return modelpkg.Message{}, fmt.Errorf("convert tools: %w", err)
		}
		if len(toolParams) > 0 {
			params.Tools = toolParams
		}
	}

	// Call SDK
	message, err := m.client.Messages.New(ctx, params)
	if err != nil {
		return modelpkg.Message{}, fmt.Errorf("anthropic sdk call: %w", err)
	}

	// Convert response
	return convertMessageFromAnthropic(*message), nil
}

// GenerateStream implements streaming with callback (required by Model interface).
func (m *SDKModel) GenerateStream(ctx context.Context, messages []modelpkg.Message, cb modelpkg.StreamCallback) (err error) {
	if cb == nil {
		return errors.New("anthropic sdk stream callback is required")
	}

	ctx, span := telemetry.StartSpan(ctx, "model.anthropic.sdk.generate_stream",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(telemetry.SanitizeAttributes(
			attribute.String("llm.provider", "anthropic"),
			attribute.String("llm.model", string(m.model)),
			attribute.Bool("llm.stream", true),
		)...),
	)
	defer telemetry.EndSpan(span, err)

	// Convert messages
	systemBlocks, messageParams := convertMessagesToAnthropic(messages, m.system)
	maxTokens := m.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// Build request params
	params := anthropicsdk.MessageNewParams{
		Model:     m.model,
		MaxTokens: int64(maxTokens),
		Messages:  messageParams,
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	// Create streaming request
	stream := m.client.Messages.NewStreaming(ctx, params)

	// Accumulate message
	message := anthropicsdk.Message{}

	for stream.Next() {
		event := stream.Current()

		// Accumulate into final message
		if err := message.Accumulate(event); err != nil {
			return fmt.Errorf("accumulate stream: %w", err)
		}

		// Send delta events
		switch delta := event.AsAny().(type) {
		case anthropicsdk.ContentBlockDeltaEvent:
			switch text := delta.Delta.AsAny().(type) {
			case anthropicsdk.TextDelta:
				if err := cb(modelpkg.StreamResult{
					Message: modelpkg.Message{Role: "assistant", Content: text.Text},
					Final:   false,
				}); err != nil {
					return err
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("stream error: %w", err)
	}

	// Send final message
	finalMsg := convertMessageFromAnthropic(message)
	return cb(modelpkg.StreamResult{
		Message: finalMsg,
		Final:   true,
	})
}

// SetSystem sets the system prompt.
func (m *SDKModel) SetSystem(system string) {
	m.system = system
}

// mapToSDKModel maps model string to SDK constant.
func mapToSDKModel(model string) anthropicsdk.Model {
	switch model {
	case "claude-3-5-sonnet-20241022":
		return anthropicsdk.ModelClaudeSonnet4_5_20250929
	case "claude-3-5-sonnet-latest":
		return anthropicsdk.ModelClaudeSonnet4_5_20250929
	case "claude-3-5-haiku-20241022":
		return anthropicsdk.ModelClaude3_5Haiku20241022
	case "claude-3-5-haiku-latest":
		return anthropicsdk.ModelClaude3_5HaikuLatest
	case "claude-3-opus-20240229":
		return anthropicsdk.ModelClaude_3_Opus_20240229
	case "claude-3-opus-latest":
		return anthropicsdk.ModelClaude3OpusLatest
	default:
		// Default to latest sonnet
		return anthropicsdk.ModelClaudeSonnet4_5_20250929
	}
}

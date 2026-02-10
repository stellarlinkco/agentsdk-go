package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockOpenAIChatCompletions implements openaiChatCompletions for testing
type mockOpenAIChatCompletions struct {
	newFunc        func(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) (*openai.ChatCompletion, error)
	streamFunc     func(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk]
	capturedParams openai.ChatCompletionNewParams
}

func (m *mockOpenAIChatCompletions) New(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) (*openai.ChatCompletion, error) {
	m.capturedParams = params
	if m.newFunc != nil {
		return m.newFunc(ctx, params, opts...)
	}
	return nil, errors.New("mock: New not implemented")
}

func (m *mockOpenAIChatCompletions) NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk] {
	m.capturedParams = params
	if m.streamFunc != nil {
		return m.streamFunc(ctx, params, opts...)
	}
	return nil
}

func TestNewOpenAI(t *testing.T) {
	tests := []struct {
		name    string
		cfg     OpenAIConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: OpenAIConfig{
				APIKey: "sk-test-key",
				Model:  "gpt-4o",
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			cfg: OpenAIConfig{
				Model: "gpt-4o",
			},
			wantErr: true,
			errMsg:  "openai: api key required",
		},
		{
			name: "whitespace API key",
			cfg: OpenAIConfig{
				APIKey: "   ",
			},
			wantErr: true,
			errMsg:  "openai: api key required",
		},
		{
			name: "default model when empty",
			cfg: OpenAIConfig{
				APIKey: "sk-test",
			},
			wantErr: false,
		},
		{
			name: "with custom base URL",
			cfg: OpenAIConfig{
				APIKey:  "sk-test",
				BaseURL: "https://custom.api.com",
			},
			wantErr: false,
		},
		{
			name: "with all options",
			cfg: OpenAIConfig{
				APIKey:      "sk-test",
				BaseURL:     "https://custom.api.com",
				Model:       "gpt-4-turbo",
				MaxTokens:   8192,
				MaxRetries:  5,
				System:      "You are a helpful assistant",
				Temperature: func() *float64 { v := 0.7; return &v }(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mdl, err := NewOpenAI(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, mdl)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, mdl)
			}
		})
	}
}

func TestOpenAIModel_Complete(t *testing.T) {
	tests := []struct {
		name        string
		request     Request
		mockResp    *openai.ChatCompletion
		mockErr     error
		wantErr     bool
		wantRole    string
		wantContent string
	}{
		{
			name: "simple completion",
			request: Request{
				Messages: []Message{
					{Role: "user", Content: "Hello"},
				},
			},
			mockResp: &openai.ChatCompletion{
				ID:    "chatcmpl-123",
				Model: "gpt-4o",
				Choices: []openai.ChatCompletionChoice{
					{
						Index:        0,
						FinishReason: "stop",
						Message: openai.ChatCompletionMessage{
							Role:    "assistant",
							Content: "Hello! How can I help you?",
						},
					},
				},
				Usage: openai.CompletionUsage{
					PromptTokens:     10,
					CompletionTokens: 20,
					TotalTokens:      30,
				},
			},
			wantRole:    "assistant",
			wantContent: "Hello! How can I help you?",
		},
		{
			name: "completion with tool calls",
			request: Request{
				Messages: []Message{
					{Role: "user", Content: "What's the weather?"},
				},
				Tools: []ToolDefinition{
					{Name: "get_weather", Description: "Get weather"},
				},
			},
			mockResp: &openai.ChatCompletion{
				ID: "chatcmpl-456",
				Choices: []openai.ChatCompletionChoice{
					{
						FinishReason: "tool_calls",
						Message: openai.ChatCompletionMessage{
							Role: "assistant",
							ToolCalls: []openai.ChatCompletionMessageToolCall{
								{
									ID: "call_abc123",
									Function: openai.ChatCompletionMessageToolCallFunction{
										Name:      "get_weather",
										Arguments: `{"location":"Tokyo"}`,
									},
								},
							},
						},
					},
				},
			},
			wantRole: "assistant",
		},
		{
			name: "API error",
			request: Request{
				Messages: []Message{
					{Role: "user", Content: "test"},
				},
			},
			mockErr: &openai.Error{
				StatusCode: http.StatusUnauthorized,
				Message:    "Invalid API key",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockOpenAIChatCompletions{
				newFunc: func(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) (*openai.ChatCompletion, error) {
					return tt.mockResp, tt.mockErr
				},
			}

			mdl := &openaiModel{
				completions: mock,
				model:       "gpt-4o",
				maxTokens:   4096,
				maxRetries:  0, // No retries for testing
			}

			resp, err := mdl.Complete(context.Background(), tt.request)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantRole, resp.Message.Role)
			if tt.wantContent != "" {
				assert.Equal(t, tt.wantContent, resp.Message.Content)
			}

			if tt.name == "completion with tool calls" {
				require.Len(t, resp.Message.ToolCalls, 1)
				assert.Equal(t, "call_abc123", resp.Message.ToolCalls[0].ID)
				assert.Equal(t, "get_weather", resp.Message.ToolCalls[0].Name)
				assert.Equal(t, "Tokyo", resp.Message.ToolCalls[0].Arguments["location"])
			}
		})
	}
}

func TestConvertMessagesToOpenAI(t *testing.T) {
	tests := []struct {
		name     string
		msgs     []Message
		defaults []string
		wantLen  int
	}{
		{
			name:    "empty messages adds placeholder",
			msgs:    []Message{},
			wantLen: 1, // placeholder user message
		},
		{
			name: "user message",
			msgs: []Message{
				{Role: "user", Content: "Hello"},
			},
			wantLen: 1,
		},
		{
			name: "system message in defaults",
			msgs: []Message{
				{Role: "user", Content: "Hello"},
			},
			defaults: []string{"You are helpful"},
			wantLen:  2, // system + user
		},
		{
			name: "assistant with tool calls",
			msgs: []Message{
				{Role: "user", Content: "test"},
				{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{ID: "call_1", Name: "tool1", Arguments: map[string]any{"a": "b"}},
					},
				},
			},
			wantLen: 2,
		},
		{
			name: "tool result message",
			msgs: []Message{
				{Role: "user", Content: "test"},
				{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "tool1"}}},
				{Role: "tool", ToolCalls: []ToolCall{{ID: "call_1", Result: "result data"}}},
			},
			wantLen: 3,
		},
		{
			name: "system message in content",
			msgs: []Message{
				{Role: "system", Content: "Be concise"},
				{Role: "user", Content: "Hello"},
			},
			wantLen: 2, // system + user
		},
		{
			name: "empty content user message",
			msgs: []Message{
				{Role: "user", Content: "  "},
			},
			wantLen: 1, // with placeholder
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMessagesToOpenAI(tt.msgs, tt.defaults...)
			assert.Len(t, result, tt.wantLen)
		})
	}
}

func TestConvertToolsToOpenAI(t *testing.T) {
	tests := []struct {
		name    string
		tools   []ToolDefinition
		wantLen int
	}{
		{
			name:    "empty tools",
			tools:   []ToolDefinition{},
			wantLen: 0,
		},
		{
			name: "single tool",
			tools: []ToolDefinition{
				{
					Name:        "get_weather",
					Description: "Get weather for a location",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{"type": "string"},
						},
					},
				},
			},
			wantLen: 1,
		},
		{
			name: "skip empty name",
			tools: []ToolDefinition{
				{Name: "", Description: "ignored"},
				{Name: "valid", Description: "kept"},
			},
			wantLen: 1,
		},
		{
			name: "multiple tools",
			tools: []ToolDefinition{
				{Name: "tool1", Description: "First tool"},
				{Name: "tool2", Description: "Second tool"},
				{Name: "tool3", Description: "Third tool"},
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToolsToOpenAI(tt.tools)
			assert.Len(t, result, tt.wantLen)

			if len(tt.tools) > 0 && tt.wantLen > 0 {
				for i, tool := range result {
					assert.NotEmpty(t, tool.Function.Name)
					if tt.tools[i].Description != "" {
						assert.NotNil(t, tool.Function.Description)
					}
				}
			}
		})
	}
}

func TestConvertOpenAIResponse(t *testing.T) {
	tests := []struct {
		name        string
		completion  *openai.ChatCompletion
		wantRole    string
		wantContent string
		wantTools   int
	}{
		{
			name:        "nil completion",
			completion:  nil,
			wantRole:    "assistant",
			wantContent: "",
		},
		{
			name: "empty choices",
			completion: &openai.ChatCompletion{
				Choices: []openai.ChatCompletionChoice{},
			},
			wantRole: "assistant",
		},
		{
			name: "text response",
			completion: &openai.ChatCompletion{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role:    "assistant",
							Content: "Hello!",
						},
						FinishReason: "stop",
					},
				},
			},
			wantRole:    "assistant",
			wantContent: "Hello!",
		},
		{
			name: "tool calls response",
			completion: &openai.ChatCompletion{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role: "assistant",
							ToolCalls: []openai.ChatCompletionMessageToolCall{
								{
									ID: "call_1",
									Function: openai.ChatCompletionMessageToolCallFunction{
										Name:      "test_tool",
										Arguments: `{"key":"value"}`,
									},
								},
							},
						},
					},
				},
			},
			wantRole:  "assistant",
			wantTools: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := convertOpenAIResponse(tt.completion)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantRole, resp.Message.Role)
			if tt.wantContent != "" {
				assert.Equal(t, tt.wantContent, resp.Message.Content)
			}
			assert.Len(t, resp.Message.ToolCalls, tt.wantTools)
		})
	}
}

func TestConvertOpenAIUsage(t *testing.T) {
	usage := openai.CompletionUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	result := convertOpenAIUsage(usage)

	assert.Equal(t, 100, result.InputTokens)
	assert.Equal(t, 50, result.OutputTokens)
	assert.Equal(t, 150, result.TotalTokens)
}

func TestParseJSONArgs(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantNil bool
		wantKey string
	}{
		{
			name:    "empty string",
			raw:     "",
			wantNil: true,
		},
		{
			name:    "valid JSON",
			raw:     `{"location":"Tokyo"}`,
			wantKey: "location",
		},
		{
			name:    "invalid JSON returns raw",
			raw:     "not json",
			wantKey: "raw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseJSONArgs(tt.raw)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.Contains(t, result, tt.wantKey)
			}
		})
	}
}

func TestIsOpenAIRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "context canceled not retryable",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "deadline exceeded not retryable",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "unauthorized not retryable",
			err:  &openai.Error{StatusCode: http.StatusUnauthorized},
			want: false,
		},
		{
			name: "rate limit retryable",
			err:  &openai.Error{StatusCode: http.StatusTooManyRequests},
			want: true,
		},
		{
			name: "server error retryable",
			err:  &openai.Error{StatusCode: http.StatusInternalServerError},
			want: true,
		},
		{
			name: "generic error retryable",
			err:  errors.New("connection reset"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOpenAIRetryable(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOpenAIProvider(t *testing.T) {
	t.Run("resolves API key from env", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-env-key")

		p := &OpenAIProvider{}
		key := p.resolveAPIKey()
		assert.Equal(t, "sk-env-key", key)
	})

	t.Run("prefers explicit API key", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-env-key")

		p := &OpenAIProvider{APIKey: "sk-explicit"}
		key := p.resolveAPIKey()
		assert.Equal(t, "sk-explicit", key)
	})

	t.Run("caching works with TTL", func(t *testing.T) {
		p := &OpenAIProvider{
			APIKey:   "sk-test",
			CacheTTL: 1 * time.Hour,
		}

		// First call creates model
		mdl1, err := p.Model(context.Background())
		require.NoError(t, err)
		require.NotNil(t, mdl1)

		// Second call returns cached model
		mdl2, err := p.Model(context.Background())
		require.NoError(t, err)
		assert.Same(t, mdl1, mdl2)
	})

	t.Run("no caching when TTL is 0", func(t *testing.T) {
		p := &OpenAIProvider{
			APIKey:   "sk-test",
			CacheTTL: 0,
		}

		// Each call creates new model
		mdl1, err := p.Model(context.Background())
		require.NoError(t, err)

		mdl2, err := p.Model(context.Background())
		require.NoError(t, err)

		// Different instances
		assert.NotSame(t, mdl1, mdl2)
	})
}

func TestBuildOpenAIAssistantMessage(t *testing.T) {
	t.Run("simple content", func(t *testing.T) {
		msg := Message{Role: "assistant", Content: "Hello"}
		result := buildOpenAIAssistantMessage(msg)

		// Verify it's an assistant message - when no tool calls, uses AssistantMessage() helper
		// which sets OfAssistant with content
		require.NotNil(t, result.OfAssistant)
	})

	t.Run("with reasoning_content sets extra fields", func(t *testing.T) {
		msg := Message{
			Role:             "assistant",
			Content:          "The answer is 42",
			ReasoningContent: "Let me think about this...",
		}
		result := buildOpenAIAssistantMessage(msg)

		require.NotNil(t, result.OfAssistant)
		// Verify the message was created with reasoning content
		// Marshal to JSON to check extra fields are set
		data, err := json.Marshal(result.OfAssistant)
		require.NoError(t, err)
		assert.Contains(t, string(data), "reasoning_content")
		assert.Contains(t, string(data), "Let me think about this...")
	})

	t.Run("empty reasoning_content does not set extra fields", func(t *testing.T) {
		msg := Message{Role: "assistant", Content: "Hello", ReasoningContent: ""}
		result := buildOpenAIAssistantMessage(msg)

		require.NotNil(t, result.OfAssistant)
		// When ReasoningContent is empty, extra fields should not be set
		data, err := json.Marshal(result.OfAssistant)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "reasoning_content")
	})

	t.Run("with tool calls", func(t *testing.T) {
		msg := Message{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_1", Name: "tool1", Arguments: map[string]any{"a": 1}},
			},
		}
		result := buildOpenAIAssistantMessage(msg)

		// Verify it's an assistant message with tool calls
		require.NotNil(t, result.OfAssistant)
		assert.Len(t, result.OfAssistant.ToolCalls, 1)
		assert.Equal(t, "call_1", result.OfAssistant.ToolCalls[0].ID)
		assert.Equal(t, "tool1", result.OfAssistant.ToolCalls[0].Function.Name)
	})

	t.Run("empty content placeholder", func(t *testing.T) {
		msg := Message{Role: "assistant", Content: "   "}
		result := buildOpenAIAssistantMessage(msg)

		// Should create assistant message with placeholder
		require.NotNil(t, result.OfAssistant)
	})
}

func TestBuildOpenAIToolResults(t *testing.T) {
	t.Run("single tool result", func(t *testing.T) {
		msg := Message{
			Role: "tool",
			ToolCalls: []ToolCall{
				{ID: "call_1", Result: "result data"},
			},
		}
		results := buildOpenAIToolResults(msg)

		assert.Len(t, results, 1)
		toolCallID := results[0].GetToolCallID()
		require.NotNil(t, toolCallID)
		assert.Equal(t, "call_1", *toolCallID)
	})

	t.Run("multiple tool results", func(t *testing.T) {
		msg := Message{
			Role: "tool",
			ToolCalls: []ToolCall{
				{ID: "call_1", Result: "result 1"},
				{ID: "call_2", Result: "result 2"},
			},
		}
		results := buildOpenAIToolResults(msg)

		assert.Len(t, results, 2)
	})

	t.Run("no tool calls uses content", func(t *testing.T) {
		msg := Message{Role: "tool", Content: "fallback"}
		results := buildOpenAIToolResults(msg)

		assert.Len(t, results, 1)
	})
}

func TestToolCallAccumulator(t *testing.T) {
	t.Run("complete tool call", func(t *testing.T) {
		acc := &toolCallAccumulator{
			id:   "call_123",
			name: "my_tool",
		}
		acc.arguments.WriteString(`{"key":"value"}`)

		tc := acc.toToolCall()
		require.NotNil(t, tc)
		assert.Equal(t, "call_123", tc.ID)
		assert.Equal(t, "my_tool", tc.Name)
		assert.Equal(t, "value", tc.Arguments["key"])
	})

	t.Run("missing id returns nil", func(t *testing.T) {
		acc := &toolCallAccumulator{name: "tool"}
		assert.Nil(t, acc.toToolCall())
	})

	t.Run("missing name returns nil", func(t *testing.T) {
		acc := &toolCallAccumulator{id: "call_1"}
		assert.Nil(t, acc.toToolCall())
	})
}

func TestConvertToFunctionParameters(t *testing.T) {
	t.Run("nil params", func(t *testing.T) {
		result := convertToFunctionParameters(nil)
		assert.Equal(t, "object", result["type"])
	})

	t.Run("empty params", func(t *testing.T) {
		result := convertToFunctionParameters(map[string]any{})
		assert.Equal(t, "object", result["type"])
	})

	t.Run("with existing type", func(t *testing.T) {
		params := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		result := convertToFunctionParameters(params)
		assert.Equal(t, "object", result["type"])
		assert.NotNil(t, result["properties"])
	})

	t.Run("adds type if missing", func(t *testing.T) {
		params := map[string]any{
			"properties": map[string]any{},
		}
		result := convertToFunctionParameters(params)
		assert.Equal(t, "object", result["type"])
	})
}

func TestOpenAIModel_SelectModel(t *testing.T) {
	mdl := &openaiModel{model: "gpt-4o"}

	t.Run("uses override when provided", func(t *testing.T) {
		result := mdl.selectModel("gpt-4-turbo")
		assert.Equal(t, "gpt-4-turbo", result)
	})

	t.Run("uses default when empty override", func(t *testing.T) {
		result := mdl.selectModel("")
		assert.Equal(t, "gpt-4o", result)
	})

	t.Run("trims whitespace", func(t *testing.T) {
		result := mdl.selectModel("  gpt-4  ")
		assert.Equal(t, "gpt-4", result)
	})
}

// Test JSON marshaling of tool call arguments
func TestToolCallArgumentsJSON(t *testing.T) {
	args := map[string]any{
		"string":  "value",
		"number":  42.0,
		"boolean": true,
		"nested":  map[string]any{"key": "val"},
	}

	data, err := json.Marshal(args)
	require.NoError(t, err)

	parsed := parseJSONArgs(string(data))
	assert.Equal(t, "value", parsed["string"])
	assert.Equal(t, 42.0, parsed["number"])
	assert.Equal(t, true, parsed["boolean"])
}

// TestOpenAIModel_CompleteStream tests streaming completion
func TestOpenAIModel_CompleteStream(t *testing.T) {
	t.Run("nil callback returns error", func(t *testing.T) {
		mdl := &openaiModel{
			completions: &mockOpenAIChatCompletions{},
			model:       "gpt-4o",
			maxTokens:   4096,
		}

		err := mdl.CompleteStream(context.Background(), Request{
			Messages: []Message{{Role: "user", Content: "test"}},
		}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "stream callback required")
	})

	t.Run("nil stream returns error", func(t *testing.T) {
		mock := &mockOpenAIChatCompletions{
			streamFunc: func(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk] {
				return nil
			},
		}

		mdl := &openaiModel{
			completions: mock,
			model:       "gpt-4o",
			maxTokens:   4096,
			maxRetries:  0,
		}

		err := mdl.CompleteStream(context.Background(), Request{
			Messages: []Message{{Role: "user", Content: "test"}},
		}, func(sr StreamResult) error {
			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "stream not available")
	})
}

// TestOpenAIModel_DoWithRetry tests retry logic
func TestOpenAIModel_DoWithRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		mdl := &openaiModel{maxRetries: 3}
		attempts := 0

		err := mdl.doWithRetry(context.Background(), func(ctx context.Context) error {
			attempts++
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 1, attempts)
	})

	t.Run("retries on retryable error", func(t *testing.T) {
		mdl := &openaiModel{maxRetries: 3}
		attempts := 0

		err := mdl.doWithRetry(context.Background(), func(ctx context.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("transient error")
			}
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 3, attempts)
	})

	t.Run("stops on non-retryable error", func(t *testing.T) {
		mdl := &openaiModel{maxRetries: 5}
		attempts := 0

		err := mdl.doWithRetry(context.Background(), func(ctx context.Context) error {
			attempts++
			return context.Canceled
		})

		require.Error(t, err)
		assert.Equal(t, 1, attempts) // Should not retry
	})

	t.Run("respects max retries", func(t *testing.T) {
		mdl := &openaiModel{maxRetries: 2}
		attempts := 0

		err := mdl.doWithRetry(context.Background(), func(ctx context.Context) error {
			attempts++
			return errors.New("always fails")
		})

		require.Error(t, err)
		assert.Equal(t, 3, attempts) // Initial + 2 retries
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		mdl := &openaiModel{maxRetries: 10}
		ctx, cancel := context.WithCancel(context.Background())
		attempts := 0

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := mdl.doWithRetry(ctx, func(ctx context.Context) error {
			attempts++
			return errors.New("always fails")
		})

		require.Error(t, err)
		assert.True(t, attempts <= 3) // Should stop early due to cancellation
	})
}

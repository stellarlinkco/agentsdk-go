package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

func TestProviderNewModelValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := NewProvider(nil)

	_, err := p.NewModel(ctx, modelpkg.ModelConfig{Provider: "openai"})
	if err == nil || !strings.Contains(err.Error(), "api key") {
		t.Fatalf("expected api key error, got %v", err)
	}

	_, err = p.NewModel(ctx, modelpkg.ModelConfig{Provider: "openai", APIKey: "sk-test"})
	if err == nil || !strings.Contains(err.Error(), "model name") {
		t.Fatalf("expected model name error, got %v", err)
	}
}

func TestModelGenerate(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var captured ChatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != chatCompletionsPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("missing auth header, got %q", got)
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		resp := ChatCompletionResponse{
			Object: "chat.completion",
			Choices: []ChatCompletionChoice{{
				Index: 0,
				Message: ChatCompletionResponseMessage{
					Role: "assistant",
					Content: MessageContent{
						{Type: "text", Text: "hello"},
					},
					ToolCalls: []AssistantToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: &FunctionCallBody{
								Name:      "lookup_weather",
								Arguments: `{"city":"SF"}`,
							},
						},
					},
				},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	model := newTestModel(t, server)
	msg, err := model.Generate(context.Background(), []modelpkg.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "weather"},
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if msg.Content != "hello" || msg.Role != "assistant" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("tool calls: %+v", msg.ToolCalls)
	}
	if city, ok := msg.ToolCalls[0].Arguments["city"]; !ok || city != "SF" {
		t.Fatalf("tool call args: %+v", msg.ToolCalls[0].Arguments)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured.Messages) != 2 {
		t.Fatalf("request messages: %+v", captured.Messages)
	}
	if captured.Stream {
		t.Fatalf("expected non-stream request")
	}
}

func TestModelGenerateStreamText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(streamHandler(t, []string{
		`{"choices":[{"delta":{"role":"assistant","content":[{"type":"text","text":"Hel"}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":[{"type":"text","text":"lo"}]},"finish_reason":"stop"}]}`,
	}))
	defer server.Close()

	model := newTestModel(t, server)
	var chunks []string
	var final modelpkg.Message
	err := model.GenerateStream(context.Background(), []modelpkg.Message{
		{Role: "user", Content: "hi"},
	}, func(res modelpkg.StreamResult) error {
		if res.Final {
			final = res.Message
			return nil
		}
		if res.Message.Content != "" {
			chunks = append(chunks, res.Message.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream error: %v", err)
	}
	if final.Content != "Hello" {
		t.Fatalf("final message: %+v", final)
	}
	if got := strings.Join(chunks, ""); got != "Hello" {
		t.Fatalf("chunks mismatch: %s", got)
	}
}

func TestModelGenerateStreamFunctionCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(streamHandler(t, []string{
		`{"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Boston\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}))
	defer server.Close()

	model := newTestModel(t, server)
	var toolUpdates []modelpkg.ToolCall
	var final modelpkg.Message
	err := model.GenerateStream(context.Background(), []modelpkg.Message{
		{Role: "user", Content: "hi"},
	}, func(res modelpkg.StreamResult) error {
		if res.Final {
			final = res.Message
			return nil
		}
		if len(res.Message.ToolCalls) > 0 {
			toolUpdates = append(toolUpdates, res.Message.ToolCalls...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream error: %v", err)
	}
	if len(toolUpdates) != 1 {
		t.Fatalf("tool updates: %+v", toolUpdates)
	}
	if toolUpdates[0].Name != "lookup_weather" {
		t.Fatalf("tool update call: %+v", toolUpdates[0])
	}
	if city := toolUpdates[0].Arguments["city"]; city != "Boston" {
		t.Fatalf("tool call args: %+v", toolUpdates[0].Arguments)
	}
	if len(final.ToolCalls) != 1 || final.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("final tool calls: %+v", final.ToolCalls)
	}
}

func streamHandler(t *testing.T, payloads []string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode stream request: %v", err)
		}
		if !req.Stream {
			t.Fatalf("expected stream flag")
		}
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range payloads {
			if _, err := w.Write([]byte("data: " + chunk + "\n\n")); err != nil {
				t.Fatalf("write chunk: %v", err)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
			t.Fatalf("write done: %v", err)
		}
		if flusher != nil {
			flusher.Flush()
		}
	})
}

func newTestModel(t *testing.T, server *httptest.Server) modelpkg.Model {
	t.Helper()
	cfg := modelpkg.ModelConfig{
		Provider: "openai",
		APIKey:   "sk-test",
		Model:    "gpt-4o-mini",
		BaseURL:  server.URL,
	}
	provider := NewProvider(server.Client())
	model, err := provider.NewModel(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewModel error: %v", err)
	}
	return model
}

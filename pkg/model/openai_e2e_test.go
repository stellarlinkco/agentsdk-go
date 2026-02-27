package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIModel_E2E_ToolCallNilArgumentsMarshalsAsEmptyObject(t *testing.T) {
	argsCh := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "decode body", http.StatusBadRequest)
			return
		}
		msgs, _ := payload["messages"].([]any)
		msg0, _ := msgs[0].(map[string]any)
		toolCalls, _ := msg0["tool_calls"].([]any)
		tool0, _ := toolCalls[0].(map[string]any)
		fn, _ := tool0["function"].(map[string]any)
		args, _ := fn["arguments"].(string)
		select {
		case argsCh <- args:
		default:
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_test","object":"chat.completion","created":0,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAI(OpenAIConfig{
		APIKey:     "test",
		BaseURL:    srv.URL + "/v1",
		Model:      "gpt-4o",
		MaxTokens:  16,
		MaxRetries: 1,
	})
	require.NoError(t, err)

	_, err = m.Complete(context.Background(), Request{
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Name: "tool1",
			}},
		}},
	})
	require.NoError(t, err)

	require.Equal(t, "{}", <-argsCh)
}

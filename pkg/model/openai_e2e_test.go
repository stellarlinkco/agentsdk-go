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
		msgsAny, ok := payload["messages"]
		if !ok {
			http.Error(w, "missing messages", http.StatusBadRequest)
			return
		}
		msgs, ok := msgsAny.([]any)
		if !ok || len(msgs) == 0 {
			http.Error(w, "invalid messages", http.StatusBadRequest)
			return
		}
		msg0, ok := msgs[0].(map[string]any)
		if !ok {
			http.Error(w, "invalid message", http.StatusBadRequest)
			return
		}
		toolCallsAny, ok := msg0["tool_calls"]
		if !ok {
			http.Error(w, "missing tool_calls", http.StatusBadRequest)
			return
		}
		toolCalls, ok := toolCallsAny.([]any)
		if !ok || len(toolCalls) == 0 {
			http.Error(w, "invalid tool_calls", http.StatusBadRequest)
			return
		}
		tool0, ok := toolCalls[0].(map[string]any)
		if !ok {
			http.Error(w, "invalid tool_call", http.StatusBadRequest)
			return
		}
		fnAny, ok := tool0["function"]
		if !ok {
			http.Error(w, "missing function", http.StatusBadRequest)
			return
		}
		fn, ok := fnAny.(map[string]any)
		if !ok {
			http.Error(w, "invalid function", http.StatusBadRequest)
			return
		}
		argsAny, ok := fn["arguments"]
		if !ok {
			http.Error(w, "missing arguments", http.StatusBadRequest)
			return
		}
		args, ok := argsAny.(string)
		if !ok {
			http.Error(w, "invalid arguments", http.StatusBadRequest)
			return
		}
		select {
		case argsCh <- args:
		default:
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"id":"chatcmpl_test","object":"chat.completion","created":0,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)); err != nil {
			return
		}
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

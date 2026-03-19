package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesModel_E2E_Complete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/responses" && r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"id":"resp_test","object":"response","created_at":0,"error":{"code":"server_error","message":""},"incomplete_details":{},"instructions":"","metadata":{},"model":"gpt-4o","output":[{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"parallel_tool_calls":false,"temperature":0,"tool_choice":"auto","tools":[],"top_p":1,"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)); err != nil {
			panic(err)
		}
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAIResponses(OpenAIConfig{
		APIKey:     "test",
		BaseURL:    srv.URL + "/v1",
		Model:      "gpt-4o",
		MaxTokens:  16,
		MaxRetries: 1,
	})
	require.NoError(t, err)

	resp, err := m.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "ok", resp.Message.Content)
	require.Equal(t, "completed", resp.StopReason)
	require.Equal(t, 2, resp.Usage.TotalTokens)
}

func TestOpenAIResponsesModel_E2E_Complete_Unauthorized_NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/responses" && r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`)); err != nil {
			panic(err)
		}
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAIResponses(OpenAIConfig{
		APIKey:     "test",
		BaseURL:    srv.URL + "/v1",
		Model:      "gpt-4o",
		MaxTokens:  16,
		MaxRetries: 10,
	})
	require.NoError(t, err)

	_, err = m.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())

	var apiErr *openai.Error
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

func TestOpenAIResponsesModel_E2E_Complete_ServerError_Retries(t *testing.T) {
	var (
		totalCalls  atomic.Int32
		outerStarts atomic.Int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalCalls.Add(1)
		if r.Header.Get("X-Stainless-Retry-Count") == "0" {
			outerStarts.Add(1)
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/responses" && r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}

		if outerStarts.Load() < 2 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte(`{"error":{"message":"server error","type":"server_error","param":null,"code":"server_error"}}`)); err != nil {
				panic(err)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"id":"resp_test","object":"response","created_at":0,"error":{"code":"server_error","message":""},"incomplete_details":{},"instructions":"","metadata":{},"model":"gpt-4o","output":[{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"parallel_tool_calls":false,"temperature":0,"tool_choice":"auto","tools":[],"top_p":1,"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)); err != nil {
			panic(err)
		}
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAIResponses(OpenAIConfig{
		APIKey:     "test",
		BaseURL:    srv.URL + "/v1",
		Model:      "gpt-4o",
		MaxTokens:  16,
		MaxRetries: 1,
	})
	require.NoError(t, err)

	resp, err := m.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "ok", resp.Message.Content)
	require.Equal(t, int32(2), outerStarts.Load())
	require.GreaterOrEqual(t, totalCalls.Load(), int32(2))
}

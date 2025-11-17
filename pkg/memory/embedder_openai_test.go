package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go/v3/option"
)

func TestOpenAIEmbedderBatchesRequests(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		atomic.AddInt32(&calls, 1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode err: %v", err)
		}
		entries := payload["input"].([]any)
		resp := map[string]any{
			"data":   []map[string]any{},
			"model":  "text-embedding-3-small",
			"object": "list",
			"usage":  map[string]any{"prompt_tokens": 0, "total_tokens": 0},
		}
		for i := range entries {
			resp["data"] = append(resp["data"].([]map[string]any), map[string]any{
				"embedding": []float64{float64(i + 1)},
				"index":     i,
				"object":    "embedding",
			})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	emb, err := NewOpenAIEmbedder("test", "text-embedding-3-small",
		WithOpenAIEmbedderBatchSize(2),
		WithOpenAIEmbedderOptions(option.WithBaseURL(server.URL)))
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder error: %v", err)
	}
	vectors, err := emb.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed err: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestOpenAIEmbedderDetectsMismatchedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"data":   []map[string]any{},
			"model":  "text-embedding-3-small",
			"object": "list",
			"usage":  map[string]any{"prompt_tokens": 0, "total_tokens": 0},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	emb, err := NewOpenAIEmbedder("test", "text-embedding-3-small",
		WithOpenAIEmbedderOptions(option.WithBaseURL(server.URL)))
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder error: %v", err)
	}
	if _, err := emb.Embed(context.Background(), []string{"only"}); err == nil {
		t.Fatalf("expected mismatch error")
	}
}

package middleware

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/memory"
	"github.com/cexll/agentsdk-go/pkg/model"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

func TestSemanticMemoryMiddlewareInjectsPrompt(t *testing.T) {
	sem := memory.NewSemanticMemory(memory.SemanticMemoryConfig{Embedder: &memory.StaticEmbedder{Vector: []float64{1}}})
	if err := sem.Save(context.Background(), "memo", memory.SaveOptions{Metadata: map[string]any{"thread_id": "thread-1"}}); err != nil {
		t.Fatalf("save error: %v", err)
	}
	mw := NewSemanticMemoryMiddleware(sem)
	req := &ModelRequest{
		Messages:  []model.Message{{Role: "user", Content: "memo"}},
		Metadata:  map[string]any{"thread_id": "thread-1"},
		SessionID: "thread-1",
	}
	called := false
	next := func(ctx context.Context, req *ModelRequest) (*ModelResponse, error) {
		called = true
		if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
			t.Fatalf("expected system prompt at head: %#v", req.Messages)
		}
		entries := parsePromptJSON(t, req.Messages[0].Content)
		if len(entries) == 0 || entries[0]["content"] != "memo" {
			t.Fatalf("unexpected entries: %#v", entries)
		}
		return &ModelResponse{Message: model.Message{}}, nil
	}
	if _, err := mw.ExecuteModelCall(context.Background(), req, next); err != nil {
		t.Fatalf("ExecuteModelCall error: %v", err)
	}
	if !called {
		t.Fatalf("next not invoked")
	}
}

func TestSemanticMemoryMiddlewareToolArguments(t *testing.T) {
	sem := memory.NewSemanticMemory(memory.SemanticMemoryConfig{Embedder: &memory.StaticEmbedder{Vector: []float64{1}}})
	mw := NewSemanticMemoryMiddleware(sem)
	req := &ToolCallRequest{
		Name:      toolbuiltin.SemanticMemorySaveToolName,
		Arguments: map[string]any{"text": "content"},
		SessionID: "thread-2",
	}
	next := func(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
		ns := asString(req.Arguments[semanticNamespaceMetadataKey])
		if ns == "" {
			t.Fatalf("namespace not injected: %#v", req.Arguments)
		}
		if asString(req.Arguments["session_id"]) != "thread-2" {
			t.Fatalf("session id missing")
		}
		return &ToolCallResponse{}, nil
	}
	if _, err := mw.ExecuteToolCall(context.Background(), req, next); err != nil {
		t.Fatalf("ExecuteToolCall error: %v", err)
	}
}

func TestSemanticMemoryMiddlewareRecallToolDefaultsTopK(t *testing.T) {
	sem := memory.NewSemanticMemory(memory.SemanticMemoryConfig{Embedder: &memory.StaticEmbedder{Vector: []float64{1}}})
	mw := NewSemanticMemoryMiddleware(sem)
	req := &ToolCallRequest{
		Name:      toolbuiltin.SemanticMemoryRecallToolName,
		Arguments: map[string]any{"query": "x"},
		SessionID: "thread-3",
	}
	next := func(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
		if _, ok := req.Arguments["top_k"]; !ok {
			t.Fatalf("expected top_k default")
		}
		return &ToolCallResponse{}, nil
	}
	if _, err := mw.ExecuteToolCall(context.Background(), req, next); err != nil {
		t.Fatalf("ExecuteToolCall error: %v", err)
	}
}

func TestSemanticMemoryMiddlewareSkipsWhenDisabled(t *testing.T) {
	mw := NewSemanticMemoryMiddleware(nil)
	req := &ModelRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}}
	called := false
	next := func(ctx context.Context, req *ModelRequest) (*ModelResponse, error) {
		called = true
		return &ModelResponse{}, nil
	}
	if _, err := mw.ExecuteModelCall(context.Background(), req, next); err != nil {
		t.Fatalf("ExecuteModelCall error: %v", err)
	}
	if !called {
		t.Fatalf("next not invoked")
	}
}

func parsePromptJSON(t *testing.T, content string) []map[string]any {
	start := strings.Index(content, "```json")
	if start == -1 {
		t.Fatalf("json block not found: %s", content)
	}
	data := content[start+7:]
	end := strings.Index(data, "```")
	if end == -1 {
		t.Fatalf("json terminator missing: %s", content)
	}
	data = data[:end]
	var entries []map[string]any
	if err := json.Unmarshal([]byte(data), &entries); err != nil {
		t.Fatalf("unmarshal prompt: %v", err)
	}
	return entries
}

package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"testing"
)

func TestSemanticMemoryNamespaceResolution(t *testing.T) {
	sem := NewSemanticMemory(SemanticMemoryConfig{Embedder: &StaticEmbedder{Vector: []float64{1}}, DefaultNamespace: "global"})
	meta := map[string]any{
		"user_id":     "alice",
		"thread_id":   "thread-1",
		"resource_id": "res",
	}
	ns := sem.ResolveNamespace("", meta, "session-1")
	if ns != "users/alice/threads/thread-1/resources/res" {
		t.Fatalf("unexpected namespace: %s", ns)
	}
}

func TestSemanticMemorySaveAndRecall(t *testing.T) {
	embedder := &hashEmbedder{}
	sem := NewSemanticMemory(SemanticMemoryConfig{Embedder: embedder, DefaultNamespace: "global"})
	ctx := context.Background()
	meta := map[string]any{"thread_id": "t1"}
	if err := sem.Save(ctx, "task progress", SaveOptions{SessionID: "sess", Metadata: meta}); err != nil {
		t.Fatalf("save error: %v", err)
	}
	res, err := sem.Recall(ctx, "task progress", RecallOptions{SessionID: "sess", Metadata: meta})
	if err != nil {
		t.Fatalf("recall error: %v", err)
	}
	if len(res) != 1 || res[0].Content != "task progress" {
		t.Fatalf("unexpected recall result: %#v", res)
	}
}

func TestSemanticMemoryLongConversationConsistency(t *testing.T) {
	embedder := &hashEmbedder{}
	sem := NewSemanticMemory(SemanticMemoryConfig{Embedder: embedder, TopK: 1, DefaultNamespace: "global"})
	ctx := context.Background()
	for i := 0; i < 150; i++ {
		text := fmt.Sprintf("memory-%03d", i)
		if err := sem.Save(ctx, text, SaveOptions{SessionID: "sess"}); err != nil {
			t.Fatalf("save iteration %d: %v", i, err)
		}
	}
	res, err := sem.Recall(ctx, "memory-149", RecallOptions{SessionID: "sess", TopK: 1})
	if err != nil {
		t.Fatalf("recall error: %v", err)
	}
	if len(res) != 1 || res[0].Content != "memory-149" {
		t.Fatalf("expected most recent memory, got %#v", res)
	}
}

func TestSemanticMemoryNamespaceFallbacks(t *testing.T) {
	sem := NewSemanticMemory(SemanticMemoryConfig{Embedder: &StaticEmbedder{Vector: []float64{1}}, DefaultNamespace: "global"})
	if ns := sem.ResolveNamespace("  users/../Alice  ", nil, ""); ns != "users/Alice" {
		t.Fatalf("sanitize failed: %s", ns)
	}
	if ns := sem.ResolveNamespace("", nil, "sessionX"); ns != "sessions/sessionX" {
		t.Fatalf("session fallback incorrect: %s", ns)
	}
}

func TestInMemorySemanticMemoryErrors(t *testing.T) {
	store := NewInMemorySemanticMemory(nil)
	if err := store.Store(context.Background(), "", "text", nil); err == nil {
		t.Fatalf("expected namespace error")
	}
	store = NewInMemorySemanticMemory(&StaticEmbedder{Vector: []float64{1}})
	if _, err := store.Recall(context.Background(), "", "q", 1); err == nil {
		t.Fatalf("expected namespace error on recall")
	}
}

func TestSemanticMemoryDelete(t *testing.T) {
	embedder := &StaticEmbedder{Vector: []float64{1}}
	sem := NewSemanticMemory(SemanticMemoryConfig{Embedder: embedder, DefaultNamespace: "threads/demo"})
	ctx := context.Background()
	if err := sem.Save(ctx, "obsolete", SaveOptions{SessionID: "demo"}); err != nil {
		t.Fatalf("save error: %v", err)
	}
	if err := sem.Delete(ctx, DeleteOptions{SessionID: "demo"}); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	res, err := sem.Recall(ctx, "obsolete", RecallOptions{SessionID: "demo"})
	if err != nil {
		t.Fatalf("recall error: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected empty recall after delete")
	}
}

func TestSemanticMemoryDisabled(t *testing.T) {
	sem := NewSemanticMemory(SemanticMemoryConfig{})
	if err := sem.Save(context.Background(), "text", SaveOptions{}); err == nil {
		t.Fatalf("expected disabled error")
	}
}

type hashEmbedder struct{}

func (hashEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for i, text := range texts {
		h := fnv.New32a()
		_, _ = h.Write([]byte(text))
		angle := float64(h.Sum32()%3600) / 10
		rad := angle * math.Pi / 180
		vectors[i] = []float64{math.Cos(rad), math.Sin(rad)}
	}
	return vectors, nil
}

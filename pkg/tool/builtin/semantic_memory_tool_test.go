package toolbuiltin

import (
	"context"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/memory"
)

func TestSaveAndRecallMemoryTools(t *testing.T) {
	sem := memory.NewSemanticMemory(memory.SemanticMemoryConfig{Embedder: &memory.StaticEmbedder{Vector: []float64{1}}})

	saveTool := NewSaveMemoryTool(sem)
	recallTool := NewRecallMemoryTool(sem)

	saveParams := map[string]any{
		"text":       "lorem ipsum",
		"namespace":  "demo",
		"session_id": "sess",
	}
	if res, err := saveTool.Execute(context.Background(), saveParams); err != nil || !res.Success {
		t.Fatalf("save execute err=%v res=%v", err, res)
	}

	recallParams := map[string]any{
		"query":      "lorem",
		"namespace":  "demo",
		"session_id": "sess",
		"top_k":      1,
	}
	res, err := recallTool.Execute(context.Background(), recallParams)
	if err != nil {
		t.Fatalf("recall execute err=%v", err)
	}
	if !res.Success {
		t.Fatalf("recall failed: %#v", res)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data: %#v", res.Data)
	}
	mems, ok := data["memories"].([]memory.Memory)
	if !ok || len(mems) == 0 || mems[0].Content != "lorem ipsum" {
		t.Fatalf("unexpected recall data: %#v", res.Data)
	}
}

func TestSemanticToolsGracefulDisable(t *testing.T) {
	save := NewSaveMemoryTool(nil)
	recall := NewRecallMemoryTool(nil)
	if res, err := save.Execute(context.Background(), map[string]any{"text": "x"}); err != nil || res.Success {
		t.Fatalf("expected graceful disable, got res=%#v err=%v", res, err)
	}
	if res, err := recall.Execute(context.Background(), map[string]any{"query": "x"}); err != nil || res.Success {
		t.Fatalf("expected graceful disable, got res=%#v err=%v", res, err)
	}
}

func TestSaveMemoryToolValidation(t *testing.T) {
	sem := memory.NewSemanticMemory(memory.SemanticMemoryConfig{Embedder: &memory.StaticEmbedder{Vector: []float64{1}}})
	tool := NewSaveMemoryTool(sem)
	if _, err := tool.Execute(context.Background(), map[string]any{"namespace": "demo"}); err == nil {
		t.Fatalf("expected validation error")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"text": "ok", "metadata": "bad"}); err == nil {
		t.Fatalf("expected metadata error")
	}
}

func TestRecallMemoryToolValidation(t *testing.T) {
	sem := memory.NewSemanticMemory(memory.SemanticMemoryConfig{Embedder: &memory.StaticEmbedder{Vector: []float64{1}}})
	tool := NewRecallMemoryTool(sem)
	if _, err := tool.Execute(context.Background(), map[string]any{"namespace": "demo"}); err == nil {
		t.Fatalf("expected query validation error")
	}
	res, err := tool.Execute(context.Background(), map[string]any{"query": "q", "top_k": "3"})
	if err != nil || !res.Success {
		t.Fatalf("expected successful recall with string top_k, res=%#v err=%v", res, err)
	}
}

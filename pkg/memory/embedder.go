package memory

import "context"

// Embedder transforms raw text into dense vectors compatible with SemanticMemoryStore.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// StaticEmbedder is a helper for tests that returns deterministic vectors.
type StaticEmbedder struct{ Vector []float64 }

// Embed returns identical vectors for each input, useful for deterministic tests.
func (e *StaticEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	_ = ctx
	vectors := make([][]float64, len(texts))
	for i := range texts {
		vectors[i] = append([]float64(nil), e.Vector...)
	}
	return vectors, nil
}

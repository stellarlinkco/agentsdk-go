package memory

import (
	"context"
	"time"
)

// AgentMemoryStore defines persistence for agent-level persona text.
type AgentMemoryStore interface {
	Read(ctx context.Context) (string, error)
	Write(ctx context.Context, content string) error
	Exists(ctx context.Context) bool
}

// WorkingMemoryStore stores scoped, short-lived context state.
type WorkingMemoryStore interface {
	Get(ctx context.Context, scope Scope) (*WorkingMemory, error)
	Set(ctx context.Context, scope Scope, memory *WorkingMemory) error
	Delete(ctx context.Context, scope Scope) error
	List(ctx context.Context) ([]Scope, error)
}

// SemanticMemoryStore stores and recalls semantic memories via embeddings.
type SemanticMemoryStore interface {
	Store(ctx context.Context, namespace, text string, metadata map[string]any) error
	Recall(ctx context.Context, namespace, query string, topK int) ([]Memory, error)
	Delete(ctx context.Context, namespace string) error
}

// Scope identifies a working-memory partition.
type Scope struct {
	ThreadID   string
	ResourceID string
}

// WorkingMemory captures short-term scoped state alongside optional schema metadata.
type WorkingMemory struct {
	Data      map[string]any
	Schema    *JSONSchema
	CreatedAt time.Time
	UpdatedAt time.Time
	TTL       time.Duration
}

// Memory represents a semantic memory record with provenance metadata.
type Memory struct {
	ID         string
	Content    string
	Embedding  []float64
	Metadata   map[string]any
	Score      float64
	Namespace  string
	Provenance *Provenance
}

// Provenance tracks where a semantic memory originated.
type Provenance struct {
	Source    string
	Timestamp time.Time
	Agent     string
}

// JSONSchema is a lightweight schema descriptor for validating working-memory payloads.
type JSONSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required"`
}

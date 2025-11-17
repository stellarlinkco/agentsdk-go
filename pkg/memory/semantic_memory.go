package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrSemanticMemoryDisabled signals the semantic memory pipeline is not configured.
var ErrSemanticMemoryDisabled = errors.New("semantic memory disabled")

// NamespaceResolver derives a namespace from metadata/session identifiers.
type NamespaceResolver func(metadata map[string]any, sessionID string) string

// SemanticMemoryConfig wires the storage backend and embedder.
type SemanticMemoryConfig struct {
	Store             SemanticMemoryStore
	Embedder          Embedder
	NamespaceResolver NamespaceResolver
	TopK              int
	DefaultNamespace  string
}

// SaveOptions captures contextual information when persisting semantic memories.
type SaveOptions struct {
	Namespace string
	Metadata  map[string]any
	SessionID string
}

// RecallOptions configures semantic recall queries.
type RecallOptions struct {
	Namespace string
	Metadata  map[string]any
	SessionID string
	TopK      int
}

// DeleteOptions controls namespace deletion.
type DeleteOptions struct {
	Namespace string
	Metadata  map[string]any
	SessionID string
}

const (
	defaultSemanticTopK = 3
	defaultNamespace    = "global"
)

// SemanticMemory coordinates the store, embedder awareness, and namespace resolution.
type SemanticMemory struct {
	store     SemanticMemoryStore
	embedder  Embedder
	resolver  NamespaceResolver
	defaultNS string
	topK      int
}

// NewSemanticMemory creates a semantic memory coordinator.
// If store is nil and an embedder is provided, an in-memory store is created for convenience.
func NewSemanticMemory(cfg SemanticMemoryConfig) *SemanticMemory {
	if cfg.TopK <= 0 {
		cfg.TopK = defaultSemanticTopK
	}
	if strings.TrimSpace(cfg.DefaultNamespace) == "" {
		cfg.DefaultNamespace = defaultNamespace
	}
	if cfg.NamespaceResolver == nil {
		cfg.NamespaceResolver = defaultNamespaceResolver
	}
	if cfg.Store == nil && cfg.Embedder != nil {
		cfg.Store = NewInMemorySemanticMemory(cfg.Embedder)
	}
	if mem, ok := cfg.Store.(*InMemorySemanticMemory); ok && mem != nil && mem.embedder == nil && cfg.Embedder != nil {
		mem.embedder = cfg.Embedder
	}
	return &SemanticMemory{
		store:     cfg.Store,
		embedder:  cfg.Embedder,
		resolver:  cfg.NamespaceResolver,
		defaultNS: cfg.DefaultNamespace,
		topK:      cfg.TopK,
	}
}

// Enabled reports whether semantic memory can operate.
func (s *SemanticMemory) Enabled() bool {
	return s != nil && s.store != nil && s.embedder != nil
}

// TopK returns the configured default recall depth.
func (s *SemanticMemory) TopK() int {
	if s == nil || s.topK <= 0 {
		return defaultSemanticTopK
	}
	return s.topK
}

// Save persists text into semantic memory using derived namespaces.
func (s *SemanticMemory) Save(ctx context.Context, text string, opts SaveOptions) error {
	if !s.Enabled() {
		return ErrSemanticMemoryDisabled
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("text is required")
	}
	ns := s.ResolveNamespace(opts.Namespace, opts.Metadata, opts.SessionID)
	return s.store.Store(ctx, ns, text, cloneMetadata(opts.Metadata))
}

// Recall queries semantic memory for related entries.
func (s *SemanticMemory) Recall(ctx context.Context, query string, opts RecallOptions) ([]Memory, error) {
	if !s.Enabled() {
		return nil, ErrSemanticMemoryDisabled
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is required")
	}
	ns := s.ResolveNamespace(opts.Namespace, opts.Metadata, opts.SessionID)
	topK := opts.TopK
	if topK <= 0 {
		topK = s.TopK()
	}
	return s.store.Recall(ctx, ns, query, topK)
}

// Delete removes an entire namespace from the store.
func (s *SemanticMemory) Delete(ctx context.Context, opts DeleteOptions) error {
	if !s.Enabled() {
		return ErrSemanticMemoryDisabled
	}
	ns := s.ResolveNamespace(opts.Namespace, opts.Metadata, opts.SessionID)
	return s.store.Delete(ctx, ns)
}

// ResolveNamespace derives the namespace used for semantic operations.
func (s *SemanticMemory) ResolveNamespace(candidate string, metadata map[string]any, sessionID string) string {
	if ns := sanitizeNamespace(candidate); ns != "" {
		return ns
	}
	if s == nil {
		return defaultNamespace
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = defaultNamespaceResolver
	}
	derived := resolver(metadata, sessionID)
	if ns := sanitizeNamespace(derived); ns != "" {
		return ns
	}
	return s.defaultNS
}

func defaultNamespaceResolver(metadata map[string]any, sessionID string) string {
	if metadata != nil {
		if ns := asString(metadata["namespace"]); ns != "" {
			return ns
		}
		if ns := asString(metadata["memory_namespace"]); ns != "" {
			return ns
		}
		user := asString(metadata["user_id"])
		thread := asString(metadata["thread_id"])
		resource := asString(metadata["resource_id"])
		if thread != "" {
			parts := []string{}
			if user != "" {
				parts = append(parts, "users", user)
			}
			parts = append(parts, "threads", thread)
			if resource != "" {
				parts = append(parts, "resources", resource)
			}
			return strings.Join(parts, "/")
		}
		if resource != "" {
			parts := []string{}
			if user != "" {
				parts = append(parts, "users", user)
			}
			parts = append(parts, "resources", resource)
			return strings.Join(parts, "/")
		}
		if user != "" {
			return strings.Join([]string{"users", user}, "/")
		}
	}
	if strings.TrimSpace(sessionID) != "" {
		return "sessions/" + sessionID
	}
	return ""
}

func sanitizeNamespace(ns string) string {
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return ""
	}
	parts := strings.Split(ns, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		seg := sanitizeSegment(part)
		if seg != "" {
			cleaned = append(cleaned, seg)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	return strings.Join(cleaned, "/")
}

func cloneMetadata(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	dup := make(map[string]any, len(meta))
	for k, v := range meta {
		dup[k] = v
	}
	return dup
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

// InMemorySemanticMemory offers a minimal vector-store backed by RAM for demos/tests.
type InMemorySemanticMemory struct {
	embedder Embedder
	mu       sync.RWMutex
	memories map[string][]Memory // namespace -> memories
}

// NewInMemorySemanticMemory constructs an in-memory semantic memory using the provided embedder.
func NewInMemorySemanticMemory(embedder Embedder) *InMemorySemanticMemory {
	return &InMemorySemanticMemory{
		embedder: embedder,
		memories: make(map[string][]Memory),
	}
}

// Store embeds the text then stores it under namespace.
func (s *InMemorySemanticMemory) Store(ctx context.Context, namespace, text string, metadata map[string]any) error {
	if s == nil {
		return errors.New("semantic memory is nil")
	}
	if s.embedder == nil {
		return errors.New("embedder is nil")
	}
	if strings.TrimSpace(namespace) == "" {
		return errors.New("namespace is required")
	}

	vectors, err := s.embedder.Embed(ctx, []string{text})
	if err != nil {
		return err
	}
	if len(vectors) == 0 {
		return errors.New("embedder returned empty vector")
	}

	var metadataCopy map[string]any
	if metadata != nil {
		metadataCopy = make(map[string]any, len(metadata))
		for k, v := range metadata {
			metadataCopy[k] = v
		}
	}

	vector := append([]float64(nil), vectors[0]...)
	mem := Memory{
		ID:        generateID(),
		Content:   text,
		Embedding: vector,
		Metadata:  metadataCopy,
		Namespace: namespace,
		Provenance: &Provenance{
			Source:    "in-memory",
			Timestamp: time.Now().UTC(),
		},
	}

	s.mu.Lock()
	s.memories[namespace] = append(s.memories[namespace], mem)
	s.mu.Unlock()
	return nil
}

// Recall performs cosine similarity search in-memory.
func (s *InMemorySemanticMemory) Recall(ctx context.Context, namespace, query string, topK int) ([]Memory, error) {
	_ = ctx
	if s == nil {
		return nil, errors.New("semantic memory is nil")
	}
	if s.embedder == nil {
		return nil, errors.New("embedder is nil")
	}
	if strings.TrimSpace(namespace) == "" {
		return nil, errors.New("namespace is required")
	}

	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, errors.New("embedder returned empty vector")
	}
	queryVec := vectors[0]

	s.mu.RLock()
	candidates := append([]Memory(nil), s.memories[namespace]...) // copy
	s.mu.RUnlock()

	for i := range candidates {
		candidates[i].Score = cosineSimilarity(queryVec, candidates[i].Embedding)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if topK > 0 && len(candidates) > topK {
		candidates = candidates[:topK]
	}
	return candidates, nil
}

// Delete removes all memories under a namespace.
func (s *InMemorySemanticMemory) Delete(ctx context.Context, namespace string) error {
	_ = ctx
	if s == nil {
		return errors.New("semantic memory is nil")
	}
	if strings.TrimSpace(namespace) == "" {
		return errors.New("namespace is required")
	}

	s.mu.Lock()
	delete(s.memories, namespace)
	s.mu.Unlock()
	return nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func generateID() string {
	return fmt.Sprintf("mem_%d", time.Now().UTC().UnixNano())
}

package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/memory"
	"github.com/cexll/agentsdk-go/pkg/model"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

const (
	semanticThreadMetadataKey    = "thread_id"
	semanticResourceMetadataKey  = "resource_id"
	semanticNamespaceMetadataKey = "memory_namespace"
)

// SemanticMemoryMiddleware injects semantic recall results into the system prompt and
// normalizes tool arguments for semantic memory tools.
type SemanticMemoryMiddleware struct {
	*BaseMiddleware
	mem       *memory.SemanticMemory
	namespace string
}

// SemanticMemoryMiddlewareOption customizes middleware behaviour.
type SemanticMemoryMiddlewareOption func(*SemanticMemoryMiddleware)

// WithSemanticNamespace overrides the namespace used for recall injection.
func WithSemanticNamespace(ns string) SemanticMemoryMiddlewareOption {
	return func(m *SemanticMemoryMiddleware) {
		m.namespace = strings.TrimSpace(ns)
	}
}

// NewSemanticMemoryMiddleware constructs the middleware.
func NewSemanticMemoryMiddleware(mem *memory.SemanticMemory, opts ...SemanticMemoryMiddlewareOption) *SemanticMemoryMiddleware {
	mw := &SemanticMemoryMiddleware{
		BaseMiddleware: NewBaseMiddleware("semantic_memory", 60),
		mem:            mem,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(mw)
		}
	}
	return mw
}

// ExecuteModelCall injects fetched memories ahead of the conversation history.
func (m *SemanticMemoryMiddleware) ExecuteModelCall(ctx context.Context, req *ModelRequest, next ModelCallFunc) (*ModelResponse, error) {
	if next == nil {
		return nil, ErrMissingNext
	}
	if req == nil || m.mem == nil || !m.mem.Enabled() {
		return next(ctx, req)
	}
	if len(req.Messages) == 0 {
		return next(ctx, req)
	}
	query := strings.TrimSpace(req.Messages[len(req.Messages)-1].Content)
	if query == "" {
		return next(ctx, req)
	}

	opts := memory.RecallOptions{
		Namespace: m.namespace,
		Metadata:  cloneMetadata(req.Metadata),
		SessionID: req.SessionID,
	}
	memories, err := m.mem.Recall(ctx, query, opts)
	if err != nil {
		if err != memory.ErrSemanticMemoryDisabled {
			fmt.Printf("semantic middleware recall failed: %v\n", err)
		}
		return next(ctx, req)
	}
	if len(memories) == 0 {
		return next(ctx, req)
	}

	prompt := buildSemanticPrompt(memories)
	systemMsg := model.Message{Role: "system", Content: prompt}
	req.Messages = append([]model.Message{systemMsg}, req.Messages...)
	return next(ctx, req)
}

// ExecuteToolCall decorates semantic-memory tool arguments with derived defaults.
func (m *SemanticMemoryMiddleware) ExecuteToolCall(ctx context.Context, req *ToolCallRequest, next ToolCallFunc) (*ToolCallResponse, error) {
	if next == nil {
		return nil, ErrMissingNext
	}
	if req == nil || m.mem == nil || !m.mem.Enabled() {
		return next(ctx, req)
	}

	scope, ok := deriveScope(req.Metadata, req.SessionID)
	if ok {
		ensureArgs(req)
		if asString(req.Arguments[semanticThreadMetadataKey]) == "" {
			req.Arguments[semanticThreadMetadataKey] = scope.ThreadID
		}
		if scope.ResourceID != "" && asString(req.Arguments[semanticResourceMetadataKey]) == "" {
			req.Arguments[semanticResourceMetadataKey] = scope.ResourceID
		}
	}
	ensureArgs(req)
	if asString(req.Arguments["session_id"]) == "" && strings.TrimSpace(req.SessionID) != "" {
		req.Arguments["session_id"] = req.SessionID
	}

	if req.Name != toolbuiltin.SemanticMemorySaveToolName && req.Name != toolbuiltin.SemanticMemoryRecallToolName {
		return next(ctx, req)
	}

	merged := mergeSemanticMetadata(req.Metadata, req.Arguments)
	requestedNS := asString(req.Arguments[semanticNamespaceMetadataKey])
	resolved := m.mem.ResolveNamespace(requestedNS, merged, req.SessionID)
	if requestedNS == "" {
		req.Arguments[semanticNamespaceMetadataKey] = resolved
	}
	if req.Name == toolbuiltin.SemanticMemoryRecallToolName {
		if _, exists := req.Arguments["top_k"]; !exists {
			req.Arguments["top_k"] = m.mem.TopK()
		}
	}
	return next(ctx, req)
}

func buildSemanticPrompt(memories []memory.Memory) string {
	entries := make([]map[string]any, 0, len(memories))
	for _, mem := range memories {
		entry := map[string]any{
			"id":        mem.ID,
			"content":   mem.Content,
			"score":     mem.Score,
			"namespace": mem.Namespace,
		}
		if mem.Metadata != nil {
			entry["metadata"] = mem.Metadata
		}
		if mem.Provenance != nil {
			entry["provenance"] = map[string]any{
				"source":    mem.Provenance.Source,
				"timestamp": mem.Provenance.Timestamp,
				"agent":     mem.Provenance.Agent,
			}
		}
		entries = append(entries, entry)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "# 语义记忆\n\n无法序列化召回结果"
	}
	return fmt.Sprintf("# 语义记忆召回结果\n\n```json\n%s\n```", string(data))
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

func mergeSemanticMetadata(meta map[string]any, args map[string]any) map[string]any {
	merged := cloneMetadata(meta)
	if merged == nil {
		merged = map[string]any{}
	}
	if args == nil {
		return merged
	}
	for _, key := range []string{semanticThreadMetadataKey, semanticResourceMetadataKey, semanticNamespaceMetadataKey, "user_id"} {
		if val, ok := args[key]; ok {
			merged[key] = val
		}
	}
	return merged
}

func ensureArgs(req *ToolCallRequest) {
	if req.Arguments == nil {
		req.Arguments = map[string]any{}
	}
}

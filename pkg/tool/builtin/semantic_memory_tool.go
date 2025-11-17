package toolbuiltin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/memory"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

const (
	// SemanticMemorySaveToolName saves long-term memory entries.
	SemanticMemorySaveToolName = "save_memory"
	// SemanticMemoryRecallToolName retrieves semantic memories.
	SemanticMemoryRecallToolName = "recall_memory"
)

// SaveMemoryTool lets the model persist semantic knowledge.
type SaveMemoryTool struct {
	mem *memory.SemanticMemory
}

// RecallMemoryTool fetches semantic context.
type RecallMemoryTool struct {
	mem *memory.SemanticMemory
}

// NewSaveMemoryTool constructs the save tool.
func NewSaveMemoryTool(mem *memory.SemanticMemory) *SaveMemoryTool {
	return &SaveMemoryTool{mem: mem}
}

// NewRecallMemoryTool constructs the recall tool.
func NewRecallMemoryTool(mem *memory.SemanticMemory) *RecallMemoryTool {
	return &RecallMemoryTool{mem: mem}
}

func (t *SaveMemoryTool) Name() string { return SemanticMemorySaveToolName }

func (t *SaveMemoryTool) Description() string {
	return "保存重要事实或用户偏好，以便在未来的对话中自动召回。"
}

func (t *SaveMemoryTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]any{
			"namespace": map[string]any{
				"type":        "string",
				"description": "可选命名空间，默认为当前线程/资源。",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "要保存的记忆内容。",
			},
			"metadata": map[string]any{
				"type":        "object",
				"description": "可选 JSON 元数据，将与记忆一起存储。",
			},
		},
		Required: []string{"text"},
	}
}

func (t *SaveMemoryTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if t == nil || t.mem == nil {
		return &tool.ToolResult{Success: false, Output: "语义记忆未启用。"}, nil
	}
	text, err := requireString(params, "text")
	if err != nil {
		return nil, err
	}
	namespace := optionalString(params, "namespace")
	session := optionalString(params, "session_id")
	meta, err := metadataParam(params)
	if err != nil {
		return nil, err
	}
	opts := memory.SaveOptions{
		Namespace: namespace,
		Metadata:  meta,
		SessionID: session,
	}
	if err := t.mem.Save(ctx, text, opts); err != nil {
		if errors.Is(err, memory.ErrSemanticMemoryDisabled) {
			return &tool.ToolResult{Success: false, Output: "语义记忆未启用，跳过保存。"}, nil
		}
		return nil, err
	}
	return &tool.ToolResult{
		Success: true,
		Output:  fmt.Sprintf("已保存记忆（namespace=%s）", t.mem.ResolveNamespace(namespace, meta, session)),
	}, nil
}

func (t *RecallMemoryTool) Name() string { return SemanticMemoryRecallToolName }

func (t *RecallMemoryTool) Description() string {
	return "根据自然语言查询检索最相关的语义记忆。"
}

func (t *RecallMemoryTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]any{
			"namespace": map[string]any{
				"type":        "string",
				"description": "可选命名空间，默认为当前线程/资源。",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "用于检索的自然语言查询。",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "返回的最大条目数量，默认为语义记忆配置。",
			},
		},
		Required: []string{"query"},
	}
}

func (t *RecallMemoryTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if t == nil || t.mem == nil {
		return &tool.ToolResult{Success: false, Output: "语义记忆未启用。"}, nil
	}
	query, err := requireString(params, "query")
	if err != nil {
		return nil, err
	}
	namespace := optionalString(params, "namespace")
	session := optionalString(params, "session_id")
	topK := parseTopK(params)
	meta, err := metadataParam(params)
	if err != nil {
		return nil, err
	}
	opts := memory.RecallOptions{
		Namespace: namespace,
		SessionID: session,
		Metadata:  meta,
		TopK:      topK,
	}
	memories, err := t.mem.Recall(ctx, query, opts)
	if err != nil {
		if errors.Is(err, memory.ErrSemanticMemoryDisabled) {
			return &tool.ToolResult{Success: false, Output: "语义记忆未启用，无法检索。"}, nil
		}
		return nil, err
	}
	output := renderRecallOutput(memories)
	return &tool.ToolResult{
		Success: true,
		Output:  output,
		Data:    map[string]any{"memories": copyMemories(memories)},
	}, nil
}

func requireString(params map[string]interface{}, key string) (string, error) {
	value := optionalString(params, key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalString(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func metadataParam(params map[string]interface{}) (map[string]any, error) {
	if params == nil {
		return nil, nil
	}
	raw, ok := params["metadata"]
	if !ok || raw == nil {
		return nil, nil
	}
	obj, ok := raw.(map[string]any)
	if ok {
		return cloneMap(obj), nil
	}
	return nil, errors.New("metadata must be an object")
}

func parseTopK(params map[string]interface{}) int {
	if params == nil {
		return 0
	}
	raw, ok := params["top_k"]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(math.Round(v))
	case string:
		value := strings.TrimSpace(v)
		if value == "" {
			return 0
		}
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return 0
}

func renderRecallOutput(memories []memory.Memory) string {
	if len(memories) == 0 {
		return "未找到匹配的语义记忆。"
	}
	payload := make([]map[string]any, 0, len(memories))
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
		payload = append(payload, entry)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "语义记忆召回成功。"
	}
	return fmt.Sprintf("召回 %d 条记忆:\n%s", len(memories), string(data))
}

func copyMemories(src []memory.Memory) []memory.Memory {
	dup := make([]memory.Memory, len(src))
	for i, mem := range src {
		dup[i] = mem
		if mem.Metadata != nil {
			dup[i].Metadata = cloneMap(mem.Metadata)
		}
		if len(mem.Embedding) > 0 {
			dup[i].Embedding = append([]float64(nil), mem.Embedding...)
		}
	}
	return dup
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

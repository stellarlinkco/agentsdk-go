package toolbuiltin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const killTaskDescription = "Terminate a running async bash task."

var killTaskSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"task_id": map[string]interface{}{
			"type":        "string",
			"description": "ID of the async task to terminate.",
		},
	},
	Required: []string{"task_id"},
}

// KillTaskTool terminates async bash tasks.
type KillTaskTool struct{}

func NewKillTaskTool() *KillTaskTool { return &KillTaskTool{} }

func (k *KillTaskTool) Name() string { return "KillTask" }

func (k *KillTaskTool) Description() string { return killTaskDescription }

func (k *KillTaskTool) Schema() *tool.JSONSchema { return killTaskSchema }

func (k *KillTaskTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	id, err := parseKillTaskID(params)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := DefaultAsyncTaskManager().Kill(id); err != nil {
		payload := map[string]interface{}{
			"task_id": id,
			"status":  "error",
			"error":   err.Error(),
		}
		out, _ := json.Marshal(payload)
		return &tool.ToolResult{Success: false, Output: string(out), Data: payload}, err
	}
	payload := map[string]interface{}{
		"task_id": id,
		"status":  "killed",
	}
	out, _ := json.Marshal(payload)
	return &tool.ToolResult{Success: true, Output: string(out), Data: payload}, nil
}

func parseKillTaskID(params map[string]interface{}) (string, error) {
	if params == nil {
		return "", errors.New("params is nil")
	}
	raw, ok := params["task_id"]
	if !ok {
		return "", errors.New("task_id is required")
	}
	value, err := coerceString(raw)
	if err != nil {
		return "", fmt.Errorf("task_id must be string: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("task_id cannot be empty")
	}
	return value, nil
}

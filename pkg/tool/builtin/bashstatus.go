package toolbuiltin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const bashStatusDescription = "Check async bash task status without consuming output."

var bashStatusSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"task_id": map[string]interface{}{
			"type":        "string",
			"description": "Async task ID returned from Bash async mode.",
		},
	},
	Required: []string{"task_id"},
}

// BashStatusTool checks async task status without consuming buffered output.
// Returns: {"status":"running|completed|failed", "exit_code":0}
type BashStatusTool struct{}

func NewBashStatusTool() *BashStatusTool { return &BashStatusTool{} }

func (b *BashStatusTool) Name() string { return "BashStatus" }

func (b *BashStatusTool) Description() string { return bashStatusDescription }

func (b *BashStatusTool) Schema() *tool.JSONSchema { return bashStatusSchema }

func (b *BashStatusTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	id, err := parseBashStatusTaskID(params)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for _, info := range DefaultAsyncTaskManager().List() {
		if info.ID != id {
			continue
		}
		payload := map[string]interface{}{
			"task_id": id,
			"status":  info.Status,
		}
		switch info.Status {
		case "completed":
			payload["exit_code"] = 0
		case "failed":
			payload["error"] = info.Error
		}
		out, _ := json.Marshal(payload)
		return &tool.ToolResult{Success: true, Output: string(out), Data: payload}, nil
	}

	return nil, fmt.Errorf("task %s not found", id)
}

func parseBashStatusTaskID(params map[string]interface{}) (string, error) {
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

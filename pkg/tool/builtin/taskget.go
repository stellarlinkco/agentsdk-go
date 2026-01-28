package toolbuiltin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const taskGetDescription = "Retrieve a task by ID with full block/blocker details."

var taskGetSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"taskId": map[string]interface{}{
			"type":        "string",
			"description": "ID of task to retrieve",
		},
	},
	Required: []string{"taskId"},
}

type TaskGetTool struct {
	store *TaskStore
}

func NewTaskGetTool(store *TaskStore) *TaskGetTool {
	return &TaskGetTool{store: store}
}

func (t *TaskGetTool) Name() string { return "TaskGet" }

func (t *TaskGetTool) Description() string { return taskGetDescription }

func (t *TaskGetTool) Schema() *tool.JSONSchema { return taskGetSchema }

func (t *TaskGetTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if t == nil || t.store == nil {
		return nil, errors.New("task store is not configured")
	}
	taskID, err := requiredString(params, "taskId")
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	target, ok := t.store.Get(taskID)
	if !ok {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	all := t.store.List()

	blockedBy := taskRefsByID(target.BlockedBy, all)
	blocks := taskRefsForBlocks(target.ID, all)

	output := formatTaskGetOutput(target, blockedBy, blocks)
	return &tool.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"task":      target,
			"blockedBy": blockedBy,
			"blocks":    blocks,
		},
	}, nil
}

type TaskRef struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
	Owner  string `json:"owner,omitempty"`
}

func taskRefsByID(ids []string, tasks []Task) []TaskRef {
	if len(ids) == 0 || len(tasks) == 0 {
		return nil
	}
	byID := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	out := make([]TaskRef, 0, len(ids))
	for _, id := range ids {
		task, ok := byID[id]
		if !ok {
			out = append(out, TaskRef{ID: id, Status: ""})
			continue
		}
		out = append(out, TaskRef{
			ID:     task.ID,
			Title:  task.Title,
			Status: task.Status,
			Owner:  task.Owner,
		})
	}
	return out
}

func taskRefsForBlocks(id string, tasks []Task) []TaskRef {
	if strings.TrimSpace(id) == "" || len(tasks) == 0 {
		return nil
	}
	out := make([]TaskRef, 0)
	for _, task := range tasks {
		if slices.Contains(task.BlockedBy, id) {
			out = append(out, TaskRef{
				ID:     task.ID,
				Title:  task.Title,
				Status: task.Status,
				Owner:  task.Owner,
			})
		}
	}
	slices.SortFunc(out, func(a, b TaskRef) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func formatTaskGetOutput(task Task, blockedBy []TaskRef, blocks []TaskRef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "task %s\n", task.ID)
	if strings.TrimSpace(task.Title) != "" {
		fmt.Fprintf(&b, "title: %s\n", strings.TrimSpace(task.Title))
	}
	fmt.Fprintf(&b, "status: %s\n", task.Status)
	if strings.TrimSpace(task.Owner) != "" {
		fmt.Fprintf(&b, "owner: %s\n", strings.TrimSpace(task.Owner))
	}
	if len(blockedBy) == 0 {
		b.WriteString("blockedBy: (none)\n")
	} else {
		b.WriteString("blockedBy:\n")
		for _, ref := range blockedBy {
			fmt.Fprintf(&b, "- %s", ref.ID)
			if ref.Status != "" {
				fmt.Fprintf(&b, " [%s]", ref.Status)
			}
			if strings.TrimSpace(ref.Title) != "" {
				fmt.Fprintf(&b, " %s", strings.TrimSpace(ref.Title))
			}
			if strings.TrimSpace(ref.Owner) != "" {
				fmt.Fprintf(&b, " (owner=%s)", strings.TrimSpace(ref.Owner))
			}
			b.WriteByte('\n')
		}
	}

	if len(blocks) == 0 {
		b.WriteString("blocks: (none)")
		return b.String()
	}
	b.WriteString("blocks:\n")
	for _, ref := range blocks {
		fmt.Fprintf(&b, "- %s [%s]", ref.ID, ref.Status)
		if strings.TrimSpace(ref.Title) != "" {
			fmt.Fprintf(&b, " %s", strings.TrimSpace(ref.Title))
		}
		if strings.TrimSpace(ref.Owner) != "" {
			fmt.Fprintf(&b, " (owner=%s)", strings.TrimSpace(ref.Owner))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

package toolbuiltin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const taskListDescription = "List tasks with optional status/owner filtering and dependency visualization."

var taskListSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"status": map[string]interface{}{
			"type":        "string",
			"description": "Filter by status",
			"enum": []string{
				TaskStatusPending,
				TaskStatusInProgress,
				TaskStatusCompleted,
				TaskStatusBlocked,
			},
		},
		"owner": map[string]interface{}{
			"type":        "string",
			"description": "Filter by owner",
		},
	},
}

type TaskListTool struct {
	store *TaskStore
}

func NewTaskListTool(store *TaskStore) *TaskListTool {
	return &TaskListTool{store: store}
}

func (t *TaskListTool) Name() string { return "TaskList" }

func (t *TaskListTool) Description() string { return taskListDescription }

func (t *TaskListTool) Schema() *tool.JSONSchema { return taskListSchema }

func (t *TaskListTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if t == nil || t.store == nil {
		return nil, errors.New("task store is not configured")
	}
	filterStatus, filterOwner, err := parseTaskListParams(params)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	all := t.store.List()
	filtered := filterTasks(all, filterStatus, filterOwner)
	counts := countTasksByStatus(filtered)
	order, depth, blocks := taskDisplayOrder(filtered)

	output := formatTaskListOutput(filtered, order, depth, blocks, counts)
	return &tool.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"tasks":  filtered,
			"total":  len(filtered),
			"counts": counts,
		},
	}, nil
}

func parseTaskListParams(params map[string]interface{}) (string, string, error) {
	if params == nil {
		return "", "", nil
	}
	var status string
	if raw, ok := params["status"]; ok {
		value, err := coerceString(raw)
		if err != nil {
			return "", "", fmt.Errorf("status must be string: %w", err)
		}
		value = strings.TrimSpace(value)
		if value != "" {
			status = normalizeTaskStatus(value)
			if status == "" {
				return "", "", fmt.Errorf("status %q is invalid", value)
			}
		}
	}
	var owner string
	if raw, ok := params["owner"]; ok {
		value, err := coerceString(raw)
		if err != nil {
			return "", "", fmt.Errorf("owner must be string: %w", err)
		}
		owner = strings.TrimSpace(value)
	}
	return status, owner, nil
}

func filterTasks(tasks []Task, status, owner string) []Task {
	if len(tasks) == 0 {
		return nil
	}
	var out []Task
	for _, task := range tasks {
		if status != "" && task.Status != status {
			continue
		}
		if owner != "" && !strings.EqualFold(task.Owner, owner) {
			continue
		}
		out = append(out, task)
	}
	return out
}

func countTasksByStatus(tasks []Task) map[string]int {
	counts := map[string]int{
		TaskStatusPending:    0,
		TaskStatusInProgress: 0,
		TaskStatusCompleted:  0,
		TaskStatusBlocked:    0,
	}
	for _, task := range tasks {
		if _, ok := counts[task.Status]; ok {
			counts[task.Status]++
		}
	}
	return counts
}

func taskDisplayOrder(tasks []Task) ([]string, map[string]int, map[string][]string) {
	if len(tasks) == 0 {
		return nil, nil, nil
	}
	byID := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}

	blocks := make(map[string][]string, len(tasks))
	indegree := make(map[string]int, len(tasks))
	for id := range byID {
		indegree[id] = 0
	}
	for _, task := range byID {
		for _, blockerID := range task.BlockedBy {
			if _, ok := byID[blockerID]; !ok {
				continue
			}
			blocks[blockerID] = append(blocks[blockerID], task.ID)
			indegree[task.ID]++
		}
	}

	available := make([]string, 0, len(byID))
	for id, deg := range indegree {
		if deg == 0 {
			available = append(available, id)
		}
	}
	sortIDsByTaskKey(available, byID)

	order := make([]string, 0, len(byID))
	inOrder := make(map[string]struct{}, len(byID))
	for len(available) > 0 {
		id := available[0]
		available = available[1:]
		order = append(order, id)
		inOrder[id] = struct{}{}

		children := blocks[id]
		sortIDsByTaskKey(children, byID)
		blocks[id] = children

		for _, child := range children {
			indegree[child]--
			if indegree[child] == 0 {
				available = append(available, child)
			}
		}
		sortIDsByTaskKey(available, byID)
	}

	if len(order) != len(byID) {
		remaining := make([]string, 0, len(byID)-len(order))
		for id := range byID {
			if _, ok := inOrder[id]; ok {
				continue
			}
			remaining = append(remaining, id)
		}
		sortIDsByTaskKey(remaining, byID)
		order = append(order, remaining...)
	}

	depth := make(map[string]int, len(order))
	for _, id := range order {
		task := byID[id]
		level := 0
		for _, blockerID := range task.BlockedBy {
			blockerDepth, ok := depth[blockerID]
			if !ok {
				continue
			}
			if blockerDepth+1 > level {
				level = blockerDepth + 1
			}
		}
		depth[id] = level
	}

	return order, depth, blocks
}

func sortIDsByTaskKey(ids []string, tasks map[string]Task) {
	slices.SortFunc(ids, func(a, b string) int {
		ta, ok := tasks[a]
		if !ok {
			return strings.Compare(a, b)
		}
		tb, ok := tasks[b]
		if !ok {
			return strings.Compare(a, b)
		}
		if pa, pb := taskStatusPriority(ta.Status), taskStatusPriority(tb.Status); pa != pb {
			if pa < pb {
				return -1
			}
			return 1
		}
		if cmp := strings.Compare(strings.ToLower(ta.Owner), strings.ToLower(tb.Owner)); cmp != 0 {
			return cmp
		}
		return strings.Compare(ta.ID, tb.ID)
	})
}

func taskStatusPriority(status string) int {
	switch status {
	case TaskStatusInProgress:
		return 0
	case TaskStatusBlocked:
		return 1
	case TaskStatusPending:
		return 2
	case TaskStatusCompleted:
		return 3
	default:
		return 4
	}
}

func formatTaskListOutput(tasks []Task, order []string, depth map[string]int, blocks map[string][]string, counts map[string]int) string {
	if len(tasks) == 0 {
		return "no tasks"
	}
	byID := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d tasks (pending:%d, in_progress:%d, completed:%d, blocked:%d)\n",
		len(tasks),
		counts[TaskStatusPending],
		counts[TaskStatusInProgress],
		counts[TaskStatusCompleted],
		counts[TaskStatusBlocked],
	)
	for _, id := range order {
		task, ok := byID[id]
		if !ok {
			continue
		}
		prefix := strings.Repeat("  ", depth[id])
		fmt.Fprintf(&b, "%s- [%s] %s", prefix, task.Status, task.ID)
		if strings.TrimSpace(task.Title) != "" {
			fmt.Fprintf(&b, " %s", strings.TrimSpace(task.Title))
		}
		if strings.TrimSpace(task.Owner) != "" {
			fmt.Fprintf(&b, " (owner=%s)", strings.TrimSpace(task.Owner))
		}
		b.WriteByte('\n')
		if len(task.BlockedBy) > 0 {
			fmt.Fprintf(&b, "%s  blockedBy: %s\n", prefix, strings.Join(task.BlockedBy, ", "))
		}
		if children := blocks[id]; len(children) > 0 {
			fmt.Fprintf(&b, "%s  blocks: %s\n", prefix, strings.Join(children, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

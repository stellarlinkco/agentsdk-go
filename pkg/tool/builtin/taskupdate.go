package toolbuiltin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const taskUpdateDescription = "Update a task's status, owner, and dependencies."

var taskUpdateSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"taskId": map[string]interface{}{
			"type":        "string",
			"description": "ID of the task to update.",
		},
		"status": map[string]interface{}{
			"type":        "string",
			"description": "New task status.",
			"enum": []string{
				TaskStatusPending,
				TaskStatusInProgress,
				TaskStatusCompleted,
			},
		},
		"owner": map[string]interface{}{
			"type":        "string",
			"description": "Optional task owner.",
		},
		"blocks": map[string]interface{}{
			"type":        "array",
			"description": "IDs of tasks blocked by this task.",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
		"blockedBy": map[string]interface{}{
			"type":        "array",
			"description": "IDs of tasks that block this task.",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
	},
	Required: []string{"taskId"},
}

type TaskUpdateTool struct {
	mu       sync.Mutex
	store    *TaskStore
	revision uint64
}

func NewTaskUpdateTool(store *TaskStore) *TaskUpdateTool {
	return &TaskUpdateTool{store: store}
}

func (t *TaskUpdateTool) Name() string { return "TaskUpdate" }

func (t *TaskUpdateTool) Description() string { return taskUpdateDescription }

func (t *TaskUpdateTool) Schema() *tool.JSONSchema { return taskUpdateSchema }

func (t *TaskUpdateTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if t == nil || t.store == nil {
		return nil, errors.New("task store is not configured")
	}
	req, err := parseTaskUpdateParams(params)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.store.mu.Lock()
	defer t.store.mu.Unlock()

	if t.store.tasks == nil {
		t.store.tasks = map[string]Task{}
	}

	current, ok := t.store.tasks[req.TaskID]
	if !ok {
		current = Task{ID: req.TaskID, Status: TaskStatusPending}
	}
	oldStatus := current.Status

	nextBlockedBy := current.BlockedBy
	if req.HasBlockedBy {
		nextBlockedBy = req.BlockedBy
	}
	if req.Status != nil && *req.Status == TaskStatusInProgress && len(nextBlockedBy) > 0 {
		return nil, errors.New("cannot set status to in_progress while blockedBy is non-empty")
	}

	if req.Owner != nil {
		current.Owner = strings.TrimSpace(*req.Owner)
	}
	if req.Status != nil {
		current.Status = *req.Status
	}
	if req.HasBlockedBy {
		current.BlockedBy = req.BlockedBy
		for _, blocker := range current.BlockedBy {
			ensureTaskExistsLocked(t.store.tasks, blocker)
		}
	}

	affected := map[string]struct{}{current.ID: {}}
	if req.HasBlocks {
		applyBlocksLocked(t.store.tasks, current.ID, req.Blocks, affected)
		for _, dep := range req.Blocks {
			ensureTaskExistsLocked(t.store.tasks, dep)
		}
	}

	current = reconcileTaskLocked(current)
	t.store.tasks[current.ID] = current

	var unblocked []string
	if oldStatus != TaskStatusCompleted && current.Status == TaskStatusCompleted {
		unblocked = unblockDownstreamLocked(t.store.tasks, current.ID, affected)
	}

	t.revision++
	revision := t.revision
	blocks := blocksForTaskLocked(t.store.tasks, current.ID)

	payload := map[string]interface{}{
		"task":     current,
		"blocks":   blocks,
		"revision": revision,
	}
	if len(unblocked) > 0 {
		payload["unblocked"] = unblocked
	}
	if len(affected) > 1 {
		payload["affected"] = sortedKeys(affected, current.ID)
	}

	return &tool.ToolResult{
		Success: true,
		Output:  formatTaskUpdateOutput(current, blocks, unblocked, revision),
		Data:    payload,
	}, nil
}

func (t *TaskUpdateTool) Snapshot(taskID string) (Task, bool) {
	if t == nil || t.store == nil {
		return Task{}, false
	}
	return t.store.Get(taskID)
}

type taskUpdateRequest struct {
	TaskID       string
	Status       *string
	Owner        *string
	Blocks       []string
	HasBlocks    bool
	BlockedBy    []string
	HasBlockedBy bool
}

func parseTaskUpdateParams(params map[string]interface{}) (taskUpdateRequest, error) {
	if params == nil {
		return taskUpdateRequest{}, errors.New("params is nil")
	}
	taskID, err := requiredString(params, "taskId")
	if err != nil {
		return taskUpdateRequest{}, err
	}
	req := taskUpdateRequest{TaskID: taskID}

	if raw, ok := params["status"]; ok && raw != nil {
		value, err := coerceString(raw)
		if err != nil {
			return taskUpdateRequest{}, fmt.Errorf("status must be string: %w", err)
		}
		normalized := normalizeUpdateStatus(value)
		if normalized == "" {
			return taskUpdateRequest{}, fmt.Errorf("status %q is invalid", strings.TrimSpace(value))
		}
		req.Status = &normalized
	}

	if raw, ok := params["owner"]; ok {
		var owner string
		if raw != nil {
			value, err := coerceString(raw)
			if err != nil {
				return taskUpdateRequest{}, fmt.Errorf("owner must be string: %w", err)
			}
			owner = strings.TrimSpace(value)
		}
		req.Owner = &owner
	}

	if raw, ok := params["blocks"]; ok {
		req.HasBlocks = true
		list, err := parseTaskIDList(raw, "blocks", taskID)
		if err != nil {
			return taskUpdateRequest{}, err
		}
		req.Blocks = list
	}

	if raw, ok := params["blockedBy"]; ok {
		req.HasBlockedBy = true
		list, err := parseTaskIDList(raw, "blockedBy", taskID)
		if err != nil {
			return taskUpdateRequest{}, err
		}
		req.BlockedBy = list
	}

	return req, nil
}

func normalizeUpdateStatus(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	switch trimmed {
	case TaskStatusPending, TaskStatusInProgress, TaskStatusCompleted:
		return trimmed
	case "complete", "done":
		return TaskStatusCompleted
	default:
		return ""
	}
}

func parseTaskIDList(value interface{}, field, selfID string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	var rawList []interface{}
	switch v := value.(type) {
	case []interface{}:
		rawList = v
	case []string:
		rawList = make([]interface{}, len(v))
		for i := range v {
			rawList[i] = v[i]
		}
	default:
		return nil, fmt.Errorf("%s must be an array, got %T", field, value)
	}

	seen := make(map[string]struct{}, len(rawList))
	out := make([]string, 0, len(rawList))
	for i, raw := range rawList {
		id, err := coerceString(raw)
		if err != nil {
			return nil, fmt.Errorf("%s[%d] must be string: %w", field, i, err)
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("%s[%d] cannot be empty", field, i)
		}
		if id == selfID {
			return nil, fmt.Errorf("%s[%d] cannot reference taskId", field, i)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func ensureTaskExistsLocked(tasks map[string]Task, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if _, ok := tasks[id]; ok {
		return
	}
	tasks[id] = Task{ID: id, Status: TaskStatusPending}
}

func reconcileTaskLocked(task Task) Task {
	task.Status = normalizeTaskStatus(task.Status)
	if task.Status == TaskStatusCompleted {
		return task
	}
	if len(task.BlockedBy) > 0 {
		task.Status = TaskStatusBlocked
		return task
	}
	if task.Status == TaskStatusBlocked {
		task.Status = TaskStatusPending
	}
	return task
}

func applyBlocksLocked(tasks map[string]Task, blockerID string, desired []string, touched map[string]struct{}) {
	existing := blocksForTaskLocked(tasks, blockerID)
	existingSet := make(map[string]struct{}, len(existing))
	for _, id := range existing {
		existingSet[id] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, id := range desired {
		desiredSet[id] = struct{}{}
	}

	for _, id := range existing {
		if _, ok := desiredSet[id]; ok {
			continue
		}
		task := tasks[id]
		next := removeTaskID(task.BlockedBy, blockerID)
		if slices.Equal(next, task.BlockedBy) {
			continue
		}
		task.BlockedBy = next
		task = reconcileTaskLocked(task)
		tasks[id] = task
		touched[id] = struct{}{}
	}

	for _, id := range desired {
		if _, ok := existingSet[id]; ok {
			continue
		}
		task, ok := tasks[id]
		if !ok {
			task = Task{ID: id, Status: TaskStatusPending}
		}
		task.BlockedBy = addTaskID(task.BlockedBy, blockerID)
		task = reconcileTaskLocked(task)
		tasks[id] = task
		touched[id] = struct{}{}
	}
}

func unblockDownstreamLocked(tasks map[string]Task, blockerID string, touched map[string]struct{}) []string {
	var downstream []string
	for id, task := range tasks {
		if slices.Contains(task.BlockedBy, blockerID) {
			downstream = append(downstream, id)
		}
	}
	if len(downstream) == 0 {
		return nil
	}
	sort.Strings(downstream)

	var unblocked []string
	for _, id := range downstream {
		task := tasks[id]
		before := len(task.BlockedBy)
		task.BlockedBy = removeTaskID(task.BlockedBy, blockerID)
		if len(task.BlockedBy) == before {
			continue
		}
		wasBlocked := task.Status == TaskStatusBlocked
		task = reconcileTaskLocked(task)
		tasks[id] = task
		touched[id] = struct{}{}
		if wasBlocked && task.Status == TaskStatusPending {
			unblocked = append(unblocked, id)
		}
	}
	if len(unblocked) == 0 {
		return nil
	}
	sort.Strings(unblocked)
	return unblocked
}

func blocksForTaskLocked(tasks map[string]Task, blockerID string) []string {
	var blocks []string
	for id, task := range tasks {
		if id == blockerID {
			continue
		}
		if slices.Contains(task.BlockedBy, blockerID) {
			blocks = append(blocks, id)
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	sort.Strings(blocks)
	return blocks
}

func addTaskID(ids []string, id string) []string {
	ids = append(ids, id)
	out, _ := normalizeTaskIDs(ids)
	return out
}

func removeTaskID(ids []string, id string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, existing := range ids {
		if existing == id {
			continue
		}
		out = append(out, existing)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedKeys(set map[string]struct{}, except string) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		if key == except {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func formatTaskUpdateOutput(task Task, blocks []string, unblocked []string, revision uint64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "task %s\n", task.ID)
	fmt.Fprintf(&b, "status: %s\n", task.Status)
	if strings.TrimSpace(task.Owner) != "" {
		fmt.Fprintf(&b, "owner: %s\n", strings.TrimSpace(task.Owner))
	}
	if len(task.BlockedBy) == 0 {
		b.WriteString("blockedBy: (none)\n")
	} else {
		fmt.Fprintf(&b, "blockedBy: %s\n", strings.Join(task.BlockedBy, ", "))
	}
	if len(blocks) == 0 {
		b.WriteString("blocks: (none)\n")
	} else {
		fmt.Fprintf(&b, "blocks: %s\n", strings.Join(blocks, ", "))
	}
	if len(unblocked) > 0 {
		fmt.Fprintf(&b, "unblocked: %s\n", strings.Join(unblocked, ", "))
	}
	fmt.Fprintf(&b, "revision: %d", revision)
	return b.String()
}

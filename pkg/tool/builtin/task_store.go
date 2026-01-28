package toolbuiltin

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

const (
	TaskStatusPending    = "pending"
	TaskStatusInProgress = "in_progress"
	TaskStatusCompleted  = "completed"
	TaskStatusBlocked    = "blocked"
)

var taskStatusSet = map[string]struct{}{
	TaskStatusPending:    {},
	TaskStatusInProgress: {},
	TaskStatusCompleted:  {},
	TaskStatusBlocked:    {},
}

// Task represents a unit of work tracked by the task system.
type Task struct {
	ID        string   `json:"id"`
	Title     string   `json:"title,omitempty"`
	Status    string   `json:"status"`
	Owner     string   `json:"owner,omitempty"`
	BlockedBy []string `json:"blockedBy,omitempty"`
}

// TaskStore keeps task state in memory with a small, concurrency-safe API.
type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]Task
}

func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: map[string]Task{},
	}
}

func (s *TaskStore) Upsert(task Task) error {
	if s == nil {
		return errors.New("task store is nil")
	}
	id := strings.TrimSpace(task.ID)
	if id == "" {
		return errors.New("task id cannot be empty")
	}
	status := normalizeTaskStatus(task.Status)
	if status == "" {
		return fmt.Errorf("task status %q is invalid", task.Status)
	}
	blockedBy, err := normalizeTaskIDs(task.BlockedBy)
	if err != nil {
		return err
	}
	if slices.Contains(blockedBy, id) {
		return errors.New("task cannot be blocked by itself")
	}

	task.ID = id
	task.Title = strings.TrimSpace(task.Title)
	task.Owner = strings.TrimSpace(task.Owner)
	task.Status = status
	task.BlockedBy = blockedBy

	s.mu.Lock()
	if s.tasks == nil {
		s.tasks = map[string]Task{}
	}
	s.tasks[id] = task
	s.mu.Unlock()
	return nil
}

func (s *TaskStore) Get(id string) (Task, bool) {
	if s == nil {
		return Task{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Task{}, false
	}
	s.mu.RLock()
	task, ok := s.tasks[id]
	s.mu.RUnlock()
	return task, ok
}

func (s *TaskStore) List() []Task {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.tasks) == 0 {
		return nil
	}
	out := make([]Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		out = append(out, task)
	}
	slices.SortFunc(out, func(a, b Task) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func normalizeTaskStatus(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return TaskStatusPending
	}
	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	if _, ok := taskStatusSet[trimmed]; ok {
		return trimmed
	}
	switch trimmed {
	case "complete", "done":
		return TaskStatusCompleted
	default:
		return ""
	}
}

func normalizeTaskIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	slices.Sort(out)
	return out, nil
}

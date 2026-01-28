package tasks

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrSelfDependency  = errors.New("tasks: task cannot depend on itself")
	ErrDependencyCycle = errors.New("tasks: dependency cycle detected")
)

func (s *TaskStore) AddDependency(taskID, blockedByID string) error {
	taskID = strings.TrimSpace(taskID)
	blockedByID = strings.TrimSpace(blockedByID)
	if taskID == "" || blockedByID == "" {
		return ErrInvalidTaskID
	}
	if taskID == blockedByID {
		return ErrSelfDependency
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	blocker := s.tasks[blockedByID]
	if task == nil || blocker == nil {
		return ErrTaskNotFound
	}

	if introducesCycleLocked(s.tasks, taskID, blockedByID) {
		return ErrDependencyCycle
	}

	task.BlockedBy = addUnique(task.BlockedBy, blockedByID)
	blocker.Blocks = addUnique(blocker.Blocks, taskID)

	s.reconcileBlockedStatusLocked(task)
	task.UpdatedAt = now
	blocker.UpdatedAt = now
	return nil
}

func (s *TaskStore) RemoveDependency(taskID, blockedByID string) error {
	taskID = strings.TrimSpace(taskID)
	blockedByID = strings.TrimSpace(blockedByID)
	if taskID == "" || blockedByID == "" {
		return ErrInvalidTaskID
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[taskID]
	blocker := s.tasks[blockedByID]
	if task == nil || blocker == nil {
		return ErrTaskNotFound
	}

	task.BlockedBy = removeString(task.BlockedBy, blockedByID)
	blocker.Blocks = removeString(blocker.Blocks, taskID)
	s.reconcileBlockedStatusLocked(task)

	task.UpdatedAt = now
	blocker.UpdatedAt = now
	return nil
}

func (s *TaskStore) onTaskCompleted(taskID string) {
	task := s.tasks[taskID]
	if task == nil {
		return
	}
	s.onTaskStatusChangedLocked(taskID, time.Now())
}

func (s *TaskStore) onTaskStatusChangedLocked(taskID string, now time.Time) {
	task := s.tasks[taskID]
	if task == nil {
		return
	}
	for _, blockedID := range task.Blocks {
		blocked := s.tasks[blockedID]
		if blocked == nil {
			continue
		}
		previous := blocked.Status
		s.reconcileBlockedStatusLocked(blocked)
		if blocked.Status != previous {
			blocked.UpdatedAt = now
		}
	}
}

func (s *TaskStore) GetBlockedTasks(taskID string) []*Task {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}

	s.mu.RLock()
	task := s.tasks[taskID]
	if task == nil {
		s.mu.RUnlock()
		return nil
	}
	ids := cloneStrings(task.Blocks)
	s.mu.RUnlock()

	out := make([]*Task, 0, len(ids))
	s.mu.RLock()
	for _, id := range ids {
		blocked := s.tasks[id]
		if blocked == nil {
			continue
		}
		out = append(out, cloneTask(blocked))
	}
	s.mu.RUnlock()
	return out
}

func (s *TaskStore) GetBlockingTasks(taskID string) []*Task {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}

	s.mu.RLock()
	task := s.tasks[taskID]
	if task == nil {
		s.mu.RUnlock()
		return nil
	}
	ids := cloneStrings(task.BlockedBy)
	s.mu.RUnlock()

	out := make([]*Task, 0, len(ids))
	s.mu.RLock()
	for _, id := range ids {
		blocker := s.tasks[id]
		if blocker == nil {
			continue
		}
		out = append(out, cloneTask(blocker))
	}
	s.mu.RUnlock()
	return out
}

func addUnique(list []string, value string) []string {
	for _, item := range list {
		if item == value {
			return list
		}
	}
	return append(list, value)
}

func introducesCycleLocked(tasks map[string]*Task, taskID, blockedByID string) bool {
	if taskID == blockedByID {
		return true
	}
	seen := map[string]struct{}{}
	stack := []string{taskID}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == blockedByID {
			return true
		}
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		task := tasks[node]
		if task == nil {
			continue
		}
		for _, next := range task.Blocks {
			if _, ok := seen[next]; ok {
				continue
			}
			stack = append(stack, next)
		}
	}
	return false
}

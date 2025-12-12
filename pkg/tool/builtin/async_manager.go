package toolbuiltin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	maxAsyncTasks     = 50
	maxAsyncOutputLen = 1024 * 1024 // 1MB
)

// AsyncTask represents a single async bash invocation.
type AsyncTask struct {
	ID        string
	Command   string
	StartTime time.Time
	Done      chan struct{}
	Output    bytes.Buffer
	Error     error

	mu       sync.Mutex
	consumed int
	cancel   context.CancelFunc
	cmd      *exec.Cmd
}

// AsyncTaskInfo is a lightweight snapshot used by List().
type AsyncTaskInfo struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
	Error     string    `json:"error,omitempty"`
}

// AsyncTaskManager tracks and manages async bash tasks.
type AsyncTaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*AsyncTask
}

var defaultAsyncTaskManager = newAsyncTaskManager()

// DefaultAsyncTaskManager returns the global async task manager.
func DefaultAsyncTaskManager() *AsyncTaskManager {
	return defaultAsyncTaskManager
}

func newAsyncTaskManager() *AsyncTaskManager {
	return &AsyncTaskManager{tasks: map[string]*AsyncTask{}}
}

// Start launches a task in the background using a detached context.
func (m *AsyncTaskManager) Start(id, command string) error {
	return m.startWithContext(context.Background(), id, command, "", 0)
}

func (m *AsyncTaskManager) startWithContext(ctx context.Context, id, command, workdir string, timeout time.Duration) error {
	if m == nil {
		return errors.New("async task manager is nil")
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return errors.New("task id cannot be empty")
	}
	trimmedCmd := strings.TrimSpace(command)
	if trimmedCmd == "" {
		return errors.New("command cannot be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.tasks == nil {
		m.tasks = map[string]*AsyncTask{}
	}
	if _, exists := m.tasks[trimmedID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("task %s already exists", trimmedID)
	}
	if m.runningCountLocked() >= maxAsyncTasks {
		m.mu.Unlock()
		return fmt.Errorf("async task limit reached (%d)", maxAsyncTasks)
	}
	task := &AsyncTask{
		ID:        trimmedID,
		Command:   trimmedCmd,
		StartTime: time.Now(),
		Done:      make(chan struct{}),
	}
	m.tasks[trimmedID] = task
	m.mu.Unlock()

	execCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}

	task.mu.Lock()
	task.cancel = cancel
	task.mu.Unlock()

	cmd := exec.CommandContext(execCtx, "bash", "-c", trimmedCmd)
	cmd.Env = os.Environ()
	if strings.TrimSpace(workdir) != "" {
		cmd.Dir = workdir
	}
	writer := &asyncTaskWriter{task: task}
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Start(); err != nil {
		cancel()
		m.mu.Lock()
		delete(m.tasks, trimmedID)
		m.mu.Unlock()
		return fmt.Errorf("start task: %w", err)
	}

	task.mu.Lock()
	task.cmd = cmd
	task.mu.Unlock()

	go func() {
		err := cmd.Wait()
		task.mu.Lock()
		task.Error = err
		task.mu.Unlock()
		cancel()
		close(task.Done)
	}()

	return nil
}

// GetOutput returns incremental output since last read, whether the task is done, and any task error.
func (m *AsyncTaskManager) GetOutput(id string) (string, bool, error) {
	task, ok := m.lookup(strings.TrimSpace(id))
	if !ok {
		return "", false, fmt.Errorf("task %s not found", strings.TrimSpace(id))
	}
	task.mu.Lock()
	defer task.mu.Unlock()
	data := task.Output.Bytes()
	if task.consumed >= len(data) {
		return "", isDone(task.Done), task.Error
	}
	chunk := string(data[task.consumed:])
	task.consumed = len(data)
	return chunk, isDone(task.Done), task.Error
}

// Kill terminates a running task.
func (m *AsyncTaskManager) Kill(id string) error {
	task, ok := m.lookup(strings.TrimSpace(id))
	if !ok {
		return fmt.Errorf("task %s not found", strings.TrimSpace(id))
	}
	task.mu.Lock()
	cancel := task.cancel
	cmd := task.cmd
	done := isDone(task.Done)
	task.mu.Unlock()

	if done {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("async task %s kill: %v", id, err)
		}
	}
	return nil
}

// List reports all known tasks with their status.
func (m *AsyncTaskManager) List() []AsyncTaskInfo {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AsyncTaskInfo, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task == nil {
			continue
		}
		done := isDone(task.Done)
		task.mu.Lock()
		err := task.Error
		cmd := task.Command
		start := task.StartTime
		id := task.ID
		task.mu.Unlock()
		status := "running"
		if done {
			if err != nil {
				status = "failed"
			} else {
				status = "completed"
			}
		}
		info := AsyncTaskInfo{
			ID:        id,
			Command:   cmd,
			Status:    status,
			StartTime: start,
		}
		if err != nil {
			info.Error = err.Error()
		}
		out = append(out, info)
	}
	return out
}

func (m *AsyncTaskManager) lookup(id string) (*AsyncTask, bool) {
	if m == nil || id == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	return task, ok
}

func (m *AsyncTaskManager) runningCountLocked() int {
	count := 0
	for _, task := range m.tasks {
		if task == nil {
			continue
		}
		if !isDone(task.Done) {
			count++
		}
	}
	return count
}

type asyncTaskWriter struct {
	task *AsyncTask
}

func (w *asyncTaskWriter) Write(p []byte) (int, error) {
	if w == nil || w.task == nil || len(p) == 0 {
		return len(p), nil
	}
	origLen := len(p)
	w.task.mu.Lock()
	defer w.task.mu.Unlock()
	if w.task.Output.Len() >= maxAsyncOutputLen {
		return origLen, nil
	}
	remaining := maxAsyncOutputLen - w.task.Output.Len()
	if remaining <= 0 {
		return origLen, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = w.task.Output.Write(p)
	return origLen, nil
}

func isDone(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

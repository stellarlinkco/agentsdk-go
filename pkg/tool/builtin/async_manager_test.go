package toolbuiltin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAsyncTaskManagerStartGetOutputAndList(t *testing.T) {
	skipIfWindows(t)
	m := newAsyncTaskManager()
	if err := m.Start("task-1", "echo hello"); err != nil {
		t.Fatalf("start: %v", err)
	}
	task, ok := m.lookup("task-1")
	if !ok {
		t.Fatalf("expected task to be registered")
	}
	select {
	case <-task.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("task did not complete")
	}

	out, done, err := m.GetOutput("task-1")
	if err != nil {
		t.Fatalf("get output: %v", err)
	}
	if !done {
		t.Fatalf("expected done=true")
	}
	if out == "" || !strings.Contains(out, "hello") {
		t.Fatalf("unexpected output %q", out)
	}
	// Second read should be empty.
	out, _, _ = m.GetOutput("task-1")
	if out != "" {
		t.Fatalf("expected no new output, got %q", out)
	}

	list := m.List()
	if len(list) != 1 || list[0].ID != "task-1" {
		t.Fatalf("unexpected list %+v", list)
	}
}

func TestAsyncTaskManagerKillStopsTask(t *testing.T) {
	skipIfWindows(t)
	m := newAsyncTaskManager()
	if err := m.Start("task-kill", "sleep 5"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := m.Kill("task-kill"); err != nil {
		t.Fatalf("kill: %v", err)
	}
	task, _ := m.lookup("task-kill")
	select {
	case <-task.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("task did not stop after kill")
	}
	_, done, err := m.GetOutput("task-kill")
	if !done {
		t.Fatalf("expected done after kill")
	}
	if err == nil {
		t.Fatalf("expected error after kill")
	}
}

func TestAsyncTaskManagerTaskLimit(t *testing.T) {
	skipIfWindows(t)
	m := newAsyncTaskManager()
	for i := 0; i < maxAsyncTasks; i++ {
		id := fmt.Sprintf("t-%d", i)
		if err := m.Start(id, "sleep 5"); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}
	if err := m.Start("overflow", "sleep 5"); err == nil {
		t.Fatalf("expected limit error")
	}
	for i := 0; i < maxAsyncTasks; i++ {
		_ = m.Kill(fmt.Sprintf("t-%d", i))
	}
}

func TestAsyncTaskManagerConcurrentStarts(t *testing.T) {
	skipIfWindows(t)
	m := newAsyncTaskManager()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = m.Start(fmt.Sprintf("c-%d", i), "echo hi")
		}(i)
	}
	wg.Wait()
	if len(m.List()) != 10 {
		t.Fatalf("expected 10 tasks, got %d", len(m.List()))
	}
}

func TestAsyncTaskManagerContextCancellation(t *testing.T) {
	skipIfWindows(t)
	m := newAsyncTaskManager()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := m.startWithContext(ctx, "ctx-task", "sleep 5", "", 0); err != nil {
		t.Fatalf("startWithContext: %v", err)
	}
	task, _ := m.lookup("ctx-task")
	select {
	case <-task.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected task to cancel")
	}
	if task.Error == nil {
		t.Fatalf("expected cancellation error")
	}
}

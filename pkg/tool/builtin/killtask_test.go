package toolbuiltin

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestKillTaskToolKillsRunningTask(t *testing.T) {
	skipIfWindows(t)
	defaultAsyncTaskManager = newAsyncTaskManager()
	dir := cleanTempDir(t)
	bash := NewBashToolWithRoot(dir)
	res, err := bash.Execute(context.Background(), map[string]interface{}{
		"command": "sleep 5",
		"async":   true,
	})
	if err != nil {
		t.Fatalf("async bash: %v", err)
	}
	id := res.Data.(map[string]interface{})["task_id"].(string)
	tool := NewKillTaskTool()
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"task_id": id}); err != nil {
		t.Fatalf("kill: %v", err)
	}
	task, _ := DefaultAsyncTaskManager().lookup(id)
	select {
	case <-task.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("task did not stop")
	}
}

func TestKillTaskToolErrorsOnMissingTask(t *testing.T) {
	defaultAsyncTaskManager = newAsyncTaskManager()
	tool := NewKillTaskTool()
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"task_id": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

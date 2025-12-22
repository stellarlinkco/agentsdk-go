package toolbuiltin

import (
	"context"
	"errors"
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

func TestKillTaskToolMetadata(t *testing.T) {
	tool := NewKillTaskTool()
	if tool.Name() != "KillTask" {
		t.Fatalf("unexpected name %q", tool.Name())
	}
	if strings.TrimSpace(tool.Description()) == "" {
		t.Fatalf("expected non-empty description")
	}
	schema := tool.Schema()
	if schema == nil || schema.Type != "object" {
		t.Fatalf("unexpected schema %+v", schema)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "task_id" {
		t.Fatalf("unexpected required %+v", schema.Required)
	}
	raw, ok := schema.Properties["task_id"]
	if !ok {
		t.Fatalf("schema missing task_id")
	}
	prop := raw.(map[string]interface{})
	if prop["type"] != "string" {
		t.Fatalf("unexpected task_id schema %+v", prop)
	}
}

func TestKillTaskToolNilContextHandling(t *testing.T) {
	tool := NewKillTaskTool()
	if _, err := tool.Execute(nil, map[string]interface{}{"task_id": "x"}); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("expected context is nil error, got %v", err)
	}
}

func TestKillTaskToolCancelledContextReturnsError(t *testing.T) {
	tool := NewKillTaskTool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := tool.Execute(ctx, map[string]interface{}{"task_id": "x"}); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestKillTaskToolTaskIDValidation(t *testing.T) {
	tool := NewKillTaskTool()
	ctx := context.Background()

	cases := []struct {
		name   string
		params map[string]interface{}
		want   string
	}{
		{name: "nil params", params: nil, want: "params is nil"},
		{name: "missing task_id", params: map[string]interface{}{}, want: "task_id is required"},
		{name: "empty task_id", params: map[string]interface{}{"task_id": ""}, want: "task_id cannot be empty"},
		{name: "non-string task_id", params: map[string]interface{}{"task_id": 123}, want: "task_id must be string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tool.Execute(ctx, tc.params); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

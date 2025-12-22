package toolbuiltin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBashStatusRunningTaskReturnsRunning(t *testing.T) {
	skipIfWindows(t)
	defaultAsyncTaskManager = newAsyncTaskManager()

	dir := cleanTempDir(t)
	bash := NewBashToolWithRoot(dir)
	res, err := bash.Execute(context.Background(), map[string]interface{}{
		"command": "sleep 2",
		"async":   true,
	})
	if err != nil {
		t.Fatalf("async bash: %v", err)
	}
	id := res.Data.(map[string]interface{})["task_id"].(string)

	statusTool := NewBashStatusTool()
	got, err := statusTool.Execute(context.Background(), map[string]interface{}{"task_id": id})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	data := got.Data.(map[string]interface{})
	if data["status"] != "running" {
		t.Fatalf("expected status=running, got %+v", data)
	}

	_ = DefaultAsyncTaskManager().Kill(id)
	task, _ := DefaultAsyncTaskManager().lookup(id)
	select {
	case <-task.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("task did not stop")
	}
}

func TestBashStatusCompletedTaskReturnsExitCodeAndDoesNotConsumeOutput(t *testing.T) {
	skipIfWindows(t)
	defaultAsyncTaskManager = newAsyncTaskManager()

	dir := cleanTempDir(t)
	bash := NewBashToolWithRoot(dir)
	res, err := bash.Execute(context.Background(), map[string]interface{}{
		"command": "echo hello",
		"async":   true,
	})
	if err != nil {
		t.Fatalf("async bash: %v", err)
	}
	id := res.Data.(map[string]interface{})["task_id"].(string)

	task, _ := DefaultAsyncTaskManager().lookup(id)
	select {
	case <-task.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("task did not complete")
	}

	statusTool := NewBashStatusTool()
	got, err := statusTool.Execute(context.Background(), map[string]interface{}{"task_id": id})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	data := got.Data.(map[string]interface{})
	if data["status"] != "completed" {
		t.Fatalf("expected status=completed, got %+v", data)
	}
	exitCode, ok := data["exit_code"].(int)
	if !ok || exitCode != 0 {
		t.Fatalf("expected exit_code=0, got %+v", data["exit_code"])
	}

	out, done, err := DefaultAsyncTaskManager().GetOutput(id)
	if err != nil {
		t.Fatalf("get output: %v", err)
	}
	if !done {
		t.Fatalf("expected done=true")
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected output to contain hello, got %q", out)
	}
}

func TestBashStatusFailedTaskReturnsFailedWithError(t *testing.T) {
	skipIfWindows(t)
	defaultAsyncTaskManager = newAsyncTaskManager()

	dir := cleanTempDir(t)
	bash := NewBashToolWithRoot(dir)
	res, err := bash.Execute(context.Background(), map[string]interface{}{
		"command": "exit 3",
		"async":   true,
	})
	if err != nil {
		t.Fatalf("async bash: %v", err)
	}
	id := res.Data.(map[string]interface{})["task_id"].(string)

	task, _ := DefaultAsyncTaskManager().lookup(id)
	select {
	case <-task.Done:
	case <-time.After(2 * time.Second):
		t.Fatalf("task did not complete")
	}

	statusTool := NewBashStatusTool()
	got, err := statusTool.Execute(context.Background(), map[string]interface{}{"task_id": id})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	data := got.Data.(map[string]interface{})
	if data["status"] != "failed" {
		t.Fatalf("expected status=failed, got %+v", data)
	}
	errMsg, ok := data["error"].(string)
	if !ok || strings.TrimSpace(errMsg) == "" {
		t.Fatalf("expected error message, got %+v", data["error"])
	}
	if !strings.Contains(errMsg, "exit status") {
		t.Fatalf("expected error to mention exit status, got %q", errMsg)
	}
}

func TestBashStatusUnknownTaskReturnsError(t *testing.T) {
	defaultAsyncTaskManager = newAsyncTaskManager()
	statusTool := NewBashStatusTool()
	if _, err := statusTool.Execute(context.Background(), map[string]interface{}{"task_id": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestBashStatusMetadata(t *testing.T) {
	statusTool := NewBashStatusTool()
	if statusTool.Name() != "BashStatus" {
		t.Fatalf("unexpected name %q", statusTool.Name())
	}
	if strings.TrimSpace(statusTool.Description()) == "" {
		t.Fatalf("expected non-empty description")
	}
	schema := statusTool.Schema()
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

func TestBashStatusNilContextHandling(t *testing.T) {
	statusTool := NewBashStatusTool()
	if _, err := statusTool.Execute(nil, map[string]interface{}{"task_id": "x"}); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("expected context is nil error, got %v", err)
	}
}

func TestBashStatusCancelledContextReturnsError(t *testing.T) {
	statusTool := NewBashStatusTool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := statusTool.Execute(ctx, map[string]interface{}{"task_id": "x"}); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestBashStatusTaskIDValidation(t *testing.T) {
	statusTool := NewBashStatusTool()
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
			if _, err := statusTool.Execute(ctx, tc.params); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

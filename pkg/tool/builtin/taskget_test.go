package toolbuiltin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTaskGetToolMetadata(t *testing.T) {
	tool := NewTaskGetTool(NewTaskStore())
	if tool.Name() != "TaskGet" {
		t.Fatalf("unexpected name %q", tool.Name())
	}
	if strings.TrimSpace(tool.Description()) == "" {
		t.Fatalf("expected non-empty description")
	}
	schema := tool.Schema()
	if schema == nil || schema.Type != "object" {
		t.Fatalf("unexpected schema %+v", schema)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "taskId" {
		t.Fatalf("unexpected required %+v", schema.Required)
	}
}

func TestTaskGetToolNilContextHandling(t *testing.T) {
	tool := NewTaskGetTool(NewTaskStore())
	if _, err := tool.Execute(nil, map[string]interface{}{"taskId": "x"}); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("expected context is nil error, got %v", err)
	}
}

func TestTaskGetToolCancelledContextReturnsError(t *testing.T) {
	tool := NewTaskGetTool(NewTaskStore())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "x"}); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestTaskGetToolReturnsBlocksAndBlockedBy(t *testing.T) {
	store := NewTaskStore()
	if err := store.Upsert(Task{ID: "t1", Title: "root", Status: TaskStatusInProgress, Owner: "alice"}); err != nil {
		t.Fatalf("seed t1: %v", err)
	}
	if err := store.Upsert(Task{ID: "t2", Title: "child", Status: TaskStatusBlocked, Owner: "bob", BlockedBy: []string{"t1"}}); err != nil {
		t.Fatalf("seed t2: %v", err)
	}
	tool := NewTaskGetTool(store)

	res, err := tool.Execute(context.Background(), map[string]interface{}{"taskId": "t1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res == nil || !res.Success {
		t.Fatalf("unexpected result %+v", res)
	}
	if !strings.Contains(res.Output, "task t1") {
		t.Fatalf("unexpected output:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "blocks:") || !strings.Contains(res.Output, "t2") {
		t.Fatalf("expected blocks info, got:\n%s", res.Output)
	}

	res, err = tool.Execute(context.Background(), map[string]interface{}{"taskId": "t2"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "blockedBy:") || !strings.Contains(res.Output, "t1") {
		t.Fatalf("expected blockedBy info, got:\n%s", res.Output)
	}
}

func TestTaskGetToolValidation(t *testing.T) {
	tool := NewTaskGetTool(NewTaskStore())
	ctx := context.Background()

	cases := []struct {
		name   string
		params map[string]interface{}
		want   string
	}{
		{name: "missing taskId", params: map[string]interface{}{}, want: "taskId is required"},
		{name: "empty taskId", params: map[string]interface{}{"taskId": " "}, want: "taskId cannot be empty"},
		{name: "non-string taskId", params: map[string]interface{}{"taskId": 123}, want: "taskId must be string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tool.Execute(ctx, tc.params); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

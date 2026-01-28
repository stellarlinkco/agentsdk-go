package toolbuiltin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTaskListToolMetadata(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskListTool(store)
	if tool.Name() != "TaskList" {
		t.Fatalf("unexpected name %q", tool.Name())
	}
	if strings.TrimSpace(tool.Description()) == "" {
		t.Fatalf("expected non-empty description")
	}
	schema := tool.Schema()
	if schema == nil || schema.Type != "object" {
		t.Fatalf("unexpected schema %+v", schema)
	}
	if _, ok := schema.Properties["status"]; !ok {
		t.Fatalf("schema missing status")
	}
	if _, ok := schema.Properties["owner"]; !ok {
		t.Fatalf("schema missing owner")
	}
}

func TestTaskListToolNilContextHandling(t *testing.T) {
	tool := NewTaskListTool(NewTaskStore())
	if _, err := tool.Execute(nil, nil); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("expected context is nil error, got %v", err)
	}
}

func TestTaskListToolCancelledContextReturnsError(t *testing.T) {
	tool := NewTaskListTool(NewTaskStore())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, nil); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestTaskListToolFiltersAndShowsDependencies(t *testing.T) {
	store := NewTaskStore()
	if err := store.Upsert(Task{ID: "t1", Title: "root", Status: TaskStatusInProgress, Owner: "alice"}); err != nil {
		t.Fatalf("seed t1: %v", err)
	}
	if err := store.Upsert(Task{ID: "t2", Title: "child", Status: TaskStatusBlocked, Owner: "bob", BlockedBy: []string{"t1"}}); err != nil {
		t.Fatalf("seed t2: %v", err)
	}
	if err := store.Upsert(Task{ID: "t3", Title: "other", Status: TaskStatusPending, Owner: "alice"}); err != nil {
		t.Fatalf("seed t3: %v", err)
	}

	tool := NewTaskListTool(store)
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"owner": "alice",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res == nil || res.Success != true {
		t.Fatalf("unexpected result %+v", res)
	}
	if !strings.Contains(res.Output, "t1") || !strings.Contains(res.Output, "t3") {
		t.Fatalf("expected output to include filtered tasks, got:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "t2") {
		t.Fatalf("did not expect output to include non-matching task t2, got:\n%s", res.Output)
	}

	res, err = tool.Execute(context.Background(), map[string]interface{}{
		"status": TaskStatusBlocked,
	})
	if err != nil {
		t.Fatalf("Execute blocked filter: %v", err)
	}
	if !strings.Contains(res.Output, "t2") {
		t.Fatalf("expected blocked task to be present, got:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "- [in_progress] t1") {
		t.Fatalf("did not expect t1 when filtering blocked, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "blockedBy: t1") {
		t.Fatalf("expected dependency line, got:\n%s", res.Output)
	}
}

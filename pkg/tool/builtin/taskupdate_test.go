package toolbuiltin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestTaskUpdateToolMetadata(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	if tool.Name() != "TaskUpdate" {
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

func TestTaskUpdateCreatesTasksAndMaintainsDependencies(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskUpdateTool(store)
	ctx := context.Background()

	if _, err := tool.Execute(ctx, map[string]interface{}{
		"taskId": "A",
		"status": "in_progress",
		"owner":  "alice",
		"blocks": []interface{}{"B"},
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}

	a, ok := tool.Snapshot("A")
	if !ok {
		t.Fatalf("expected A to exist")
	}
	if a.Status != TaskStatusInProgress || a.Owner != "alice" {
		t.Fatalf("unexpected A state %+v", a)
	}

	b, ok := tool.Snapshot("B")
	if !ok {
		t.Fatalf("expected B to exist")
	}
	if !slices.Equal(b.BlockedBy, []string{"A"}) {
		t.Fatalf("unexpected B blockedBy %v", b.BlockedBy)
	}
	if b.Status != TaskStatusBlocked {
		t.Fatalf("expected B status blocked, got %q", b.Status)
	}

	if _, err := tool.Execute(ctx, map[string]interface{}{
		"taskId":    "A",
		"blockedBy": []interface{}{"C"},
	}); err != nil {
		t.Fatalf("add blocker to A: %v", err)
	}
	a, _ = tool.Snapshot("A")
	if a.Status != TaskStatusBlocked {
		t.Fatalf("expected A status blocked, got %q", a.Status)
	}
	if !slices.Equal(a.BlockedBy, []string{"C"}) {
		t.Fatalf("unexpected A blockedBy %v", a.BlockedBy)
	}
	if _, ok := tool.Snapshot("C"); !ok {
		t.Fatalf("expected C to exist")
	}
}

func TestTaskUpdateRejectsInProgressWhenBlocked(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	ctx := context.Background()

	if _, err := tool.Execute(ctx, map[string]interface{}{
		"taskId":    "A",
		"blockedBy": []string{"B"},
	}); err != nil {
		t.Fatalf("seed blockers: %v", err)
	}

	_, err := tool.Execute(ctx, map[string]interface{}{
		"taskId": "A",
		"status": "in_progress",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestTaskUpdateBlocksReplacementRemovesReverseEdges(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	ctx := context.Background()

	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "A", "blocks": []interface{}{"B"}}); err != nil {
		t.Fatalf("seed blocks: %v", err)
	}
	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "A", "blocks": []interface{}{}}); err != nil {
		t.Fatalf("clear blocks: %v", err)
	}
	b, _ := tool.Snapshot("B")
	if slices.Contains(b.BlockedBy, "A") {
		t.Fatalf("expected reverse edge removed, got %v", b.BlockedBy)
	}
	if b.Status != TaskStatusPending {
		t.Fatalf("expected B status pending, got %q", b.Status)
	}
}

func TestTaskUpdateBlockedByReplacementUpdatesStatus(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	ctx := context.Background()

	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "B", "blockedBy": []string{"A"}}); err != nil {
		t.Fatalf("seed blockedBy: %v", err)
	}
	b, _ := tool.Snapshot("B")
	if b.Status != TaskStatusBlocked {
		t.Fatalf("expected B status blocked, got %q", b.Status)
	}

	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "B", "blockedBy": []string{}}); err != nil {
		t.Fatalf("clear blockedBy: %v", err)
	}
	b, _ = tool.Snapshot("B")
	if len(b.BlockedBy) != 0 {
		t.Fatalf("expected B blockedBy cleared, got %v", b.BlockedBy)
	}
	if b.Status != TaskStatusPending {
		t.Fatalf("expected B status pending, got %q", b.Status)
	}
}

func TestTaskUpdateCompletionUnblocksDownstreamTasks(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	ctx := context.Background()

	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "A", "blocks": []interface{}{"B", "C"}}); err != nil {
		t.Fatalf("seed A blocks: %v", err)
	}
	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "D", "blocks": []interface{}{"B"}}); err != nil {
		t.Fatalf("seed D blocks: %v", err)
	}

	res, err := tool.Execute(ctx, map[string]interface{}{"taskId": "A", "status": "completed"})
	if err != nil {
		t.Fatalf("complete A: %v", err)
	}
	data, ok := res.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected data type %T", res.Data)
	}
	unblocked, _ := data["unblocked"].([]string)
	if !slices.Equal(unblocked, []string{"C"}) {
		t.Fatalf("unexpected unblocked list %v", unblocked)
	}

	b, _ := tool.Snapshot("B")
	if !slices.Equal(b.BlockedBy, []string{"D"}) {
		t.Fatalf("expected B still blocked by D, got %v", b.BlockedBy)
	}
	if b.Status != TaskStatusBlocked {
		t.Fatalf("expected B status blocked, got %q", b.Status)
	}

	c, _ := tool.Snapshot("C")
	if len(c.BlockedBy) != 0 {
		t.Fatalf("expected C blockers cleared, got %v", c.BlockedBy)
	}
	if c.Status != TaskStatusPending {
		t.Fatalf("expected C status pending, got %q", c.Status)
	}
}

func TestTaskUpdateValidation(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	ctx := context.Background()

	_, err := tool.Execute(ctx, nil)
	if err == nil {
		t.Fatalf("expected error for nil params")
	}

	_, err = tool.Execute(ctx, map[string]interface{}{"taskId": "A", "status": "invalid"})
	if err == nil {
		t.Fatalf("expected invalid status error")
	}

	_, err = tool.Execute(ctx, map[string]interface{}{"taskId": "A", "blocks": "nope"})
	if err == nil {
		t.Fatalf("expected invalid blocks type error")
	}
}

func TestTaskUpdateContextCanceledDoesNotMutateStore(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := tool.Execute(ctx, map[string]interface{}{"taskId": "A"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if _, ok := tool.Snapshot("A"); ok {
		t.Fatalf("expected task not to be created when context canceled")
	}
}

func TestTaskUpdateSnapshotNilReceiver(t *testing.T) {
	var tool *TaskUpdateTool
	if _, ok := tool.Snapshot("A"); ok {
		t.Fatalf("expected ok=false")
	}
}

func TestTaskUpdateConcurrentExecutions(t *testing.T) {
	tool := NewTaskUpdateTool(NewTaskStore())
	ctx := context.Background()

	const workers = 8
	const sink = "SINK"

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			taskID := fmt.Sprintf("T-%d", i)
			if _, err := tool.Execute(ctx, map[string]interface{}{
				"taskId": taskID,
				"blocks": []string{sink},
			}); err != nil {
				t.Errorf("worker %d Execute: %v", i, err)
			}
		}()
	}
	wg.Wait()

	sinkTask, ok := tool.Snapshot(sink)
	if !ok {
		t.Fatalf("expected sink task to exist")
	}
	if sinkTask.Status != TaskStatusBlocked {
		t.Fatalf("expected sink status blocked, got %q", sinkTask.Status)
	}
	if len(sinkTask.BlockedBy) != workers {
		t.Fatalf("expected %d blockers, got %d: %v", workers, len(sinkTask.BlockedBy), sinkTask.BlockedBy)
	}
}

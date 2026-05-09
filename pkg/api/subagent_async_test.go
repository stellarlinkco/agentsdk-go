package api

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	hooks "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	"github.com/stellarlinkco/agentsdk-go/pkg/message"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
)

func TestRuntimeBindsSubagentCompletionToHooksAndHistory(t *testing.T) {
	t.Parallel()

	mgr := subagents.NewManager()
	if err := mgr.Register(subagents.Definition{Name: "worker"}, subagents.HandlerFunc(func(ctx context.Context, subCtx subagents.Context, req subagents.Request) (subagents.Result, error) {
		return subagents.Result{Output: strings.Repeat("x", 2100)}, errors.New("boom")
	})); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	var (
		mu     sync.Mutex
		events []hooks.Event
		done   = make(chan hooks.SubagentCompletePayload, 1)
	)
	exec := hooks.NewExecutor(hooks.WithMiddleware(func(next hooks.MiddlewareHandler) hooks.MiddlewareHandler {
		return func(ctx context.Context, evt hooks.Event) error {
			if evt.Type == hooks.SubagentComplete {
				mu.Lock()
				events = append(events, evt)
				mu.Unlock()
				if payload, ok := evt.Payload.(hooks.SubagentCompletePayload); ok {
					select {
					case done <- payload:
					default:
					}
				}
			}
			return next(ctx, evt)
		}
	}))

	rt := &Runtime{
		opts:      Options{subMgr: mgr},
		histories: newHistoryStore(4, nil),
		hooks:     exec,
	}
	rt.bindSubagentCallbacks()

	history, err := rt.histories.Get("sess")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	history.Append(message.Message{Role: "user", Content: "start"})

	taskID, err := mgr.DispatchAsync(subagents.WithContext(context.Background(), subagents.Context{SessionID: "sess"}), "worker", "inspect repo")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}

	var payload hooks.SubagentCompletePayload
	select {
	case payload = <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for subagent complete event")
	}

	if payload.TaskID != taskID {
		t.Fatalf("payload.TaskID = %q, want %q", payload.TaskID, taskID)
	}
	if payload.Name != "worker" || payload.Status != "error" || payload.Error != "boom" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if len(payload.Output) != 2000 {
		t.Fatalf("payload output len = %d, want 2000", len(payload.Output))
	}

	deadline := time.Now().Add(time.Second)
	for history.Len() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if history.Len() != 2 {
		t.Fatalf("history len = %d, want 2", history.Len())
	}

	last, ok := history.Last()
	if !ok {
		t.Fatal("expected injected history message")
	}
	if last.Role != "user" {
		t.Fatalf("last role = %q, want user", last.Role)
	}
	if !strings.Contains(last.Content, "[Subagent Result: worker]\nTask: inspect repo\nStatus: error\nOutput: "+strings.Repeat("x", 2000)+"\nError: boom") {
		t.Fatalf("unexpected summary content: %q", last.Content)
	}
	if last.Metadata["api.synthetic"] != true || last.Metadata["api.synthetic_type"] != "subagent_result" {
		t.Fatalf("missing synthetic marker: %+v", last.Metadata)
	}

	status, err := mgr.TaskStatus(taskID)
	if err != nil {
		t.Fatalf("TaskStatus: %v", err)
	}
	if status.State != subagents.StatusError || status.Error != "boom" {
		t.Fatalf("unexpected task status: %+v", status)
	}

	mu.Lock()
	count := len(events)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("SubagentComplete events = %d, want 1", count)
	}
}

func TestRuntimeCanDisableSubagentSummaryInjection(t *testing.T) {
	t.Parallel()

	mgr := subagents.NewManager()
	if err := mgr.Register(subagents.Definition{Name: "worker"}, subagents.HandlerFunc(func(ctx context.Context, subCtx subagents.Context, req subagents.Request) (subagents.Result, error) {
		return subagents.Result{Output: "done"}, nil
	})); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	rt := &Runtime{
		opts:      Options{DisableSubagentSummary: true, subMgr: mgr},
		histories: newHistoryStore(4, nil),
		hooks:     hooks.NewExecutor(),
	}
	rt.bindSubagentCallbacks()

	history, err := rt.histories.Get("sess")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	history.Append(message.Message{Role: "user", Content: "start"})

	taskID, err := mgr.DispatchAsync(subagents.WithContext(context.Background(), subagents.Context{SessionID: "sess"}), "worker", "inspect repo")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, err := mgr.TaskStatus(taskID)
		if err == nil && (status.State == subagents.StatusSuccess || status.State == subagents.StatusError) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if history.Len() != 1 {
		t.Fatalf("history len = %d, want 1 when summary injection disabled", history.Len())
	}
}

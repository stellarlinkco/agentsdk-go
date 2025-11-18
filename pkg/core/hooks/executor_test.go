package hooks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/core/events"
	"github.com/cexll/agentsdk-go/pkg/core/middleware"
)

func TestExecutorDispatchesAllEventTypes(t *testing.T) {
	t.Parallel()
	hook := newCountingHook(7)
	exec := NewExecutor()
	defer exec.Close()
	exec.Register(hook)

	eventsToSend := []events.Event{
		{Type: events.PreToolUse, Payload: events.ToolUsePayload{Name: "ls"}},
		{Type: events.PostToolUse, Payload: events.ToolResultPayload{Name: "ls"}},
		{Type: events.UserPromptSubmit, Payload: events.UserPromptPayload{Prompt: "hi"}},
		{Type: events.SessionStart, Payload: events.SessionPayload{SessionID: "abc"}},
		{Type: events.Stop, Payload: events.StopPayload{Reason: "done"}},
		{Type: events.SubagentStop, Payload: events.SubagentStopPayload{Name: "sub", Reason: "done"}},
		{Type: events.Notification, Payload: events.NotificationPayload{Message: "note"}},
	}

	for _, evt := range eventsToSend {
		if err := exec.Publish(evt); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	hook.wait(t, time.Second)

	for _, evt := range eventsToSend {
		if hook.count(evt.Type) != 1 {
			t.Fatalf("expected count 1 for %s got %d", evt.Type, hook.count(evt.Type))
		}
	}
}

func TestExecutorErrorIsolation(t *testing.T) {
	t.Parallel()
	errCh := make(chan events.EventType, 4)
	exec := NewExecutor(WithErrorHandler(func(t events.EventType, err error) {
		if err != nil {
			errCh <- t
		}
	}))
	defer exec.Close()
	var wg sync.WaitGroup
	wg.Add(1)

	exec.Register(failingHook{wg: &wg})

	if err := exec.Publish(events.Event{Type: events.PreToolUse, Payload: events.ToolUsePayload{Name: "boom"}}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := exec.Publish(events.Event{Type: events.Stop, Payload: events.StopPayload{Reason: "ok"}}); err != nil {
		t.Fatalf("publish stop: %v", err)
	}

	wg.Wait()
	select {
	case evtType := <-errCh:
		if evtType != events.PreToolUse {
			t.Fatalf("unexpected error type %s", evtType)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected error callback")
	}
}

func TestExecutorTimeoutDoesNotBlock(t *testing.T) {
	t.Parallel()
	fast := make(chan struct{}, 1)
	exec := NewExecutor(WithTimeout(20 * time.Millisecond))
	defer exec.Close()
	exec.Register(timeoutHook{postDone: fast})

	start := time.Now()
	if err := exec.Publish(events.Event{Type: events.PreToolUse, Payload: events.ToolUsePayload{Name: "slow"}}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := exec.Publish(events.Event{Type: events.PostToolUse, Payload: events.ToolResultPayload{Name: "fast"}}); err != nil {
		t.Fatalf("publish post: %v", err)
	}
	select {
	case <-fast:
	case <-time.After(150 * time.Millisecond):
		t.Fatalf("post event blocked by slow hook")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout ineffective, elapsed %v", elapsed)
	}
}

func TestExecutorDedup(t *testing.T) {
	t.Parallel()
	hook := newCountingHook(1)
	exec := NewExecutor()
	defer exec.Close()
	exec.Register(hook)

	evt := events.Event{Type: events.Notification, ID: "dup", Payload: events.NotificationPayload{Message: "dup"}}
	for i := 0; i < 3; i++ {
		if err := exec.Publish(evt); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	hook.wait(t, time.Second)
	if got := hook.count(events.Notification); got != 1 {
		t.Fatalf("expected deduped notification, got %d", got)
	}
}

func TestExecutorMiddlewareErrorPropagation(t *testing.T) {
	t.Parallel()
	var order []string
	errCh := make(chan error, 1)
	exec := NewExecutor(
		WithMiddleware(
			func(next middleware.Handler) middleware.Handler {
				return func(ctx context.Context, evt events.Event) error {
					_ = ctx
					order = append(order, "mw1")
					return next(ctx, evt)
				}
			},
			func(next middleware.Handler) middleware.Handler {
				return func(ctx context.Context, evt events.Event) error {
					_ = ctx
					order = append(order, "mw2")
					return errors.New("mw fail")
				}
			},
		),
		WithErrorHandler(func(t events.EventType, err error) {
			if t == events.SessionStart {
				errCh <- err
			}
		}),
	)
	defer exec.Close()

	if err := exec.Publish(events.Event{Type: events.SessionStart}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case err := <-errCh:
		if err == nil || err.Error() != "mw fail" {
			t.Fatalf("unexpected error %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("middleware error not reported")
	}
	expected := []string{"mw1", "mw2"}
	if fmt.Sprint(order) != fmt.Sprint(expected) {
		t.Fatalf("unexpected order %v", order)
	}
}

func TestExecutorWithCustomBusAndClose(t *testing.T) {
	t.Parallel()
	bus := events.NewBus(events.WithQueueDepth(4))
	exec := NewExecutor(WithBus(bus))
	hook := newCountingHook(1)
	exec.Register(hook)
	if err := exec.Publish(events.Event{Type: events.Notification}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	hook.wait(t, time.Second)
	exec.Close()
	if err := exec.Publish(events.Event{Type: events.Notification}); err == nil {
		t.Fatalf("expected error after close")
	}
}

func TestExecutorNilPublish(t *testing.T) {
	var exec *Executor
	if err := exec.Publish(events.Event{Type: events.Stop}); err == nil {
		t.Fatalf("expected error for nil executor")
	}
}

func TestExecutorHandlesPanicHooks(t *testing.T) {
	t.Parallel()
	errCh := make(chan string, 1)
	exec := NewExecutor(WithErrorHandler(func(t events.EventType, err error) {
		if err != nil {
			errCh <- err.Error()
		}
	}))
	defer exec.Close()
	exec.Register(panicHook{})
	if err := exec.Publish(events.Event{Type: events.Notification}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case msg := <-errCh:
		if msg == "" || !strings.Contains(msg, "panic") {
			t.Fatalf("unexpected panic message %q", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("panic not reported")
	}
}

type countingHook struct {
	wg     sync.WaitGroup
	mu     sync.Mutex
	counts map[events.EventType]int
}

func newCountingHook(total int) *countingHook {
	h := &countingHook{
		counts: make(map[events.EventType]int),
	}
	h.wg.Add(total)
	return h
}

func (h *countingHook) inc(t events.EventType) {
	h.mu.Lock()
	h.counts[t]++
	h.mu.Unlock()
	h.wg.Done()
}

func (h *countingHook) count(t events.EventType) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts[t]
}

func (h *countingHook) wait(t *testing.T, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for hooks")
	}
}

func (h *countingHook) PreToolUse(ctx context.Context, payload events.ToolUsePayload) error {
	_ = ctx
	_ = payload
	h.inc(events.PreToolUse)
	return nil
}

func (h *countingHook) PostToolUse(ctx context.Context, payload events.ToolResultPayload) error {
	_ = ctx
	_ = payload
	h.inc(events.PostToolUse)
	return nil
}

func (h *countingHook) UserPromptSubmit(ctx context.Context, payload events.UserPromptPayload) error {
	_ = ctx
	_ = payload
	h.inc(events.UserPromptSubmit)
	return nil
}

func (h *countingHook) SessionStart(ctx context.Context, payload events.SessionPayload) error {
	_ = ctx
	_ = payload
	h.inc(events.SessionStart)
	return nil
}

func (h *countingHook) Stop(ctx context.Context, payload events.StopPayload) error {
	_ = ctx
	_ = payload
	h.inc(events.Stop)
	return nil
}

func (h *countingHook) SubagentStop(ctx context.Context, payload events.SubagentStopPayload) error {
	_ = ctx
	_ = payload
	h.inc(events.SubagentStop)
	return nil
}

func (h *countingHook) Notification(ctx context.Context, payload events.NotificationPayload) error {
	_ = ctx
	_ = payload
	h.inc(events.Notification)
	return nil
}

type failingHook struct {
	wg *sync.WaitGroup
}

func (f failingHook) PreToolUse(context.Context, events.ToolUsePayload) error {
	return context.Canceled
}

func (f failingHook) Stop(ctx context.Context, payload events.StopPayload) error {
	defer f.wg.Done()
	_ = ctx
	_ = payload
	return nil
}

type timeoutHook struct {
	postDone chan<- struct{}
}

func (t timeoutHook) PreToolUse(context.Context, events.ToolUsePayload) error {
	time.Sleep(150 * time.Millisecond)
	return nil
}

func (t timeoutHook) PostToolUse(context.Context, events.ToolResultPayload) error {
	select {
	case t.postDone <- struct{}{}:
	default:
	}
	return nil
}

type panicHook struct{}

func (panicHook) Notification(context.Context, events.NotificationPayload) error {
	panic("boom")
}

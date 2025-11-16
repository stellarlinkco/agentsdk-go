package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/event"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func TestServerRunEndpoint(t *testing.T) {
	stub := &testAgent{
		runFunc: func(ctx context.Context, input string) (*agent.RunResult, error) {
			return &agent.RunResult{Output: input, StopReason: "complete"}, nil
		},
		runStreamFunc: func(ctx context.Context, input string) (<-chan event.Event, error) {
			ch := make(chan event.Event)
			close(ch)
			return ch, nil
		},
	}
	srv := New(stub)
	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{"input":"demo"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "demo") {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestServerStreamBroadcast(t *testing.T) {
	events := []event.Event{
		event.NewEvent(event.EventProgress, "sess", "ready"),
		event.NewEvent(event.EventCompletion, "sess", nil),
	}
	stub := &testAgent{
		runFunc: func(ctx context.Context, input string) (*agent.RunResult, error) {
			return &agent.RunResult{}, nil
		},
		runStreamFunc: func(ctx context.Context, input string) (<-chan event.Event, error) {
			ch := make(chan event.Event, len(events))
			go func() {
				for _, evt := range events {
					ch <- evt
					time.Sleep(5 * time.Millisecond)
				}
				close(ch)
			}()
			return ch, nil
		},
	}
	srv := New(stub)

	ctxA, cancelA := context.WithCancel(context.Background())
	reqA := httptest.NewRequest(http.MethodGet, "/run/stream", nil).WithContext(ctxA)
	recA := httptest.NewRecorder()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.handleStream(recA, reqA)
	}()

	ctxB, cancelB := context.WithCancel(context.Background())
	reqB := httptest.NewRequest(http.MethodGet, "/run/stream?input=demo", nil).WithContext(ctxB)
	recB := httptest.NewRecorder()
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.handleStream(recB, reqB)
	}()

	time.Sleep(40 * time.Millisecond)
	cancelA()
	cancelB()
	wg.Wait()

	if !strings.Contains(recA.Body.String(), "event: progress") {
		t.Fatalf("watcher missing SSE payload: %s", recA.Body.String())
	}
	if !strings.Contains(recB.Body.String(), "event: progress") {
		t.Fatalf("initiator missing SSE payload: %s", recB.Body.String())
	}
}

func TestServerStreamClientDisconnectCancelsRun(t *testing.T) {
	done := make(chan struct{})
	stub := &testAgent{
		runFunc: func(ctx context.Context, input string) (*agent.RunResult, error) { return &agent.RunResult{}, nil },
		runStreamFunc: func(ctx context.Context, input string) (<-chan event.Event, error) {
			ch := make(chan event.Event)
			go func() {
				<-ctx.Done()
				close(done)
				close(ch)
			}()
			return ch, nil
		},
	}
	srv := New(stub)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/run/stream?input=bye", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.handleStream(rec, req)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run stream context not canceled")
	}
}

type testAgent struct {
	runFunc       func(context.Context, string) (*agent.RunResult, error)
	runStreamFunc func(context.Context, string) (<-chan event.Event, error)
}

func (t *testAgent) Run(ctx context.Context, input string) (*agent.RunResult, error) {
	if t.runFunc == nil {
		return &agent.RunResult{}, nil
	}
	return t.runFunc(ctx, input)
}

func (t *testAgent) RunStream(ctx context.Context, input string) (<-chan event.Event, error) {
	if t.runStreamFunc == nil {
		ch := make(chan event.Event)
		close(ch)
		return ch, nil
	}
	return t.runStreamFunc(ctx, input)
}

func (t *testAgent) AddTool(tool.Tool) error { return nil }

func (t *testAgent) WithHook(agent.Hook) agent.Agent { return t }

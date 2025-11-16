package agent

import (
	"context"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/event"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func TestRunStreamToolProgress(t *testing.T) {
	ag := newTestAgent(t)
	stub := &mockTool{name: "echo", result: &tool.ToolResult{Output: "pong"}}
	if err := ag.AddTool(stub); err != nil {
		t.Fatalf("add tool: %v", err)
	}
	ch, err := ag.RunStream(context.Background(), "tool:echo {}")
	if err != nil {
		t.Fatalf("run stream: %v", err)
	}
	var started, finished bool
	for evt := range ch {
		if evt.Type != event.EventProgress {
			continue
		}
		data, ok := evt.Data.(event.ProgressData)
		if !ok {
			continue
		}
		if data.Stage == "tool:echo" {
			if data.Message == "started" {
				started = true
			}
			if data.Message == "finished" {
				finished = true
			}
		}
	}
	if !started || !finished {
		t.Fatalf("expected tool progress events, started=%v finished=%v", started, finished)
	}
}

func TestRunStreamBackpressureEvent(t *testing.T) {
	ag, err := New(Config{Name: "bp", StreamBuffer: 1, DefaultContext: RunContext{SessionID: "bp"}})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	ch, err := ag.RunStream(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run stream: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	var saw bool
	for evt := range ch {
		if evt.Type != event.EventProgress {
			continue
		}
		data, ok := evt.Data.(event.ProgressData)
		if !ok {
			continue
		}
		if data.Stage == "backpressure" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("expected backpressure progress event")
	}
}

func TestRunStreamCancellation(t *testing.T) {
	ag := newTestAgent(t)
	blocking := &sleepyTool{name: "wait"}
	if err := ag.AddTool(blocking); err != nil {
		t.Fatalf("add tool: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	ch, err := ag.RunStream(ctx, "tool:wait {}")
	if err != nil {
		t.Fatalf("run stream: %v", err)
	}
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	var stopped bool
	for {
		select {
		case <-timeout.C:
			t.Fatal("timed out waiting for stream close")
		case evt, ok := <-ch:
			if !ok {
				if !stopped {
					t.Fatal("missing stopped event before close")
				}
				return
			}
			if evt.Type != event.EventProgress {
				continue
			}
			data, ok := evt.Data.(event.ProgressData)
			if !ok {
				continue
			}
			if data.Stage == "stopped" {
				stopped = true
			}
		}
	}
}

type sleepyTool struct {
	name string
}

func (s *sleepyTool) Name() string             { return s.name }
func (s *sleepyTool) Description() string      { return "sleep until context done" }
func (s *sleepyTool) Schema() *tool.JSONSchema { return nil }

func (s *sleepyTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(50 * time.Millisecond):
		return &tool.ToolResult{}, nil
	}
}

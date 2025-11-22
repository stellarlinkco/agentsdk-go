package api

import (
	"context"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/middleware"
)

func TestChunkString(t *testing.T) {
	chunks := chunkString("helloworld", 4)
	if len(chunks) != 3 || chunks[0] != "hell" || chunks[1] != "owor" || chunks[2] != "ld" {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
	if chunkString("", 4) != nil {
		t.Fatal("expected nil for empty input")
	}
	if chunkString("data", 0) != nil {
		t.Fatal("expected nil for non-positive size")
	}
}

func TestProgressMiddlewareNameAndEmitterGuards(t *testing.T) {
	// Name should be stable to align with middleware chain filtering.
	mw := newProgressMiddleware(nil)
	if mw.Name() != "progress" {
		t.Fatalf("unexpected middleware name: %s", mw.Name())
	}

	// Nil channel should be a no-op.
	(progressEmitter{}).emit(context.Background(), StreamEvent{Type: "noop"})

	// Cancelled context should not block trying to write into the channel.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan StreamEvent)
	progressEmitter{ch: events}.emit(ctx, StreamEvent{Type: "cancelled"})
	select {
	case evt := <-events:
		t.Fatalf("expected no event due to cancellation, got %+v", evt)
	default:
	}
}

func TestProgressMiddlewareEmitsLifecycleEvents(t *testing.T) {
	events := make(chan StreamEvent, 32)
	mw := newProgressMiddleware(events)

	call := agent.ToolCall{ID: "1", Name: "tool", Input: map[string]any{"k": "v"}}
	state := &middleware.State{
		Iteration: 1,
		ModelOutput: &agent.ModelOutput{
			Content:   "ok",
			ToolCalls: []agent.ToolCall{call},
			Done:      true,
		},
	}
	ctx := context.Background()

	if err := mw.BeforeAgent(ctx, state); err != nil {
		t.Fatalf("before agent: %v", err)
	}
	if err := mw.BeforeModel(ctx, state); err != nil {
		t.Fatalf("before model: %v", err)
	}
	if err := mw.AfterModel(ctx, state); err != nil {
		t.Fatalf("after model: %v", err)
	}

	state.ToolCall = call
	state.ToolResult = agent.ToolResult{Output: "out", Metadata: map[string]any{"x": 1}}
	if err := mw.BeforeTool(ctx, state); err != nil {
		t.Fatalf("before tool: %v", err)
	}
	if err := mw.AfterTool(ctx, state); err != nil {
		t.Fatalf("after tool: %v", err)
	}
	if err := mw.AfterAgent(ctx, state); err != nil {
		t.Fatalf("after agent: %v", err)
	}

	close(events)
	found := map[string]bool{}
	for evt := range events {
		found[evt.Type] = true
	}
	for _, typ := range []string{EventAgentStart, EventMessageStart, EventContentBlockDelta, EventToolExecutionResult, EventAgentStop} {
		if !found[typ] {
			t.Fatalf("missing event %s from progress middleware", typ)
		}
	}
}

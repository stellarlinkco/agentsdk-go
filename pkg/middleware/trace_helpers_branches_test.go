package middleware

import (
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestTraceHelpersMoreBranches(t *testing.T) {
	tm := &TraceMiddleware{}
	st := &State{Values: map[string]any{}}
	start := time.Unix(10, 0).UTC()

	tm.trackDuration(StageBeforeAgent, st, start)
	if got := tm.trackDuration(StageAfterAgent, st, start.Add(25*time.Millisecond)); got == 0 {
		t.Fatalf("expected agent duration recorded")
	}

	tm.trackDuration(StageBeforeTool, st, start)
	if got := tm.trackDuration(StageAfterTool, st, start.Add(15*time.Millisecond)); got == 0 {
		t.Fatalf("expected tool duration recorded")
	}

	if got := tm.trackDuration(Stage(999), st, start); got != 0 {
		t.Fatalf("expected unknown stage duration 0, got %d", got)
	}

	if got := usageTotal(nil); got != 0 {
		t.Fatalf("expected zero usage for nil map, got %d", got)
	}
	if got := usageTotal(map[string]any{}); got != 0 {
		t.Fatalf("expected zero usage for missing usage field, got %d", got)
	}
	var nilUsage *model.Usage
	if got := usageTotal(map[string]any{"usage": nilUsage}); got != 0 {
		t.Fatalf("expected zero usage for nil *Usage, got %d", got)
	}
}

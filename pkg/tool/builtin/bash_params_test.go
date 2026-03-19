package toolbuiltin

import (
	"testing"
	"time"
)

func TestParseAsyncFlagAndTaskID(t *testing.T) {
	t.Skip("async/task_id parameters removed from bash tool")
}

func TestDurationFromParamVariants(t *testing.T) {
	if _, err := durationFromParam(-1.0); err == nil {
		t.Fatalf("expected negative duration error")
	}
	if d, err := durationFromParam("2s"); err != nil || d != 2*time.Second {
		t.Fatalf("expected duration 2s, got %v err=%v", d, err)
	}
	if d, err := durationFromParam("1.5"); err != nil || d != time.Duration(1.5*float64(time.Second)) {
		t.Fatalf("expected duration 1.5s, got %v err=%v", d, err)
	}
	if _, err := durationFromParam(struct{}{}); err == nil {
		t.Fatalf("expected unsupported duration type error")
	}
}

func TestResolveRootFallback(t *testing.T) {
	if got := resolveRoot(" "); got == "" {
		t.Fatalf("expected resolved root")
	}
}

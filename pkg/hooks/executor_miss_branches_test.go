package hooks

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func noOutputCommandForShell() string {
	if runtime.GOOS == "windows" {
		return "rem"
	}
	return ":"
}

func invalidJSONCommandForShell() string {
	if runtime.GOOS == "windows" {
		// cmd.exe will echo "{", which is invalid JSON.
		return "echo {"
	}
	return "printf '{'"
}

func TestExecutorExecuteRejectsUnsupportedEventType(t *testing.T) {
	t.Parallel()

	exec := NewExecutor()
	got, err := exec.Execute(context.Background(), Event{Type: EventType("not-a-real-event")})
	if err == nil || !strings.Contains(err.Error(), "unsupported event") {
		t.Fatalf("err=%v, want unsupported event error", err)
	}
	if got != nil {
		t.Fatalf("results=%v, want nil", got)
	}
}

func TestExecutorExecuteReturnsBuildPayloadErrors(t *testing.T) {
	t.Parallel()

	exec := NewExecutor()
	exec.Register(ShellHook{Event: Stop, Command: noOutputCommandForShell()})

	_, err := exec.Execute(context.Background(), Event{
		Type:    Stop,
		Payload: struct{}{},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported payload type") {
		t.Fatalf("err=%v, want unsupported payload type", err)
	}
}

func TestExecutorOnceHooksAreSkippedAfterFirstRun(t *testing.T) {
	t.Parallel()

	exec := NewExecutor()
	exec.Register(
		ShellHook{Event: SessionStart, Command: noOutputCommandForShell()},
		ShellHook{Event: Stop, Command: noOutputCommandForShell(), Once: true},
	)

	evt := Event{
		Type:      Stop,
		SessionID: "sess",
		Payload:   StopPayload{Reason: "hi"},
	}

	first, err := exec.Execute(context.Background(), evt)
	if err != nil {
		t.Fatalf("first Execute err=%v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first results len=%d, want 1", len(first))
	}

	second, err := exec.Execute(context.Background(), evt)
	if err != nil {
		t.Fatalf("second Execute err=%v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second results len=%d, want 0", len(second))
	}
}

func TestExecutorAsyncHooksReportErrorsViaHandler(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	exec := NewExecutor(WithErrorHandler(func(_ EventType, err error) {
		select {
		case errCh <- err:
		default:
		}
	}))
	exec.Register(ShellHook{Event: Stop, Async: true})

	res, err := exec.Execute(context.Background(), Event{
		Type:    Stop,
		Payload: StopPayload{Reason: "hi"},
	})
	if err != nil {
		t.Fatalf("Execute err=%v, want nil", err)
	}
	if len(res) != 0 {
		t.Fatalf("results len=%d, want 0", len(res))
	}

	select {
	case got := <-errCh:
		if got == nil || !strings.Contains(got.Error(), "missing command") {
			t.Fatalf("async err=%v, want missing command", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for async hook error")
	}
}

func TestExecutorExecuteHookRejectsMissingCommand(t *testing.T) {
	t.Parallel()

	exec := NewExecutor()
	exec.Register(ShellHook{Event: Stop})
	_, err := exec.Execute(context.Background(), Event{
		Type:    Stop,
		Payload: StopPayload{Reason: "hi"},
	})
	if err == nil || !strings.Contains(err.Error(), "missing command") {
		t.Fatalf("err=%v, want missing command", err)
	}
}

func TestExecutorExecuteHookRejectsInvalidJSONOutput(t *testing.T) {
	t.Parallel()

	exec := NewExecutor()
	exec.Register(ShellHook{Event: Stop, Command: invalidJSONCommandForShell()})
	_, err := exec.Execute(context.Background(), Event{
		Type:    Stop,
		Payload: StopPayload{Reason: "hi"},
	})
	if err == nil || !strings.Contains(err.Error(), "decode hook output") {
		t.Fatalf("err=%v, want decode hook output error", err)
	}
}

func TestBuildPayloadIncludesSessionStartMetadataAndRejectsMarshalErrors(t *testing.T) {
	t.Parallel()

	payload, err := buildPayload(Event{
		Type: SessionStart,
		Payload: SessionStartPayload{
			SessionID: "sess",
			Source:    "source",
			Model:     "model",
			AgentType: "agent",
			Metadata:  map[string]any{"k": "v"},
		},
	})
	if err != nil {
		t.Fatalf("buildPayload err=%v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := env["metadata"]; got == nil {
		t.Fatalf("metadata missing: %v", env)
	}

	_, err = buildPayload(Event{
		Type: SessionStart,
		Payload: SessionStartPayload{
			SessionID: "sess",
			Metadata:  map[string]any{"bad": func() {}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "marshal payload") {
		t.Fatalf("err=%v, want marshal payload error", err)
	}
}

func TestExtractMatcherTargetFallsThroughToDefaultReturn(t *testing.T) {
	t.Parallel()

	if got := extractMatcherTarget(EventType("unknown"), nil); got != "" {
		t.Fatalf("got=%q, want empty", got)
	}
}

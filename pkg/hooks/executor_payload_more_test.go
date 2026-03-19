package hooks

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEffectiveTimeout(t *testing.T) {
	if got := effectiveTimeout(10*time.Second, 0); got != 10*time.Second {
		t.Fatalf("got=%s", got)
	}
	if got := effectiveTimeout(0, 5*time.Second); got != 5*time.Second {
		t.Fatalf("got=%s", got)
	}
	if got := effectiveTimeout(0, 0); got != defaultHookTimeout {
		t.Fatalf("got=%s", got)
	}
}

func TestBuildPayload_SupportedTypes(t *testing.T) {
	tests := []Event{
		{
			Type:      PreToolUse,
			SessionID: "sess",
			Payload:   ToolUsePayload{Name: "bash", Params: map[string]any{"command": "echo hi"}, ToolUseID: "t1"},
		},
		{
			Type:      PostToolUse,
			SessionID: "sess",
			Payload: ToolResultPayload{
				Name:      "bash",
				Params:    map[string]any{"command": "echo hi"},
				ToolUseID: "t1",
				Result:    map[string]any{"ok": true},
				Duration:  12 * time.Millisecond,
				Err:       errors.New("tool failed"),
			},
		},
		{Type: SubagentStart, Payload: SubagentStartPayload{Name: "agent", AgentID: "id", AgentType: "type", Metadata: map[string]any{"a": 1}}},
		{Type: SubagentStop, Payload: SubagentStopPayload{Name: "agent", AgentID: "id", AgentType: "type", Reason: "done", TranscriptPath: "/tmp/x", StopHookActive: true}},
		{Type: SessionStart, Payload: SessionStartPayload{SessionID: "s", Source: "cli", Model: "m"}},
		{Type: SessionEnd, Payload: SessionEndPayload{SessionID: "s", Reason: "completed", Metadata: map[string]any{"k": "v"}}},
		{Type: Stop, Payload: StopPayload{Reason: "max_iter", StopHookActive: true}},
		{Type: Stop, Payload: nil},
	}
	for _, evt := range tests {
		data, err := buildPayload(evt)
		if err != nil {
			t.Fatalf("buildPayload(%s): %v", evt.Type, err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal(%s): %v", evt.Type, err)
		}
		if got["hook_event_name"] == nil {
			t.Fatalf("%s: missing hook_event_name", evt.Type)
		}
		if got["cwd"] == nil {
			t.Fatalf("%s: missing cwd", evt.Type)
		}
	}
}

func TestBuildPayload_UnsupportedType(t *testing.T) {
	_, err := buildPayload(Event{Type: Stop, Payload: 123})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestExtractMatcherTarget_Cases(t *testing.T) {
	if got := extractMatcherTarget(PreToolUse, ToolUsePayload{Name: "bash"}); got != "bash" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(PostToolUse, ToolResultPayload{Name: "read"}); got != "read" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(SessionStart, SessionStartPayload{Source: "cli"}); got != "cli" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(SessionEnd, SessionEndPayload{Reason: "done"}); got != "done" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(SubagentStart, SubagentStartPayload{Name: "x", AgentType: "type"}); got != "type" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(SubagentStart, SubagentStartPayload{Name: "x"}); got != "x" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(SubagentStop, SubagentStopPayload{Name: "x", AgentType: "type"}); got != "type" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(SubagentStop, SubagentStopPayload{Name: "x"}); got != "x" {
		t.Fatalf("got=%q", got)
	}
	if got := extractMatcherTarget(Stop, StopPayload{Reason: "x"}); got != "" {
		t.Fatalf("got=%q", got)
	}
}

package middleware

import (
	"errors"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

func TestTraceEventPayloadHelpersCoverPointerAndRawBranches(t *testing.T) {
	req := &model.Request{System: "sys", MaxTokens: 1}
	if got := modelRequestPayload(req); got == nil {
		t.Fatalf("expected modelRequestPayload to return payload")
	}

	resp := &model.Response{StopReason: "end_turn"}
	if got := modelResponsePayload(resp); got == nil {
		t.Fatalf("expected modelResponsePayload to return payload")
	}

	if got := modelResponsePayload(map[string]any{"k": "v"}); got == nil || got["k"] != "v" {
		t.Fatalf("got=%v, want map clone", got)
	}

	call := &tool.Call{Name: " tool ", Path: " /x ", Params: map[string]any{"k": "v"}}
	if got := toolCallPayload(call); got == nil || got["name"] != "tool" {
		t.Fatalf("got=%v, want trimmed name", got)
	}

	if got := toolCallPayload(errors.New("boom")); got == nil || got["raw"] == nil {
		t.Fatalf("got=%v, want raw payload", got)
	}

	if got := toolResultPayload(errors.New("boom")); got == nil || got["raw"] == nil {
		t.Fatalf("got=%v, want raw payload", got)
	}
}

func TestTraceEventStructToMapCoversPointerStructBranch(t *testing.T) {
	type sample struct {
		Messages string
	}
	out := structToMap(&sample{Messages: "x"}, map[string]string{"Messages": "messages"})
	if out == nil || out["messages"] == nil {
		t.Fatalf("out=%v, want field mapping", out)
	}
}

func TestTraceEventSnapshotNilBranches(t *testing.T) {
	if got := snapshotModelRequest(nil); got != nil {
		t.Fatalf("got=%v, want nil", got)
	}
	if got := snapshotModelResponse(nil); got != nil {
		t.Fatalf("got=%v, want nil", got)
	}
	if got := snapshotToolCallResult(nil); got != nil {
		t.Fatalf("got=%v, want nil", got)
	}
}

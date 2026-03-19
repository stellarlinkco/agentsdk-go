package main

import (
	"errors"
	"testing"
	"time"

	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestGenRequestID_RandErrorFallsBack(t *testing.T) {
	old := randRead
	t.Cleanup(func() { randRead = old })
	randRead = func([]byte) (int, error) { return 0, errors.New("boom") }

	if got := genRequestID(); got != "req-unknown" {
		t.Fatalf("got=%q", got)
	}
}

func TestClampPreview_Edges(t *testing.T) {
	if got := clampPreview("x", 0); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := clampPreview("  abc  ", 3); got != "abc" {
		t.Fatalf("got=%q", got)
	}
	if got := clampPreview("abcd", 3); got != "abc…" {
		t.Fatalf("got=%q", got)
	}
}

func TestReadStringAndNowOr(t *testing.T) {
	if got := readString(nil, "x"); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := readString(map[string]any{"x": 1}, "x"); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := readString(map[string]any{"x": " y "}, "x"); got != " y " {
		t.Fatalf("got=%q", got)
	}

	now := time.Now()
	stored := now.Add(-time.Second)
	if got := nowOr(stored, now); !got.Equal(stored) {
		t.Fatalf("got=%s want=%s", got, stored)
	}
	if got := nowOr("nope", now); !got.Equal(now) {
		t.Fatalf("got=%s want=%s", got, now)
	}
}

func TestLastUserPrompt_Edges(t *testing.T) {
	if got := lastUserPrompt(nil); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := lastUserPrompt([]modelpkg.Message{{Role: "assistant", Content: "x"}}); got != "" {
		t.Fatalf("got=%q", got)
	}
	msgs := []modelpkg.Message{
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: "  hi  "},
	}
	if got := lastUserPrompt(msgs); got != "hi" {
		t.Fatalf("got=%q", got)
	}
}

func TestLastToolResult_Edges(t *testing.T) {
	if got := lastToolResult(nil); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := lastToolResult([]modelpkg.Message{{Role: "tool"}}, ""); got != "" {
		t.Fatalf("got=%q", got)
	}
	msgs := []modelpkg.Message{
		{Role: "assistant", Content: "x"},
		{
			Role:    "tool",
			Content: "fallback",
			ToolCalls: []modelpkg.ToolCall{
				{Name: "observe_logs", Result: ""},
			},
		},
	}
	if got := lastToolResult(msgs, "OBSERVE_LOGS"); got != "fallback" {
		t.Fatalf("got=%q", got)
	}

	msgs = []modelpkg.Message{
		{
			Role: "tool",
			ToolCalls: []modelpkg.ToolCall{
				{Name: "observe_logs", Result: "ok"},
			},
		},
	}
	if got := lastToolResult(msgs, "observe_logs"); got != "ok" {
		t.Fatalf("got=%q", got)
	}

	msgs = []modelpkg.Message{
		{
			Role:      "tool",
			Content:   "   ",
			ToolCalls: []modelpkg.ToolCall{{Name: "observe_logs", Result: " "}},
		},
	}
	if got := lastToolResult(msgs, "observe_logs"); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestReadStringParam_Edges(t *testing.T) {
	if got := readStringParam(nil, "x"); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := readStringParam(map[string]any{"x": 1}, "x"); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := readStringParam(map[string]any{"x": " y "}, "x"); got != "y" {
		t.Fatalf("got=%q", got)
	}
}

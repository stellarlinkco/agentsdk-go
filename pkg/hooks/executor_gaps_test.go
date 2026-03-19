package hooks

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestNewSelectorRejectsInvalidRegex(t *testing.T) {
	t.Parallel()

	if _, err := NewSelector("(", ""); err == nil || !strings.Contains(err.Error(), "compile tool matcher") {
		t.Fatalf("expected tool matcher compile error, got %v", err)
	}
	if _, err := NewSelector("", "("); err == nil || !strings.Contains(err.Error(), "compile payload matcher") {
		t.Fatalf("expected payload matcher compile error, got %v", err)
	}
}

func TestSelectorMatchReturnsFalseOnMarshalError(t *testing.T) {
	t.Parallel()

	sel := Selector{Pattern: regexp.MustCompile(".*")}
	evt := Event{
		Type: PreToolUse,
		Payload: ToolUsePayload{
			Name:   "Bash",
			Params: map[string]any{"bad": func() {}},
		},
	}
	if sel.Match(evt) {
		t.Fatalf("expected marshal failure to yield no match")
	}
}

func TestExecuteNilContextUsesBackground(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := writeScript(t, dir, "ok.sh", shScript(
		"#!/bin/sh\nexit 0\n",
		"@exit /b 0\r\n",
	))

	exec := NewExecutor()
	exec.Register(ShellHook{Event: Stop, Command: script})

	var ctx context.Context
	results, err := exec.Execute(ctx, Event{Type: Stop})
	if err != nil || len(results) != 1 {
		t.Fatalf("expected success with nil ctx, got results=%d err=%v", len(results), err)
	}
}

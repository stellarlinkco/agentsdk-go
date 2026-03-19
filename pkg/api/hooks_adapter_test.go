package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	hooks "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
)

func TestPreToolUseAllowsInputModification(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Exit 0 with JSON containing hookSpecificOutput.updatedInput
	script := writeScript(t, dir, "modify.sh", shScript(
		"#!/bin/sh\nprintf '{\"hookSpecificOutput\":{\"updatedInput\":{\"k\":\"v2\"}}}'\n",
		"@echo {\"hookSpecificOutput\":{\"updatedInput\":{\"k\":\"v2\"}}}\r\n",
	))

	exec := hooks.NewExecutor()
	exec.Register(hooks.ShellHook{Event: hooks.PreToolUse, Command: script})
	adapter := &runtimeHookAdapter{executor: exec}

	params, err := adapter.PreToolUse(context.Background(), hooks.ToolUsePayload{
		Name:   "Echo",
		Params: map[string]any{"k": "v1"},
	})
	if err != nil {
		t.Fatalf("pre tool use: %v", err)
	}
	if params["k"] != "v2" {
		t.Fatalf("expected modified param, got %+v", params)
	}
}

func TestPreToolUseDeniesExecution(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Exit 0 with JSON decision=deny
	script := writeScript(t, dir, "deny.sh", shScript(
		"#!/bin/sh\nprintf '{\"decision\":\"deny\",\"reason\":\"blocked\"}'\n",
		"@echo {\"decision\":\"deny\",\"reason\":\"blocked\"}\r\n",
	))

	exec := hooks.NewExecutor()
	exec.Register(hooks.ShellHook{Event: hooks.PreToolUse, Command: script})
	adapter := &runtimeHookAdapter{executor: exec}

	_, err := adapter.PreToolUse(context.Background(), hooks.ToolUsePayload{
		Name:   "Echo",
		Params: map[string]any{"k": "v"},
	})
	if err == nil {
		t.Fatalf("expected deny error")
	}
	if !errors.Is(err, ErrToolUseDenied) {
		t.Fatalf("expected ErrToolUseDenied, got %v", err)
	}
}

func TestPreToolUseBlockingError(t *testing.T) {
	t.Parallel()

	// Exit 2 = blocking error
	exec := hooks.NewExecutor()
	exec.Register(hooks.ShellHook{
		Event:   hooks.PreToolUse,
		Command: shCmd("echo blocked >&2; exit 2", "echo blocked >&2 & exit /b 2"),
	})
	adapter := &runtimeHookAdapter{executor: exec}

	_, err := adapter.PreToolUse(context.Background(), hooks.ToolUsePayload{
		Name:   "Echo",
		Params: map[string]any{"k": "v"},
	})
	if err == nil {
		t.Fatalf("expected blocking error")
	}
}

func TestRuntimeHookAdapterNewEventsRecord(t *testing.T) {
	t.Parallel()
	rec := defaultHookRecorder()
	exec := hooks.NewExecutor()
	adapter := &runtimeHookAdapter{executor: exec, recorder: rec}

	if err := adapter.SessionStart(context.Background(), hooks.SessionStartPayload{SessionID: "s"}); err != nil {
		t.Fatalf("session start: %v", err)
	}
	if err := adapter.SessionEnd(context.Background(), hooks.SessionEndPayload{SessionID: "s"}); err != nil {
		t.Fatalf("session end: %v", err)
	}
	if err := adapter.SubagentStart(context.Background(), hooks.SubagentStartPayload{Name: "sa", AgentID: "a1"}); err != nil {
		t.Fatalf("subagent start: %v", err)
	}
	if err := adapter.SubagentStop(context.Background(), hooks.SubagentStopPayload{Name: "sa", AgentID: "a1"}); err != nil {
		t.Fatalf("subagent stop: %v", err)
	}

	drained := rec.Drain()
	want := map[hooks.EventType]bool{
		hooks.SessionStart:  false,
		hooks.SessionEnd:    false,
		hooks.SubagentStart: false,
		hooks.SubagentStop:  false,
	}
	for _, evt := range drained {
		if _, ok := want[evt.Type]; ok {
			want[evt.Type] = true
		}
	}
	for typ, seen := range want {
		if !seen {
			t.Fatalf("expected %s event recorded", typ)
		}
	}
}

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, ".sh") + ".bat"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("chmod script: %v", err)
	}
	return path
}

func shScript(unix, win string) string {
	if runtime.GOOS == "windows" {
		return win
	}
	return unix
}

func shCmd(unix, win string) string {
	if runtime.GOOS == "windows" {
		return win
	}
	return unix
}

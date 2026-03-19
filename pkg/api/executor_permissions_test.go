package api

import (
	"context"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
)

func TestRuntimeToolExecutorIsAllowedRespectsWhitelists(t *testing.T) {
	exec := runtimeToolExecutor{allow: map[string]struct{}{"echo": {}}}

	ctxWithAllowed := subagents.WithContext(context.Background(), subagents.Context{ToolWhitelist: []string{"echo"}})
	if !exec.isAllowed(ctxWithAllowed, "echo") {
		t.Fatal("expected tool allowed when present in both whitelists")
	}

	ctxDenied := subagents.WithContext(context.Background(), subagents.Context{ToolWhitelist: []string{"other"}})
	if exec.isAllowed(ctxDenied, "echo") {
		t.Fatal("expected tool denied when subagent whitelist excludes it")
	}

	exec.allow = nil
	if !exec.isAllowed(context.Background(), "any") {
		t.Fatal("nil runtime allowlist should permit tool")
	}
	if exec.isAllowed(context.Background(), "   ") {
		t.Fatal("blank tool name should be rejected")
	}
}

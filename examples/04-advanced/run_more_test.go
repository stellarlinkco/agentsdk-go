package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

func TestRun_AllFeaturesEnabled(t *testing.T) {
	requireAPIKey(t)

	oldNew := newAPIRuntime
	newAPIRuntime = func(context.Context, api.Options) (advancedRuntime, error) { return stubRuntime{}, nil }
	t.Cleanup(func() { newAPIRuntime = oldNew })

	wd := t.TempDir()
	traceDir := t.TempDir()

	cfg := runConfig{
		prompt:            "incident log error deploy",
		sessionID:         "advanced-demo-test",
		owner:             "advanced-example-test",
		projectRoot:       wd,
		enableHooks:       false,
		enableMCP:         false,
		enableSandbox:     true,
		sandboxRoot:       "",
		allowHost:         "example.com",
		cpuLimit:          1,
		memLimit:          64 * 1024 * 1024,
		diskLimit:         16 * 1024 * 1024,
		enableSkills:      true,
		enableSubagents:   true,
		enableTrace:       true,
		traceDir:          traceDir,
		traceSkills:       true,
		slowThreshold:     time.Millisecond,
		toolLatency:       0,
		runTimeout:        2 * time.Second,
		middlewareTimeout: 2 * time.Second,
		maxIterations:     3,
		rps:               100,
		burst:             100,
		concurrent:        10,
		hookTimeout:       0,
		forceSkill:        "add-note",
		targetSubagent:    "plan",
		severity:          "high",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := run(ctx, cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestSecurityMiddleware_Errors(t *testing.T) {
	mw := newSecurityMiddleware([]string{"drop table"}, nil).(*securityMiddleware)

	ctx := context.Background()
	st := &middleware.State{Values: map[string]any{promptKey: "drop table users"}}
	if err := mw.BeforeAgent(ctx, st); err == nil {
		t.Fatalf("expected blocked phrase error")
	}

	st = &middleware.State{Values: map[string]any{promptKey: "ok"}}
	st.ToolCall = model.ToolCall{Name: "observe_logs", Arguments: map[string]any{}}
	if err := mw.BeforeTool(ctx, st); err == nil {
		t.Fatalf("expected missing query error")
	}

	st = &middleware.State{Values: map[string]any{promptKey: "ok"}}
	st.ToolCall = model.ToolCall{Name: "observe_logs", Arguments: map[string]any{"query": "hi"}}
	st.ToolResult = &tool.CallResult{Call: tool.Call{Name: "observe_logs"}, Result: &tool.ToolResult{Output: "drop table users"}}
	if err := mw.AfterTool(ctx, st); err == nil {
		t.Fatalf("expected blocked output error")
	}
}

func TestClampPreview(t *testing.T) {
	if got := clampPreview("  abc  ", 2); !strings.HasPrefix(got, "ab") {
		t.Fatalf("got=%q", got)
	}
	if got := clampPreview("", 2); got != "" {
		t.Fatalf("got=%q", got)
	}
}

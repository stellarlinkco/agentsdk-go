package main

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	"github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
)

func TestPrintSummary_CoversAllSections(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := runConfig{
		enableHooks:     true,
		enableSkills:    true,
		enableSubagents: true,
		enableSandbox:   true,
		enableTrace:     true,
		traceDir:        "trace-out",
	}

	mw := buildMiddlewares(cfg, logger)
	hookBundle := buildHooks(logger, 0)
	hookBundle.tracker.record(hooks.PreToolUse)

	resp := &api.Response{
		Result: &api.Result{
			Output: "ok",
			ToolCalls: []model.ToolCall{
				{Name: "observe_logs", Arguments: map[string]any{"query": "x"}},
			},
		},
		SkillResults: []api.SkillExecution{
			{
				Definition: skills.Definition{Name: "add-note"},
				Result:     skills.Result{Output: "note"},
			},
		},
		Subagent: &subagents.Result{Subagent: "plan", Output: "plan-output"},
		HookEvents: []hooks.Event{
			{ID: "evt1", Type: hooks.Stop, Timestamp: time.Now()},
		},
		Settings: &config.Settings{Env: map[string]string{"ADVANCED_EXAMPLE": "true"}},
		SandboxSnapshot: api.SandboxReport{
			Roots:          []string{"/tmp"},
			AllowedPaths:   []string{"/tmp"},
			AllowedDomains: []string{"example.com"},
		},
	}

	printSummary(resp, cfg, mw, hookBundle)
}

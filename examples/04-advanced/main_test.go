package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
)

func TestRunOfflineMinimal(t *testing.T) {
	requireAPIKey(t)

	oldNew := newAPIRuntime
	newAPIRuntime = func(context.Context, api.Options) (advancedRuntime, error) { return stubRuntime{}, nil }
	t.Cleanup(func() { newAPIRuntime = oldNew })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	cfg := runConfig{
		prompt:            "生成一份安全巡检摘要并标注下一步",
		sessionID:         "advanced-demo-test",
		owner:             "advanced-example-test",
		projectRoot:       wd,
		enableHooks:       false,
		enableMCP:         false,
		enableSandbox:     false,
		enableSkills:      false,
		enableSubagents:   false,
		enableTrace:       false,
		traceDir:          t.TempDir(),
		slowThreshold:     250 * time.Millisecond,
		toolLatency:       0,
		runTimeout:        2 * time.Second,
		middlewareTimeout: 2 * time.Second,
		maxIterations:     3,
		rps:               100,
		burst:             100,
		concurrent:        10,
		hookTimeout:       0,
		forceSkill:        "",
		targetSubagent:    "",
		severity:          "high",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
}

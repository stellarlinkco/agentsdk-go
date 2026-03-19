package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	hookspkg "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
)

func TestRun_ResolveWorkingDirError(t *testing.T) {
	requireAPIKey(t)

	old := osGetwd
	t.Cleanup(func() { osGetwd = old })
	osGetwd = func() (string, error) { return "", errors.New("wd boom") }

	err := run(context.Background(), runConfig{projectRoot: ""})
	if err == nil || !strings.Contains(err.Error(), "resolve working dir:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_ProjectRootAbsError(t *testing.T) {
	requireAPIKey(t)

	old := filepathAbs
	t.Cleanup(func() { filepathAbs = old })
	filepathAbs = func(string) (string, error) { return "", errors.New("abs boom") }

	err := run(context.Background(), runConfig{projectRoot: "x"})
	if err == nil || !strings.Contains(err.Error(), "resolve project root:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_DefaultProjectRootUsesGetwd(t *testing.T) {
	requireAPIKey(t)

	oldGetwd := osGetwd
	oldNew := newAPIRuntime
	t.Cleanup(func() {
		osGetwd = oldGetwd
		newAPIRuntime = oldNew
	})
	osGetwd = func() (string, error) { return t.TempDir(), nil }
	newAPIRuntime = func(context.Context, api.Options) (advancedRuntime, error) { return nil, errors.New("new boom") }

	err := run(context.Background(), runConfig{projectRoot: "", prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "build runtime:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_BuildRuntimeError(t *testing.T) {
	requireAPIKey(t)

	old := newAPIRuntime
	t.Cleanup(func() { newAPIRuntime = old })
	newAPIRuntime = func(context.Context, api.Options) (advancedRuntime, error) { return nil, errors.New("new boom") }

	err := run(context.Background(), runConfig{projectRoot: t.TempDir(), prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "build runtime:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_RunAgentError(t *testing.T) {
	requireAPIKey(t)

	oldNew := newAPIRuntime
	newAPIRuntime = func(context.Context, api.Options) (advancedRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("run boom")
		}}, nil
	}
	t.Cleanup(func() { newAPIRuntime = oldNew })

	cfg := runConfig{
		prompt:            "x",
		sessionID:         "s",
		owner:             "o",
		projectRoot:       t.TempDir(),
		enableHooks:       false,
		enableMCP:         false,
		enableSandbox:     false,
		enableSkills:      false,
		enableSubagents:   false,
		enableTrace:       false,
		traceDir:          t.TempDir(),
		slowThreshold:     time.Second,
		toolLatency:       0,
		runTimeout:        10 * time.Second,
		middlewareTimeout: time.Second,
		maxIterations:     1,
		rps:               100,
		burst:             100,
		concurrent:        1,
		hookTimeout:       0,
		severity:          "high",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, cfg); err == nil || !strings.Contains(err.Error(), "run agent:") {
		t.Fatalf("err=%v", err)
	}
}

func TestMain_FatalsOnRunError(t *testing.T) {
	requireAPIKey(t)

	origArgs := os.Args
	origFS := flag.CommandLine
	oldFatal := advancedFatal
	oldAbs := filepathAbs
	t.Cleanup(func() {
		os.Args = origArgs
		flag.CommandLine = origFS
		advancedFatal = oldFatal
		filepathAbs = oldAbs
	})

	flag.CommandLine = flag.NewFlagSet("advanced", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	called := false
	advancedFatal = func(...any) { called = true }
	filepathAbs = func(string) (string, error) { return "", errors.New("abs boom") }

	os.Args = []string{"advanced", "--project-root=/tmp"}
	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

func TestPrintSummary_ExercisesBranches(t *testing.T) {
	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		_ = w.Close()
		_, _ = io.ReadAll(r)
		_ = r.Close()
	}()

	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := buildMiddlewares(runConfig{prompt: "p", owner: "o", slowThreshold: 0}, logger)
	hookBundle := buildHooks(logger, 0)
	hookBundle.tracker.record(hookspkg.PreToolUse)

	printSummary(nil, runConfig{}, mw, hookBundle)

	settings := config.GetDefaultSettings()
	if settings.Env == nil {
		settings.Env = map[string]string{}
	}
	settings.Env["ADVANCED_EXAMPLE"] = "true"
	resp := &api.Response{
		Result: &api.Result{
			Output: "ok",
			ToolCalls: []modelpkg.ToolCall{
				{Name: "observe_logs", Arguments: map[string]any{"query": "x"}},
			},
		},
		SkillResults: []api.SkillExecution{
			{Definition: skills.Definition{Name: "add-note"}, Result: skills.Result{Output: "note"}, Err: nil},
		},
		Subagent:   &subagents.Result{Subagent: "plan", Output: "plan-output"},
		HookEvents: []hookspkg.Event{{Type: hookspkg.Stop}},
		Settings:   &settings,
	}

	cfg := runConfig{
		enableSkills:    true,
		enableSubagents: true,
		enableHooks:     true,
		enableSandbox:   true,
		enableTrace:     true,
	}
	mw.traceDir = "trace-out"
	printSummary(resp, cfg, mw, hookBundle)
}

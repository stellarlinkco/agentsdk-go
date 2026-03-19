package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestNewDemoModel_DefaultSettingsAndFields(t *testing.T) {
	mdl := newDemoModel("/root", "owner", true, nil)
	got, ok := mdl.(*demoModel)
	if !ok {
		t.Fatalf("type=%T", mdl)
	}
	if got.projectRoot != "/root" || got.owner != "owner" || got.useMCP != true {
		t.Fatalf("model=%+v", got)
	}
	if got.settings == nil {
		t.Fatalf("expected settings")
	}
}

func TestDemoModel_Complete_NilReceiver(t *testing.T) {
	var m *demoModel
	_, err := m.Complete(context.Background(), modelpkg.Request{Messages: []modelpkg.Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestDemoModel_Complete_EmptyPrompt(t *testing.T) {
	settings := config.GetDefaultSettings()
	m := &demoModel{projectRoot: "x", owner: "y", useMCP: false, settings: &settings}
	_, err := m.Complete(context.Background(), modelpkg.Request{Messages: []modelpkg.Message{{Role: "assistant", Content: "x"}}})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestDemoModel_Complete_ToolCallFlow(t *testing.T) {
	settings := config.GetDefaultSettings()
	if settings.Env == nil {
		settings.Env = map[string]string{}
	}
	settings.Env["ADVANCED_EXAMPLE"] = "true"
	m := &demoModel{projectRoot: "/p", owner: "o", useMCP: true, settings: &settings}

	resp, err := m.Complete(context.Background(), modelpkg.Request{Messages: []modelpkg.Message{{Role: "user", Content: "scan"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.StopReason != "tool_call" {
		t.Fatalf("StopReason=%q", resp.StopReason)
	}
	if !strings.Contains(resp.Message.Content, "ADVANCED_EXAMPLE=true") {
		t.Fatalf("content=%q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 2 {
		t.Fatalf("tool_calls=%v", resp.Message.ToolCalls)
	}
}

func TestDemoModel_Complete_SummaryFlow(t *testing.T) {
	settings := config.GetDefaultSettings()
	m := &demoModel{projectRoot: "/p", owner: "o", useMCP: true, settings: &settings}

	msgs := []modelpkg.Message{
		{Role: "user", Content: "scan"},
		{
			Role: "tool",
			ToolCalls: []modelpkg.ToolCall{
				{Name: "observe_logs", Result: "logs-ok"},
			},
		},
		{
			Role: "tool",
			ToolCalls: []modelpkg.ToolCall{
				{Name: "get_current_time", Result: "2026-01-01"},
			},
		},
	}
	resp, err := m.Complete(context.Background(), modelpkg.Request{Messages: msgs})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.StopReason != "done" {
		t.Fatalf("StopReason=%q", resp.StopReason)
	}
	if !strings.Contains(resp.Message.Content, "logs-ok") || !strings.Contains(resp.Message.Content, "2026-01-01") {
		t.Fatalf("content=%q", resp.Message.Content)
	}
}

func TestDemoModel_CompleteStream_CallbackPaths(t *testing.T) {
	settings := config.GetDefaultSettings()
	m := &demoModel{projectRoot: "/p", owner: "o", useMCP: false, settings: &settings}
	req := modelpkg.Request{Messages: []modelpkg.Message{{Role: "user", Content: "scan"}}}

	if err := m.CompleteStream(context.Background(), req, nil); err != nil {
		t.Fatalf("CompleteStream(nil): %v", err)
	}

	called := false
	err := m.CompleteStream(context.Background(), req, func(res modelpkg.StreamResult) error {
		called = true
		if !res.Final || res.Response == nil {
			t.Fatalf("res=%+v", res)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CompleteStream(cb): %v", err)
	}
	if !called {
		t.Fatalf("expected callback")
	}
}

func TestNewObserveLogsTool_SettingsDefaultAndExecute(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))

	toolAny := newObserveLogsTool(0, logger, nil)
	tl, ok := toolAny.(*observeLogsTool)
	if !ok {
		t.Fatalf("type=%T", toolAny)
	}
	if tl.settings == nil {
		t.Fatalf("expected settings")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tl = newObserveLogsTool(50*time.Millisecond, logger, tl.settings).(*observeLogsTool)
	if _, err := tl.Execute(ctx, map[string]any{"query": "x", "project_root": "/p"}); err == nil {
		t.Fatalf("expected ctx error")
	}

	settings := config.GetDefaultSettings()
	if settings.Env == nil {
		settings.Env = map[string]string{}
	}
	settings.Env["ADVANCED_EXAMPLE"] = "on"
	toolAny = newObserveLogsTool(0, logger, &settings)
	tl = toolAny.(*observeLogsTool)
	res, err := tl.Execute(context.Background(), map[string]any{"query": "q", "project_root": "/p", "owner": "o"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res == nil || !res.Success {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(res.Output, "查询: q") || !strings.Contains(res.Output, "ADVANCED_EXAMPLE=on") || !strings.Contains(res.Output, "owner=o") {
		t.Fatalf("output=%q", res.Output)
	}
}

func TestObserveLogsTool_Execute_WaitsAndCanTimeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	settings := config.GetDefaultSettings()
	tl := newObserveLogsTool(50*time.Millisecond, logger, &settings).(*observeLogsTool)

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := tl.Execute(ctx, map[string]any{"query": "q", "project_root": "/p"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestDemoModel_CompleteStream_PropagatesCompleteError(t *testing.T) {
	settings := config.GetDefaultSettings()
	m := &demoModel{projectRoot: "/p", owner: "o", useMCP: false, settings: &settings}

	called := false
	err := m.CompleteStream(context.Background(), modelpkg.Request{}, func(modelpkg.StreamResult) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if called {
		t.Fatalf("unexpected callback")
	}
}

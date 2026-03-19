package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestLoggingMiddleware_NilValuesAndNilModelOutput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newLoggingMiddleware(logger).(*loggingMiddleware)

	st := &middleware.State{Values: nil}
	if err := mw.BeforeAgent(context.Background(), st); err != nil {
		t.Fatalf("BeforeAgent: %v", err)
	}
	if st.Values == nil || st.Values[requestIDKey] == nil || st.Values[startedAtKey] == nil {
		t.Fatalf("values=%v", st.Values)
	}

	st.ModelOutput = nil
	if err := mw.AfterAgent(context.Background(), st); err != nil {
		t.Fatalf("AfterAgent: %v", err)
	}

	st.Values[startedAtKey] = "not-time"
	st.Iteration = 0
	if err := mw.AfterAgent(context.Background(), st); err != nil {
		t.Fatalf("AfterAgent: %v", err)
	}
}

func TestRateLimitMiddleware_Branches(t *testing.T) {
	rl := newRateLimitMiddleware(100, 100, 1)

	ctx := context.Background()
	if err := rl.BeforeAgent(ctx, &middleware.State{}); err != nil {
		t.Fatalf("BeforeAgent #1: %v", err)
	}
	if err := rl.BeforeAgent(ctx, &middleware.State{}); err == nil || !strings.Contains(err.Error(), "concurrent limit") {
		t.Fatalf("BeforeAgent #2 err=%v", err)
	}
	_ = rl.AfterAgent(ctx, &middleware.State{})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	rl = newRateLimitMiddleware(100, 100, 1)
	if err := rl.BeforeAgent(cancelled, &middleware.State{}); err == nil {
		t.Fatalf("expected ctx error")
	}
}

func TestLoggingMiddleware_AfterModel_WithContentBlocks(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newLoggingMiddleware(logger).(*loggingMiddleware)

	out := &model.Response{Message: model.Message{Role: "assistant", Content: "hello", ToolCalls: []model.ToolCall{{Name: "observe_logs"}}}}

	st := &middleware.State{
		Values:      map[string]any{requestIDKey: "r1", startedAtKey: time.Now()},
		ModelOutput: out,
		Iteration:   1,
	}
	if err := mw.AfterAgent(context.Background(), st); err != nil {
		t.Fatalf("AfterAgent: %v", err)
	}
}

func TestMiddleware_Names(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if got := newLoggingMiddleware(logger).Name(); got != "logging" {
		t.Fatalf("got=%q", got)
	}
	if got := newRateLimitMiddleware(1, 1, 1).Name(); got != "ratelimit" {
		t.Fatalf("got=%q", got)
	}
	if got := newSecurityMiddleware(nil, logger).Name(); got != "security" {
		t.Fatalf("got=%q", got)
	}
	if got := newMonitoringMiddleware(time.Millisecond, logger).Name(); got != "monitoring" {
		t.Fatalf("got=%q", got)
	}
	if got := newSettingsMiddleware("p", "o", logger).Name(); got != "settings" {
		t.Fatalf("got=%q", got)
	}
}

func TestRateLimitMiddleware_CtxDoneBranchInSelect(t *testing.T) {
	rl := newRateLimitMiddleware(100, 100, 1)
	if err := rl.BeforeAgent(context.Background(), &middleware.State{}); err != nil {
		t.Fatalf("BeforeAgent #1: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rl.BeforeAgent(ctx, &middleware.State{}); err == nil {
		t.Fatalf("expected ctx error")
	}
}

func TestRateLimitMiddleware_WaitForToken_SlowPath(t *testing.T) {
	rl := newRateLimitMiddleware(5, 5, 1)
	rl.tokens = 0
	rl.ratePerSec = 0
	rl.burst = 0
	rl.lastRefill = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := rl.waitForToken(ctx); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRateLimitMiddleware_TryConsume_Branches(t *testing.T) {
	rl := newRateLimitMiddleware(5, 5, 1)
	rl.tokens = 0
	rl.lastRefill = time.Now().Add(time.Second)
	if rl.tryConsume() {
		t.Fatalf("expected false")
	}

	rl = newRateLimitMiddleware(5, 1, 1)
	rl.tokens = 100
	rl.lastRefill = time.Now().Add(-time.Second)
	if !rl.tryConsume() {
		t.Fatalf("expected true")
	}
	if rl.tokens > rl.burst {
		t.Fatalf("tokens=%v burst=%v", rl.tokens, rl.burst)
	}
}

func TestSecurityMiddleware_BeforeAgent_Edges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newSecurityMiddleware([]string{"drop table"}, logger).(*securityMiddleware)

	st := &middleware.State{Values: map[string]any{}}
	if err := mw.BeforeAgent(context.Background(), st); err == nil {
		t.Fatalf("expected error")
	}

	st = &middleware.State{Values: map[string]any{promptKey: "ok"}}
	if err := mw.BeforeAgent(context.Background(), st); err != nil {
		t.Fatalf("BeforeAgent: %v", err)
	}
}

func TestSecurityMiddleware_AfterModel_BlocksOutput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newSecurityMiddleware([]string{"drop table"}, logger).(*securityMiddleware)

	st := &middleware.State{
		Values:      map[string]any{},
		ModelOutput: &model.Response{Message: model.Message{Role: "assistant", Content: "drop table users"}},
	}
	if err := mw.AfterAgent(context.Background(), st); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMonitoringMiddleware_SnapshotAndWarnings(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newMonitoringMiddleware(0, logger)

	st := &middleware.State{
		Values:    map[string]any{},
		Iteration: 0,
	}
	st.Values["monitoring.start"] = time.Now().Add(-time.Millisecond)
	st.Values[modelKey(0)] = time.Now().Add(-time.Millisecond)
	st.Values[toolKey(0)] = time.Now().Add(-time.Millisecond)

	_ = mw.AfterAgent(context.Background(), st)
	_ = mw.AfterTool(context.Background(), st)
	_ = mw.AfterAgent(context.Background(), st)

	total, slow, _, _ := mw.Snapshot()
	if total == 0 || slow == 0 {
		t.Fatalf("total=%d slow=%d", total, slow)
	}

	var nilMW *monitoringMiddleware
	if total, slow, max, last := nilMW.Snapshot(); total != 0 || slow != 0 || max != 0 || last != 0 {
		t.Fatalf("snapshot=%v %v %v %v", total, slow, max, last)
	}

	empty := &monitoringMiddleware{metrics: nil}
	if total, slow, max, last := empty.Snapshot(); total != 0 || slow != 0 || max != 0 || last != 0 {
		t.Fatalf("snapshot=%v %v %v %v", total, slow, max, last)
	}
}

func TestSettingsMiddleware_BeforeAgent_Edges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newSettingsMiddleware("prompt", "owner", logger).(middleware.Funcs)

	st := &middleware.State{Values: nil, Agent: nil}
	if err := mw.BeforeAgent(context.Background(), st); err != nil {
		t.Fatalf("BeforeAgent: %v", err)
	}
	if st.Values[promptKey] != "prompt" {
		t.Fatalf("values=%v", st.Values)
	}
	if _, ok := st.Values["settings.env"].(map[string]string); !ok {
		t.Fatalf("settings.env=%T", st.Values["settings.env"])
	}

	st = &middleware.State{Values: map[string]any{}, Agent: "not-agent"}
	if err := mw.BeforeAgent(context.Background(), st); err != nil {
		t.Fatalf("BeforeAgent: %v", err)
	}
}

func TestSecurityMiddleware_BeforeTool_NotesFlag(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newSecurityMiddleware(nil, logger).(*securityMiddleware)

	st := &middleware.State{Values: map[string]any{}, ToolCall: model.ToolCall{Name: "observe_logs", Arguments: map[string]any{"query": "ok"}}}
	if err := mw.BeforeTool(context.Background(), st); err != nil {
		t.Fatalf("BeforeTool: %v", err)
	}
	flags, _ := st.Values[securityFlagsKey].([]string)
	if len(flags) == 0 {
		t.Fatalf("flags=%v", flags)
	}

	st = &middleware.State{Values: map[string]any{}, ToolCall: model.ToolCall{Name: "observe_logs", Arguments: map[string]any{}}}
	if err := mw.BeforeTool(context.Background(), st); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMonitoringMiddleware_UsesFallbackTimes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mw := newMonitoringMiddleware(time.Second, logger)
	st := &middleware.State{Values: map[string]any{}, Iteration: 1}
	st.Values[modelKey(1)] = "not-time"
	st.Values[toolKey(1)] = "not-time"
	if err := mw.AfterAgent(context.Background(), st); err != nil {
		t.Fatalf("AfterAgent: %v", err)
	}
	if err := mw.AfterTool(context.Background(), st); err != nil {
		t.Fatalf("AfterTool: %v", err)
	}
}

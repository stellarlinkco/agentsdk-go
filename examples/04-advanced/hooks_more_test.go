package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/hooks"
)

func TestDemoHooks_RecordAndSnapshot(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{}))
	h := newDemoHooks(logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = h.PreToolUse(ctx, hooks.ToolUsePayload{Name: "t", Params: map[string]any{"k": "v"}})
	_ = h.PostToolUse(ctx, hooks.ToolResultPayload{Name: "t"})
	_ = h.Stop(ctx, hooks.StopPayload{Reason: "r"})

	got := h.snapshot()
	if got[hooks.PreToolUse] != 1 || got[hooks.PostToolUse] != 1 || got[hooks.Stop] != 1 {
		t.Fatalf("snapshot=%v", got)
	}
}

func TestHookMiddlewares_InvokeNext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{}))

	cases := []struct {
		name string
		mw   hooks.Middleware
	}{
		{name: "log", mw: logEventMiddleware(logger)},
		{name: "timing", mw: timingMiddleware(logger)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			want := errors.New("boom")
			next := func(context.Context, hooks.Event) error {
				called++
				return want
			}
			h := tc.mw(next)
			err := h(context.Background(), hooks.Event{Type: hooks.PreToolUse, ID: "id"})
			if !errors.Is(err, want) {
				t.Fatalf("err=%v", err)
			}
			if called != 1 {
				t.Fatalf("called=%d", called)
			}
		})
	}
}

func TestBuildHooksBundle(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{}))
	b := buildHooks(logger, 10*time.Millisecond)
	if b.tracker == nil {
		t.Fatalf("expected tracker")
	}
	if len(b.mw) != 2 {
		t.Fatalf("mw=%v", b.mw)
	}
}

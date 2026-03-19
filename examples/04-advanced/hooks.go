package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/hooks"
)

type hookBundle struct {
	handlers []any
	mw       []hooks.Middleware
	tracker  *demoHooks
}

func buildHooks(logger *slog.Logger, timeout time.Duration) hookBundle {
	tracker := newDemoHooks(logger)
	mw := []hooks.Middleware{logEventMiddleware(logger), timingMiddleware(logger)}
	return hookBundle{handlers: []any{tracker}, mw: mw, tracker: tracker}
}

type demoHooks struct {
	logger *slog.Logger
	mu     sync.Mutex
	counts map[hooks.EventType]int
}

func newDemoHooks(logger *slog.Logger) *demoHooks {
	return &demoHooks{logger: logger, counts: map[hooks.EventType]int{}}
}

func (h *demoHooks) snapshot() map[hooks.EventType]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[hooks.EventType]int, len(h.counts))
	for k, v := range h.counts {
		out[k] = v
	}
	return out
}

func (h *demoHooks) record(t hooks.EventType) {
	h.mu.Lock()
	h.counts[t]++
	h.mu.Unlock()
}

func (h *demoHooks) PreToolUse(ctx context.Context, payload hooks.ToolUsePayload) error {
	h.logger.Info("hook PreToolUse", "tool", payload.Name, "params", payload.Params)
	h.record(hooks.PreToolUse)
	return ctx.Err()
}

func (h *demoHooks) PostToolUse(ctx context.Context, payload hooks.ToolResultPayload) error {
	h.logger.Info("hook PostToolUse", "tool", payload.Name, "duration", payload.Duration, "err", payload.Err)
	h.record(hooks.PostToolUse)
	return ctx.Err()
}

func (h *demoHooks) Stop(ctx context.Context, payload hooks.StopPayload) error {
	h.logger.Info("hook Stop", "reason", payload.Reason)
	h.record(hooks.Stop)
	return ctx.Err()
}

func logEventMiddleware(logger *slog.Logger) hooks.Middleware {
	return func(next hooks.MiddlewareHandler) hooks.MiddlewareHandler {
		return func(ctx context.Context, evt hooks.Event) error {
			logger.Info("hook middleware", "event", evt.Type, "id", evt.ID)
			return next(ctx, evt)
		}
	}
}

func timingMiddleware(logger *slog.Logger) hooks.Middleware {
	return func(next hooks.MiddlewareHandler) hooks.MiddlewareHandler {
		return func(ctx context.Context, evt hooks.Event) error {
			start := time.Now()
			err := next(ctx, evt)
			logger.Info("hook timing", "event", evt.Type, "took", time.Since(start))
			return err
		}
	}
}

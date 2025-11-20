package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cexll/agentsdk-go/pkg/core/events"
	"github.com/cexll/agentsdk-go/pkg/core/hooks"
	coremw "github.com/cexll/agentsdk-go/pkg/core/middleware"
)

const totalLifecycleEvents = 7

type runConfig struct {
	prompt      string
	sessionID   string
	toolName    string
	owner       string
	hookTimeout time.Duration
	dedupWindow int
	toolLatency time.Duration
}

func main() {
	cfg := parseConfig()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	hook := newDemoHooks(logger, totalLifecycleEvents)
	executor := hooks.NewExecutor(
		hooks.WithMiddleware(logEventMiddleware(logger), timingMiddleware(logger)),
		hooks.WithTimeout(cfg.hookTimeout),
		hooks.WithEventDedup(cfg.dedupWindow),
		hooks.WithErrorHandler(func(t events.EventType, err error) {
			if err != nil {
				logger.Warn("hook failed", "event", t, "err", err)
			}
		}),
	)
	defer executor.Close()
	executor.Register(hook)

	if err := emitLifecycleEvents(ctx, executor, cfg); err != nil {
		logger.Error("publish events failed", "err", err)
		os.Exit(1)
	}

	if err := hook.wait(ctx, 2*time.Second); err != nil {
		logger.Error("hooks did not complete", "err", err)
		os.Exit(1)
	}

	logger.Info("demo finished", "session_id", cfg.sessionID, "counts", hook.countsSnapshot())
}

func parseConfig() runConfig {
	var cfg runConfig
	flag.StringVar(&cfg.prompt, "prompt", "分析日志并生成摘要", "user prompt fed into the demo payloads")
	flag.StringVar(&cfg.sessionID, "session", "hooks-demo", "session id used in payloads")
	flag.StringVar(&cfg.toolName, "tool", "log_scan", "tool name for pre/post tool hooks")
	flag.StringVar(&cfg.owner, "owner", "hooks-example", "logical owner added to payload metadata")
	flag.DurationVar(&cfg.toolLatency, "tool-latency", 120*time.Millisecond, "simulated tool duration")
	flag.DurationVar(&cfg.hookTimeout, "hook-timeout", 500*time.Millisecond, "per-hook timeout")
	flag.IntVar(&cfg.dedupWindow, "dedup-window", 32, "deduplication window size")
	flag.Parse()
	return cfg
}

func emitLifecycleEvents(ctx context.Context, executor *hooks.Executor, cfg runConfig) error {
	eventsToSend := []events.Event{
		{
			Type: events.SessionStart,
			Payload: events.SessionPayload{
				SessionID: cfg.sessionID,
				Metadata: map[string]any{
					"owner": cfg.owner,
				},
			},
		},
		{
			Type:    events.UserPromptSubmit,
			Payload: events.UserPromptPayload{Prompt: cfg.prompt},
		},
		{
			Type: events.PreToolUse,
			Payload: events.ToolUsePayload{
				Name: cfg.toolName,
				Params: map[string]any{
					"query": cfg.prompt,
				},
			},
		},
		{
			Type: events.PostToolUse,
			Payload: events.ToolResultPayload{
				Name:     cfg.toolName,
				Result:   fmt.Sprintf("模拟耗时 %s 的工具结果", cfg.toolLatency),
				Duration: cfg.toolLatency,
			},
		},
		{
			Type: events.Notification,
			ID:   "notify-once",
			Payload: events.NotificationPayload{
				Message: "报告已生成，准备退出",
				Meta: map[string]any{
					"session": cfg.sessionID,
				},
			},
		},
		{
			Type: events.SubagentStop,
			Payload: events.SubagentStopPayload{
				Name:   "child-a",
				Reason: "演示子代理收敛",
			},
		},
		{
			Type: events.Stop,
			Payload: events.StopPayload{
				Reason: "主代理完成运行",
			},
		},
	}

	for _, evt := range eventsToSend {
		if err := publish(ctx, executor, evt); err != nil {
			return err
		}
	}

	// Duplicate notification shows de-duplication; it is ignored by design.
	_ = executor.Publish(events.Event{
		Type: events.Notification,
		ID:   "notify-once",
		Payload: events.NotificationPayload{
			Message: "重复的通知不会再次触发 hook",
		},
	})
	return nil
}

func publish(ctx context.Context, executor *hooks.Executor, evt events.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return executor.Publish(evt)
	}
}

type demoHooks struct {
	logger *slog.Logger

	mu     sync.Mutex
	counts map[events.EventType]int
	wg     sync.WaitGroup
}

func newDemoHooks(logger *slog.Logger, expected int) *demoHooks {
	h := &demoHooks{
		logger: logger,
		counts: make(map[events.EventType]int),
	}
	h.wg.Add(expected)
	return h
}

func (h *demoHooks) wait(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-waitCtx.Done():
		return waitCtx.Err()
	case <-done:
		return nil
	}
}

func (h *demoHooks) countsSnapshot() map[events.EventType]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	snapshot := make(map[events.EventType]int, len(h.counts))
	for k, v := range h.counts {
		snapshot[k] = v
	}
	return snapshot
}

func (h *demoHooks) record(t events.EventType) {
	h.mu.Lock()
	h.counts[t]++
	h.mu.Unlock()
	h.wg.Done()
}

func (h *demoHooks) PreToolUse(ctx context.Context, payload events.ToolUsePayload) error {
	h.logger.Info("PreToolUse", "tool", payload.Name, "params", payload.Params)
	h.record(events.PreToolUse)
	return nil
}

func (h *demoHooks) PostToolUse(ctx context.Context, payload events.ToolResultPayload) error {
	h.logger.Info("PostToolUse", "tool", payload.Name, "duration", payload.Duration, "err", payload.Err, "result", payload.Result)
	h.record(events.PostToolUse)
	return nil
}

func (h *demoHooks) UserPromptSubmit(ctx context.Context, payload events.UserPromptPayload) error {
	h.logger.Info("UserPromptSubmit", "prompt", payload.Prompt)
	h.record(events.UserPromptSubmit)
	return nil
}

func (h *demoHooks) SessionStart(ctx context.Context, payload events.SessionPayload) error {
	h.logger.Info("SessionStart", "session", payload.SessionID, "metadata", payload.Metadata)
	h.record(events.SessionStart)
	return nil
}

func (h *demoHooks) Stop(ctx context.Context, payload events.StopPayload) error {
	h.logger.Info("Stop", "reason", payload.Reason)
	h.record(events.Stop)
	return nil
}

func (h *demoHooks) SubagentStop(ctx context.Context, payload events.SubagentStopPayload) error {
	h.logger.Info("SubagentStop", "name", payload.Name, "reason", payload.Reason)
	h.record(events.SubagentStop)
	return nil
}

func (h *demoHooks) Notification(ctx context.Context, payload events.NotificationPayload) error {
	h.logger.Info("Notification", "message", payload.Message, "meta", payload.Meta)
	h.record(events.Notification)
	return nil
}

func logEventMiddleware(logger *slog.Logger) coremw.Middleware {
	return func(next coremw.Handler) coremw.Handler {
		return func(ctx context.Context, evt events.Event) error {
			logger.Info("middleware dispatch", "event", evt.Type, "id", evt.ID)
			return next(ctx, evt)
		}
	}
}

func timingMiddleware(logger *slog.Logger) coremw.Middleware {
	return func(next coremw.Handler) coremw.Handler {
		return func(ctx context.Context, evt events.Event) error {
			start := time.Now()
			err := next(ctx, evt)
			logger.Info("middleware timing", "event", evt.Type, "took", time.Since(start))
			return err
		}
	}
}

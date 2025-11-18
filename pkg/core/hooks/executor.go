package hooks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cexll/agentsdk-go/pkg/core/events"
	"github.com/cexll/agentsdk-go/pkg/core/middleware"
)

// Executor orchestrates event delivery to registered hooks via the shared
// event bus. It ensures ordering, deduplication and fault isolation.
type Executor struct {
	bus     *events.Bus
	busOpts []events.BusOption
	hooksMu sync.RWMutex
	hooks   []any
	mw      []middleware.Middleware
	timeout time.Duration
	errFn   func(events.EventType, error)
	bound   sync.Once
}

// ExecutorOption configures optional behaviour.
type ExecutorOption func(*Executor)

// WithMiddleware configures middleware around hook invocation.
func WithMiddleware(mw ...middleware.Middleware) ExecutorOption {
	return func(e *Executor) {
		e.mw = append(e.mw, mw...)
	}
}

// WithTimeout sets a default timeout for hook execution if individual
// subscriptions do not override it.
func WithTimeout(d time.Duration) ExecutorOption {
	return func(e *Executor) {
		e.timeout = d
	}
}

// WithEventDedup configures deduplication window size.
func WithEventDedup(limit int) ExecutorOption {
	return func(e *Executor) {
		e.busOpts = append(e.busOpts, events.WithDedupWindow(limit))
	}
}

// WithBus allows injecting a pre-configured bus (useful for testing).
func WithBus(bus *events.Bus) ExecutorOption {
	return func(e *Executor) {
		e.bus = bus
	}
}

// WithErrorHandler captures hook failures without propagating them.
func WithErrorHandler(fn func(events.EventType, error)) ExecutorOption {
	return func(e *Executor) {
		e.errFn = fn
	}
}

// NewExecutor creates a new executor with defaults tuned for deterministic
// ordering and safe concurrent use.
func NewExecutor(opts ...ExecutorOption) *Executor {
	exe := &Executor{}
	for _, opt := range opts {
		opt(exe)
	}
	if exe.bus == nil {
		exe.bus = events.NewBus(exe.busOpts...)
	}
	if exe.errFn == nil {
		exe.errFn = func(events.EventType, error) {}
	}
	exe.Bind()
	return exe
}

// Register adds hooks for the executor to notify.
func (e *Executor) Register(h ...any) {
	e.hooksMu.Lock()
	defer e.hooksMu.Unlock()
	e.hooks = append(e.hooks, h...)
}

// Publish sends an event through the bus. Deduplication and ordering are
// handled transparently.
func (e *Executor) Publish(evt events.Event) error {
	if e == nil || e.bus == nil {
		return errors.New("hooks: executor missing bus")
	}
	return e.bus.Publish(evt)
}

// Bind wire the executor with bus subscribers for the supported event types.
func (e *Executor) Bind() {
	e.bound.Do(func() {
		build := func(fn func(context.Context, events.Event)) events.Handler {
			return func(ctx context.Context, evt events.Event) {
				handler := middleware.Chain(func(ctx context.Context, evt events.Event) error {
					fn(ctx, evt)
					return nil
				}, e.mw...)
				if err := handler(ctx, evt); err != nil {
					e.errFn(evt.Type, err)
				}
			}
		}
		opts := func() []events.SubscriptionOption {
			if e.timeout <= 0 {
				return nil
			}
			return []events.SubscriptionOption{events.WithSubscriptionTimeout(e.timeout)}
		}
		e.bus.Subscribe(events.PreToolUse, build(e.handlePreToolUse), opts()...)
		e.bus.Subscribe(events.PostToolUse, build(e.handlePostToolUse), opts()...)
		e.bus.Subscribe(events.UserPromptSubmit, build(e.handleUserPrompt), opts()...)
		e.bus.Subscribe(events.SessionStart, build(e.handleSessionStart), opts()...)
		e.bus.Subscribe(events.Stop, build(e.handleStop), opts()...)
		e.bus.Subscribe(events.SubagentStop, build(e.handleSubagentStop), opts()...)
		e.bus.Subscribe(events.Notification, build(e.handleNotification), opts()...)
	})
}

func (e *Executor) handlePreToolUse(ctx context.Context, evt events.Event) {
	payload, _ := evt.Payload.(events.ToolUsePayload)
	e.forEachHook(func(h any) {
		if hook, ok := h.(PreToolUseHook); ok {
			if err := safeCall(func() error { return hook.PreToolUse(ctx, payload) }); err != nil {
				e.errFn(events.PreToolUse, err)
			}
		}
	})
}

func (e *Executor) handlePostToolUse(ctx context.Context, evt events.Event) {
	payload, _ := evt.Payload.(events.ToolResultPayload)
	e.forEachHook(func(h any) {
		if hook, ok := h.(PostToolUseHook); ok {
			if err := safeCall(func() error { return hook.PostToolUse(ctx, payload) }); err != nil {
				e.errFn(events.PostToolUse, err)
			}
		}
	})
}

func (e *Executor) handleUserPrompt(ctx context.Context, evt events.Event) {
	payload, _ := evt.Payload.(events.UserPromptPayload)
	e.forEachHook(func(h any) {
		if hook, ok := h.(UserPromptSubmitHook); ok {
			if err := safeCall(func() error { return hook.UserPromptSubmit(ctx, payload) }); err != nil {
				e.errFn(events.UserPromptSubmit, err)
			}
		}
	})
}

func (e *Executor) handleSessionStart(ctx context.Context, evt events.Event) {
	payload, _ := evt.Payload.(events.SessionPayload)
	e.forEachHook(func(h any) {
		if hook, ok := h.(SessionStartHook); ok {
			if err := safeCall(func() error { return hook.SessionStart(ctx, payload) }); err != nil {
				e.errFn(events.SessionStart, err)
			}
		}
	})
}

func (e *Executor) handleStop(ctx context.Context, evt events.Event) {
	payload, _ := evt.Payload.(events.StopPayload)
	e.forEachHook(func(h any) {
		if hook, ok := h.(StopHook); ok {
			if err := safeCall(func() error { return hook.Stop(ctx, payload) }); err != nil {
				e.errFn(events.Stop, err)
			}
		}
	})
}

func (e *Executor) handleSubagentStop(ctx context.Context, evt events.Event) {
	payload, _ := evt.Payload.(events.SubagentStopPayload)
	e.forEachHook(func(h any) {
		if hook, ok := h.(SubagentStopHook); ok {
			if err := safeCall(func() error { return hook.SubagentStop(ctx, payload) }); err != nil {
				e.errFn(events.SubagentStop, err)
			}
		}
	})
}

func (e *Executor) handleNotification(ctx context.Context, evt events.Event) {
	payload, _ := evt.Payload.(events.NotificationPayload)
	e.forEachHook(func(h any) {
		if hook, ok := h.(NotificationHook); ok {
			if err := safeCall(func() error { return hook.Notification(ctx, payload) }); err != nil {
				e.errFn(events.Notification, err)
			}
		}
	})
}

func (e *Executor) forEachHook(fn func(any)) {
	e.hooksMu.RLock()
	defer e.hooksMu.RUnlock()
	for _, h := range e.hooks {
		fn(h)
	}
}

func safeCall(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hooks: panic: %v", r)
			return
		}
	}()
	return fn()
}

// Close stops the underlying bus.
func (e *Executor) Close() {
	if e == nil || e.bus == nil {
		return
	}
	e.bus.Close()
}

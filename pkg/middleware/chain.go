package middleware

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Chain executes middleware sequentially and enforces short-circuit semantics.
type Chain struct {
	middlewares []Middleware
	timeout     time.Duration
	mu          sync.RWMutex
}

// ChainOption mutates the chain configuration.
type ChainOption func(*Chain)

// WithTimeout configures a per-stage timeout. Zero means no timeout.
func WithTimeout(d time.Duration) ChainOption {
	return func(c *Chain) {
		c.timeout = d
	}
}

// NewChain constructs a chain with the provided middleware. Nil items are
// ignored to keep the calling code simple.
func NewChain(mw []Middleware, opts ...ChainOption) *Chain {
	filtered := make([]Middleware, 0, len(mw))
	for _, m := range mw {
		if m != nil {
			filtered = append(filtered, m)
		}
	}
	c := &Chain{middlewares: filtered}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Use appends middleware at runtime.
func (c *Chain) Use(m Middleware) {
	if m == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.middlewares = append(c.middlewares, m)
}

// Execute runs the requested stage on all middleware in order. It stops on
// the first error and returns it.
func (c *Chain) Execute(ctx context.Context, stage Stage, st *State) error {
	c.mu.RLock()
	mws := make([]Middleware, len(c.middlewares))
	copy(mws, c.middlewares)
	c.mu.RUnlock()

	for _, mw := range mws {
		var err error
		exec := func(ctx context.Context) error {
			switch stage {
			case StageBeforeAgent:
				return mw.BeforeAgent(ctx, st)
			case StageBeforeTool:
				return mw.BeforeTool(ctx, st)
			case StageAfterTool:
				return mw.AfterTool(ctx, st)
			case StageAfterAgent:
				return mw.AfterAgent(ctx, st)
			default:
				return fmt.Errorf("middleware: unknown stage %d", stage)
			}
		}
		err = c.runWithTimeout(ctx, exec, mw)
		if err != nil {
			return fmt.Errorf("middleware %s failed: %w", middlewareName(mw), err)
		}
	}
	return nil
}

func (c *Chain) runWithTimeout(ctx context.Context, fn func(context.Context) error, mw Middleware) error {
	if c.timeout <= 0 {
		return fn(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- fn(ctx)
	}()

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("middleware %s timed out", middlewareName(mw))
		}
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func middlewareName(m Middleware) string {
	if m == nil {
		return "<nil>"
	}
	if name := m.Name(); name != "" {
		return name
	}
	return "<unnamed>"
}

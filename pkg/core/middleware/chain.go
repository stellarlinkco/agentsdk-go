package middleware

import (
	"context"

	"github.com/cexll/agentsdk-go/pkg/core/events"
)

// Handler is executed by the middleware chain. It operates on events and can
// return an error to signal failure to upstream callers.
type Handler func(context.Context, events.Event) error

// Middleware wraps a Handler, typically adding cross-cutting concerns such as
// logging, tracing or metrics.
type Middleware func(Handler) Handler

// Chain applies middlewares in the order they are provided, producing a final
// handler. The last middleware wraps the provided handler.
func Chain(h Handler, mws ...Middleware) Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

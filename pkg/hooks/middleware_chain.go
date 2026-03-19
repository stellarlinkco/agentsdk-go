package hooks

import "context"

// Handler is executed by the middleware chain. It operates on events and can
// return an error to signal failure to upstream callers.
type MiddlewareHandler func(context.Context, Event) error

// Middleware wraps a Handler, typically adding cross-cutting concerns such as
// logging, tracing or metrics.
type Middleware func(MiddlewareHandler) MiddlewareHandler

// Chain applies middlewares in the order they are provided, producing a final
// handler. The last middleware wraps the provided handler.
func Chain(h MiddlewareHandler, mws ...Middleware) MiddlewareHandler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

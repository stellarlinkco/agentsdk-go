package agent

import (
	"time"

	"github.com/cexll/agentsdk-go/pkg/middleware"
)

// Options controls runtime behavior of the Agent.
type Options struct {
	// MaxIterations limits how many cycles Run may execute.
	// Zero means no limit.
	MaxIterations int
	// Timeout bounds the entire Run invocation. Zero disables it.
	Timeout time.Duration
	// Middleware chain. Defaults to an empty chain when nil.
	Middleware *middleware.Chain
}

func (o Options) withDefaults() Options {
	if o.Middleware == nil {
		o.Middleware = middleware.NewChain(nil)
	}
	return o
}

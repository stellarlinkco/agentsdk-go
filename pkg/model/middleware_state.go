package model

import (
	"context"
	"strings"
)

type middlewareContextKey string

const (
	middlewareStateKey middlewareContextKey = "github.com/stellarlinkco/agentsdk-go/middleware-state"
	// MiddlewareStateKey exposes the context key so other packages can attach middleware state.
	MiddlewareStateKey = middlewareStateKey
)

// MiddlewareState is the minimal contract required for model providers to
// surface request/response data to middleware consumers without depending on
// the middleware package (which would cause an import cycle).
type MiddlewareState interface {
	SetModelInput(any)
	SetModelOutput(any)
	SetValue(string, any)
}

func middlewareState(ctx context.Context) MiddlewareState {
	if ctx == nil {
		return nil
	}
	state, ok := ctx.Value(middlewareStateKey).(MiddlewareState)
	if !ok {
		return nil
	}
	return state
}

func recordModelRequest(ctx context.Context, req Request) {
	if state := middlewareState(ctx); state != nil {
		state.SetModelInput(req)
	}
}

func recordModelResponse(ctx context.Context, resp *Response) {
	if resp == nil {
		return
	}
	if state := middlewareState(ctx); state != nil {
		state.SetModelOutput(resp)
		state.SetValue("model.response", resp)
		state.SetValue("model.usage", resp.Usage)
		if trimmed := strings.TrimSpace(resp.StopReason); trimmed != "" {
			state.SetValue("model.stop_reason", trimmed)
		}
	}
}

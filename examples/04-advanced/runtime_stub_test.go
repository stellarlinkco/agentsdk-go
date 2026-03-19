package main

import (
	"context"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
)

type stubRuntime struct {
	run func(context.Context, api.Request) (*api.Response, error)
}

func (s stubRuntime) Run(ctx context.Context, req api.Request) (*api.Response, error) {
	if s.run == nil {
		return &api.Response{Result: &api.Result{Output: "ok"}}, nil
	}
	return s.run(ctx, req)
}

func (stubRuntime) Close() error { return nil }

func requireAPIKey(t *testing.T) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
}


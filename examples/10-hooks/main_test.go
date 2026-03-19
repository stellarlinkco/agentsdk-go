package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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

func TestRun_RequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_RuntimeRunError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := hooksNewRuntime
	hooksNewRuntime = func(context.Context, api.Options) (hooksRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("boom")
		}}, nil
	}
	t.Cleanup(func() { hooksNewRuntime = oldNew })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_ContextCanceled(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMain_DoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := hooksFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldNew := hooksNewRuntime
	t.Cleanup(func() {
		hooksFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		hooksNewRuntime = oldNew
	})

	called := false
	hooksFatal = func(...any) { called = true }
	hooksNewRuntime = func(context.Context, api.Options) (hooksRuntime, error) { return stubRuntime{}, nil }

	os.Args = []string{"10-hooks.test"}

	main()

	if called {
		t.Fatalf("unexpected fatal")
	}
}

func TestRun_BuildRuntimeError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := hooksNewRuntime
	t.Cleanup(func() { hooksNewRuntime = old })
	hooksNewRuntime = func(context.Context, api.Options) (hooksRuntime, error) { return nil, errors.New("boom") }

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMain_FatalsOnRunError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := hooksFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldNew := hooksNewRuntime
	t.Cleanup(func() {
		hooksFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		hooksNewRuntime = oldNew
	})

	called := false
	hooksFatal = func(...any) { called = true }
	hooksNewRuntime = func(context.Context, api.Options) (hooksRuntime, error) { return nil, errors.New("boom") }

	os.Args = []string{"10-hooks.test"}

	main()

	if !called {
		t.Fatalf("expected fatal")
	}
}

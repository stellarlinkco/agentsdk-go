package main

import (
	"bytes"
	"context"
	"errors"
	"os"
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

func TestRun_RequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NoOutput_PrintsPlaceholder(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := embedNewRuntime
	embedNewRuntime = func(context.Context, api.Options) (embedRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return &api.Response{Result: &api.Result{Output: " "}}, nil
		}}, nil
	}
	t.Cleanup(func() { embedNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); got == "" || !bytes.Contains([]byte(got), []byte("(no output)")) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRun_ModelError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := embedNewRuntime
	embedNewRuntime = func(context.Context, api.Options) (embedRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("boom")
		}}, nil
	}
	t.Cleanup(func() { embedNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NewRuntimeError(t *testing.T) {
	old := embedNewRuntime
	embedNewRuntime = func(_ context.Context, _ api.Options) (embedRuntime, error) {
		return nil, errors.New("new boom")
	}
	t.Cleanup(func() { embedNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_RequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, err := buildOptions(nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_WithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	opts, err := buildOptions(nil)
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if opts.ModelFactory == nil {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := embedNewRuntime
	embedNewRuntime = func(context.Context, api.Options) (embedRuntime, error) { return stubRuntime{}, nil }
	t.Cleanup(func() { embedNewRuntime = oldNew })

	oldFatal := embedFatal
	embedFatal = func(_ ...any) { t.Fatalf("unexpected fatal") }
	t.Cleanup(func() { embedFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"06-embed"}

	main()
}

func TestMain_FatalsOnRunError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := embedFatal
	var called bool
	embedFatal = func(_ ...any) { called = true }
	t.Cleanup(func() { embedFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"06-embed"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

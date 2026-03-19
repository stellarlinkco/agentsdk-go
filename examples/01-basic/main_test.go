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

func TestRun_RequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NoOutput_PrintsPlaceholder(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := basicNewRuntime
	basicNewRuntime = func(context.Context, api.Options) (basicRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return &api.Response{Result: &api.Result{Output: "   "}}, nil
		}}, nil
	}
	t.Cleanup(func() { basicNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); got == "" || !bytes.Contains([]byte(got), []byte("(no output)")) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRun_ModelError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := basicNewRuntime
	basicNewRuntime = func(context.Context, api.Options) (basicRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("boom")
		}}, nil
	}
	t.Cleanup(func() { basicNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NewRuntimeError(t *testing.T) {
	old := basicNewRuntime
	basicNewRuntime = func(_ context.Context, _ api.Options) (basicRuntime, error) {
		return nil, errors.New("new boom")
	}
	t.Cleanup(func() { basicNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_RequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	var out bytes.Buffer
	if _, err := buildOptions(nil, &out, ".trace"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_WithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	var out bytes.Buffer
	opts, err := buildOptions(nil, &out, ".trace")
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if opts.ModelFactory == nil {
		t.Fatalf("expected ModelFactory")
	}
}

func TestMain_Smoke(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := basicNewRuntime
	basicNewRuntime = func(context.Context, api.Options) (basicRuntime, error) { return stubRuntime{}, nil }
	t.Cleanup(func() { basicNewRuntime = oldNew })

	oldArgs := os.Args
	oldWD, _ := os.Getwd()
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"01-basic"}
	_ = os.Chdir(t.TempDir())
	t.Cleanup(func() {
		if oldWD != "" {
			_ = os.Chdir(oldWD)
		}
	})
	main()
}

func TestMain_FatalsOnRunError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := basicFatal
	var called bool
	basicFatal = func(_ ...any) { called = true }
	t.Cleanup(func() { basicFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"01-basic"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

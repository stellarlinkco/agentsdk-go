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

	old := customToolsNewRuntime
	customToolsNewRuntime = func(context.Context, api.Options) (customToolsRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return &api.Response{Result: &api.Result{Output: " "}}, nil
		}}, nil
	}
	t.Cleanup(func() { customToolsNewRuntime = old })

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

	old := customToolsNewRuntime
	customToolsNewRuntime = func(context.Context, api.Options) (customToolsRuntime, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("boom")
		}}, nil
	}
	t.Cleanup(func() { customToolsNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NewRuntimeError(t *testing.T) {
	old := customToolsNewRuntime
	customToolsNewRuntime = func(_ context.Context, _ api.Options) (customToolsRuntime, error) {
		return nil, errors.New("new boom")
	}
	t.Cleanup(func() { customToolsNewRuntime = old })

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

func TestEchoTool_SchemaAndExecute(t *testing.T) {
	tool := &EchoTool{}
	if tool.Name() == "" || tool.Description() == "" {
		t.Fatalf("expected metadata")
	}
	if schema := tool.Schema(); schema == nil || schema.Type == "" {
		t.Fatalf("unexpected schema: %+v", schema)
	}
	res, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err != nil || res == nil || res.Output != "hello" {
		t.Fatalf("Execute err=%v res=%+v", err, res)
	}
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := customToolsNewRuntime
	customToolsNewRuntime = func(context.Context, api.Options) (customToolsRuntime, error) { return stubRuntime{}, nil }
	t.Cleanup(func() { customToolsNewRuntime = oldNew })

	oldFatal := customToolsFatal
	customToolsFatal = func(_ ...any) { t.Fatalf("unexpected fatal") }
	t.Cleanup(func() { customToolsFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"05-custom-tools"}

	main()
}

func TestMain_FatalsOnRunError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := customToolsFatal
	var called bool
	customToolsFatal = func(_ ...any) { called = true }
	t.Cleanup(func() { customToolsFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"05-custom-tools"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

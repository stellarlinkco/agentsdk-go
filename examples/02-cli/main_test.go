package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
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

func TestRun_RequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	in := strings.NewReader("")
	if err := run(context.Background(), nil, in, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildConfigAndOptions_RequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions(nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildConfigAndOptions_WithKeySetsModelFactory(t *testing.T) {
	requireAPIKey(t)
	var out bytes.Buffer
	_, opts, err := buildConfigAndOptions(nil, &out)
	if err != nil {
		t.Fatalf("buildConfigAndOptions: %v", err)
	}
	if opts.ModelFactory == nil {
		t.Fatalf("expected ModelFactory")
	}
	if opts.Model != nil {
		t.Fatalf("expected nil Model")
	}
}

func TestBuildConfigAndOptions_EnableMCP(t *testing.T) {
	requireAPIKey(t)
	var out bytes.Buffer
	_, opts, err := buildConfigAndOptions([]string{"--enable-mcp=true"}, &out)
	if err != nil {
		t.Fatalf("buildConfigAndOptions: %v", err)
	}
	if opts.MCPServers != nil {
		t.Fatalf("expected nil MCPServers when enabled, got=%v", opts.MCPServers)
	}
}

func TestBuildConfigAndOptions_DefaultDisablesMCPServers(t *testing.T) {
	requireAPIKey(t)
	var out bytes.Buffer
	_, opts, err := buildConfigAndOptions(nil, &out)
	if err != nil {
		t.Fatalf("buildConfigAndOptions: %v", err)
	}
	if opts.ModelFactory == nil {
		t.Fatalf("expected ModelFactory")
	}
	if opts.MCPServers == nil || len(opts.MCPServers) != 0 {
		t.Fatalf("expected empty MCPServers slice, got=%v", opts.MCPServers)
	}
}

func TestBuildConfigAndOptions_ParseError(t *testing.T) {
	requireAPIKey(t)
	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions([]string{"--nope"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_InteractiveExit(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) { return stubRuntime{}, nil }

	ctx := context.Background()
	in := strings.NewReader("exit\n")
	var out bytes.Buffer
	if err := run(ctx, []string{"--interactive=true"}, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "You>") {
		t.Fatalf("expected prompt, got=%q", out.String())
	}
}

func TestRun_InteractiveSkipsEmptyInput(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) { return stubRuntime{}, nil }

	ctx := context.Background()
	in := strings.NewReader("\nexit\n")
	var out bytes.Buffer
	if err := run(ctx, []string{"--interactive=true"}, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if n := strings.Count(out.String(), "You>"); n != 2 {
		t.Fatalf("expected 2 prompts, got %d: %q", n, out.String())
	}
}

type readerError struct{ err error }

func (r readerError) Read([]byte) (int, error) { return 0, r.err }

func TestRun_InteractiveScannerError(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) { return stubRuntime{}, nil }

	ctx := context.Background()
	in := readerError{err: errors.New("boom")}
	var out bytes.Buffer
	if err := run(ctx, []string{"--interactive=true"}, in, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEnvOrDefault_UsesEnv(t *testing.T) {
	t.Setenv("SESSION_ID", "x")
	if got := envOrDefault("SESSION_ID", "fallback"); got != "x" {
		t.Fatalf("got=%q", got)
	}
	if got := envOrDefault("MISSING", "fallback"); got != "fallback" {
		t.Fatalf("got=%q", got)
	}
	_ = os.Getenv("SESSION_ID")
}

func TestMain_DoesNotFatal(t *testing.T) {
	requireAPIKey(t)

	oldFatal := cliFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldNew := cliNewRuntime
	t.Cleanup(func() {
		cliFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		cliNewRuntime = oldNew
	})

	called := false
	cliFatal = func(...any) { called = true }
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return &api.Response{Result: &api.Result{Output: "hi"}}, nil
		}}, nil
	}

	tmp := t.TempDir()
	os.Args = []string{"02-cli.test", "--project-root", tmp}

	main()

	if called {
		t.Fatalf("unexpected fatal")
	}
}

func TestRun_NonInteractive_NoOutputPrintsFallback(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return &api.Response{Result: &api.Result{Output: ""}}, nil
		}}, nil
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"--prompt", "x"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "(no output)") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestRun_Interactive_RunErrorPrintedAndContinues(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("boom")
		}}, nil
	}

	var out bytes.Buffer
	in := strings.NewReader("hi\nexit\n")
	if err := run(context.Background(), []string{"--interactive=true"}, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "Error:") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestBuildConfigAndOptions_ProjectRootAbsError(t *testing.T) {
	requireAPIKey(t)
	old := filepathAbs
	t.Cleanup(func() { filepathAbs = old })
	filepathAbs = func(string) (string, error) { return "", errors.New("abs boom") }

	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions([]string{"--project-root", "x"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMain_FatalsOnRunError(t *testing.T) {
	requireAPIKey(t)

	oldFatal := cliFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldNew := cliNewRuntime
	t.Cleanup(func() {
		cliFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		cliNewRuntime = oldNew
	})

	called := false
	cliFatal = func(...any) { called = true }
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("boom")
		}}, nil
	}

	tmp := t.TempDir()
	os.Args = []string{"02-cli.test", "--project-root", tmp}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

func TestRun_BuildRuntimeError(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return nil, errors.New("boom")
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{"--project-root", t.TempDir()}, strings.NewReader(""), &out)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "build runtime:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_NonInteractive_RunError(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return nil, errors.New("boom")
		}}, nil
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{"--prompt", "x"}, strings.NewReader(""), &out)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "run:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Interactive_EnableMCPMessage(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) { return stubRuntime{}, nil }

	var out bytes.Buffer
	in := strings.NewReader("exit\n")
	if err := run(context.Background(), []string{"--interactive=true", "--enable-mcp=true"}, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "MCP auto-load enabled") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestRun_Interactive_PrintsAssistantOutput(t *testing.T) {
	requireAPIKey(t)

	oldNew := cliNewRuntime
	t.Cleanup(func() { cliNewRuntime = oldNew })
	cliNewRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return stubRuntime{run: func(context.Context, api.Request) (*api.Response, error) {
			return &api.Response{Result: &api.Result{Output: "hi"}}, nil
		}}, nil
	}

	var out bytes.Buffer
	in := strings.NewReader("hi\nexit\n")
	if err := run(context.Background(), []string{"--interactive=true"}, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "Assistant>") {
		t.Fatalf("out=%q", out.String())
	}
}

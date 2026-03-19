package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestRun_OfflineSinglePromptDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	in := strings.NewReader("")
	if err := run(context.Background(), nil, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Len() == 0 {
		t.Fatalf("expected output")
	}
}

func TestBuildConfigAndOptions_OnlineRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions([]string{"--online"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildConfigAndOptions_OnlineWithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	_, opts, err := buildConfigAndOptions([]string{"--online"}, &out)
	if err != nil {
		t.Fatalf("buildConfigAndOptions: %v", err)
	}
	if opts.ModelFactory == nil {
		t.Fatalf("expected ModelFactory")
	}
}

func TestBuildConfigAndOptions_EnableMCP(t *testing.T) {
	var out bytes.Buffer
	_, opts, err := buildConfigAndOptions([]string{"--enable-mcp=true"}, &out)
	if err != nil {
		t.Fatalf("buildConfigAndOptions: %v", err)
	}
	if opts.MCPServers != nil {
		t.Fatalf("expected nil MCPServers when enabled, got=%v", opts.MCPServers)
	}
}

func TestBuildConfigAndOptions_DefaultOfflineDisablesMCPServers(t *testing.T) {
	var out bytes.Buffer
	_, opts, err := buildConfigAndOptions(nil, &out)
	if err != nil {
		t.Fatalf("buildConfigAndOptions: %v", err)
	}
	if opts.Model == nil {
		t.Fatalf("expected offline Model")
	}
	if opts.ModelFactory != nil {
		t.Fatalf("expected nil ModelFactory")
	}
	if opts.MCPServers == nil || len(opts.MCPServers) != 0 {
		t.Fatalf("expected empty MCPServers slice, got=%v", opts.MCPServers)
	}
}

func TestBuildConfigAndOptions_ParseError(t *testing.T) {
	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions([]string{"--nope"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_InteractiveExit(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

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
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

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
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

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

type fixedModel struct {
	content string
	err     error
}

func (m fixedModel) Complete(context.Context, modelpkg.Request) (*modelpkg.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &modelpkg.Response{
		Message:    modelpkg.Message{Role: "assistant", Content: m.content},
		StopReason: "stop",
	}, nil
}

func (m fixedModel) CompleteStream(ctx context.Context, req modelpkg.Request, cb modelpkg.StreamHandler) error {
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	if cb == nil {
		return nil
	}
	return cb(modelpkg.StreamResult{Final: true, Response: resp})
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := cliFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldOffline := offlineModel
	t.Cleanup(func() {
		cliFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		offlineModel = oldOffline
	})

	called := false
	cliFatal = func(...any) { called = true }
	offlineModel = &demomodel.EchoModel{Prefix: "offline"}

	tmp := t.TempDir()
	os.Args = []string{"02-cli.test", "--project-root", tmp}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w

	main()

	_ = w.Close()
	_, _ = io.ReadAll(r)
	_ = r.Close()

	if called {
		t.Fatalf("unexpected fatal")
	}
}

func TestRun_NonInteractive_NoOutputPrintsFallback(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := offlineModel
	t.Cleanup(func() { offlineModel = old })
	offlineModel = fixedModel{content: ""}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"--prompt", "x"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "(no output)") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestRun_Interactive_RunErrorPrintedAndContinues(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := offlineModel
	t.Cleanup(func() { offlineModel = old })
	offlineModel = fixedModel{err: errors.New("boom")}

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
	old := filepathAbs
	t.Cleanup(func() { filepathAbs = old })
	filepathAbs = func(string) (string, error) { return "", errors.New("abs boom") }

	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions([]string{"--project-root", "x"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMain_FatalsOnRunError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := cliFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldOffline := offlineModel
	t.Cleanup(func() {
		cliFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		offlineModel = oldOffline
	})

	called := false
	cliFatal = func(...any) { called = true }
	offlineModel = fixedModel{err: errors.New("boom")}

	tmp := t.TempDir()
	os.Args = []string{"02-cli.test", "--project-root", tmp}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w

	main()

	_ = w.Close()
	_, _ = io.ReadAll(r)
	_ = r.Close()
	if !called {
		t.Fatalf("expected fatal")
	}
}

func TestRun_BuildRuntimeError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := offlineModel
	t.Cleanup(func() { offlineModel = old })
	offlineModel = nil

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
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := offlineModel
	t.Cleanup(func() { offlineModel = old })
	offlineModel = fixedModel{err: errors.New("boom")}

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
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

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
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := offlineModel
	t.Cleanup(func() { offlineModel = old })
	offlineModel = &demomodel.EchoModel{Prefix: "offline"}

	var out bytes.Buffer
	in := strings.NewReader("hi\nexit\n")
	if err := run(context.Background(), []string{"--interactive=true"}, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "Assistant>") {
		t.Fatalf("out=%q", out.String())
	}
}

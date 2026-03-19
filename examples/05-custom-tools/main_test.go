package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type customToolsBlankModel struct{}

func (customToolsBlankModel) Complete(ctx context.Context, _ modelpkg.Request) (*modelpkg.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &modelpkg.Response{Message: modelpkg.Message{Role: "assistant", Content: " "}, StopReason: "stop"}, nil
}

func (m customToolsBlankModel) CompleteStream(ctx context.Context, req modelpkg.Request, cb modelpkg.StreamHandler) error {
	if cb == nil {
		return nil
	}
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	return cb(modelpkg.StreamResult{Final: true, Response: resp})
}

type customToolsErrModel struct{ err error }

func (m customToolsErrModel) Complete(_ context.Context, _ modelpkg.Request) (*modelpkg.Response, error) {
	return nil, m.err
}

func (m customToolsErrModel) CompleteStream(_ context.Context, _ modelpkg.Request, _ modelpkg.StreamHandler) error {
	return m.err
}

func TestRun_OfflineDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Len() == 0 {
		t.Fatalf("expected output")
	}
}

func TestRun_NoOutput_PrintsPlaceholder(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := customToolsOfflineModel
	customToolsOfflineModel = customToolsBlankModel{}
	t.Cleanup(func() { customToolsOfflineModel = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); got == "" || !bytes.Contains([]byte(got), []byte("(no output)")) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRun_ModelError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := customToolsOfflineModel
	customToolsOfflineModel = customToolsErrModel{err: errors.New("boom")}
	t.Cleanup(func() { customToolsOfflineModel = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NewRuntimeError(t *testing.T) {
	old := customToolsNewRuntime
	customToolsNewRuntime = func(_ context.Context, _ api.Options) (*api.Runtime, error) {
		return nil, errors.New("new boom")
	}
	t.Cleanup(func() { customToolsNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_OnlineRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	if err := run(context.Background(), []string{"--online"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_OnlineRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, err := buildOptions([]string{"--online"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_OnlineWithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	opts, err := buildOptions([]string{"--online"})
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if opts.ModelFactory == nil || opts.Model != nil {
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

func TestHasArg_EdgeCases(t *testing.T) {
	if hasArg([]string{"--online"}, "") {
		t.Fatalf("expected hasArg=false for empty want")
	}
	if !hasArg([]string{"  --online "}, "--online") {
		t.Fatalf("expected hasArg=true with trimming")
	}
	if hasArg([]string{"--offline"}, "--online") {
		t.Fatalf("expected hasArg=false when missing")
	}
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
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
	os.Args = []string{"05-custom-tools", "--online"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

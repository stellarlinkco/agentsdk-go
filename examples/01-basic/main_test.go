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

type basicBlankModel struct{}

func (basicBlankModel) Complete(ctx context.Context, _ modelpkg.Request) (*modelpkg.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &modelpkg.Response{Message: modelpkg.Message{Role: "assistant", Content: "   "}, StopReason: "stop"}, nil
}

func (m basicBlankModel) CompleteStream(ctx context.Context, req modelpkg.Request, cb modelpkg.StreamHandler) error {
	if cb == nil {
		return nil
	}
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	return cb(modelpkg.StreamResult{Final: true, Response: resp})
}

type basicErrModel struct{ err error }

func (m basicErrModel) Complete(_ context.Context, _ modelpkg.Request) (*modelpkg.Response, error) {
	return nil, m.err
}

func (m basicErrModel) CompleteStream(_ context.Context, _ modelpkg.Request, _ modelpkg.StreamHandler) error {
	return m.err
}

func TestRun_OfflineDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); got == "" {
		t.Fatalf("expected output")
	}
}

func TestRun_NoOutput_PrintsPlaceholder(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := basicOfflineModel
	basicOfflineModel = basicBlankModel{}
	t.Cleanup(func() { basicOfflineModel = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); got == "" || !bytes.Contains([]byte(got), []byte("(no output)")) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRun_ModelError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := basicOfflineModel
	basicOfflineModel = basicErrModel{err: errors.New("boom")}
	t.Cleanup(func() { basicOfflineModel = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NewRuntimeError(t *testing.T) {
	old := basicNewRuntime
	basicNewRuntime = func(_ context.Context, _ api.Options) (*api.Runtime, error) {
		return nil, errors.New("new boom")
	}
	t.Cleanup(func() { basicNewRuntime = old })

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out, t.TempDir()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_OnlineRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	var out bytes.Buffer
	if _, err := buildOptions([]string{"--online"}, &out, ".trace"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHasArg_EmptyWantFalse(t *testing.T) {
	if hasArg([]string{"--online"}, "") {
		t.Fatalf("expected false")
	}
}

func TestBuildOptions_OnlineWithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	var out bytes.Buffer
	opts, err := buildOptions([]string{"--online"}, &out, ".trace")
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if opts.ModelFactory == nil {
		t.Fatalf("expected ModelFactory")
	}
}

func TestHasArg_Match(t *testing.T) {
	if !hasArg([]string{"a", " --online "}, "--online") {
		t.Fatalf("expected match")
	}
}

func TestMain_Smoke(t *testing.T) {
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
	os.Args = []string{"01-basic", "--online"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

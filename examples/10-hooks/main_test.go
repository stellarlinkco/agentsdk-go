package main

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestRun_OfflineDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := run(ctx, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRun_OnlineRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := run(ctx, []string{"--online"}); err == nil {
		t.Fatalf("expected error")
	}
}

type fixedModel struct{ err error }

func (m fixedModel) Complete(context.Context, modelpkg.Request) (*modelpkg.Response, error) {
	return nil, m.err
}

func (m fixedModel) CompleteStream(ctx context.Context, req modelpkg.Request, cb modelpkg.StreamHandler) error {
	_, err := m.Complete(ctx, req)
	return err
}

func TestRun_OfflineModelError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := offlineModel
	t.Cleanup(func() { offlineModel = old })
	offlineModel = fixedModel{err: errors.New("boom")}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_OnlineWithKey_ContextCanceled(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, []string{"--online"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := hooksFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldOffline := offlineModel
	t.Cleanup(func() {
		hooksFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		offlineModel = oldOffline
	})

	called := false
	hooksFatal = func(...any) { called = true }
	offlineModel = &demomodel.EchoModel{Prefix: "offline"}

	os.Args = []string{"10-hooks.test"}
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

func TestRun_BuildRuntimeError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := offlineModel
	t.Cleanup(func() { offlineModel = old })
	offlineModel = nil

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
	oldOffline := offlineModel
	t.Cleanup(func() {
		hooksFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
		offlineModel = oldOffline
	})

	called := false
	hooksFatal = func(...any) { called = true }
	offlineModel = fixedModel{err: errors.New("boom")}

	os.Args = []string{"10-hooks.test"}
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

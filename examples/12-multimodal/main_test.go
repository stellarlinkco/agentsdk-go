package main

import (
	"context"
	"errors"
	"image"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
)

type stubRuntime struct {
	failAt int
	calls  int
}

func (m *stubRuntime) Run(ctx context.Context, _ api.Request) (*api.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.calls++
	if m.failAt > 0 && m.calls == m.failAt {
		return nil, errors.New("boom")
	}
	return &api.Response{Result: &api.Result{Output: "ok"}}, nil
}

func (*stubRuntime) Close() error { return nil }

func TestRunRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_BuildRuntimeErrorIsWrapped(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := multimodalNewRuntime
	multimodalNewRuntime = func(_ context.Context, _ api.Options) (multimodalRuntime, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { multimodalNewRuntime = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "build runtime:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Demo1ErrorIsWrapped(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := multimodalNewRuntime
	multimodalNewRuntime = func(context.Context, api.Options) (multimodalRuntime, error) {
		return &stubRuntime{failAt: 1}, nil
	}
	t.Cleanup(func() { multimodalNewRuntime = oldNew })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "demo1:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Demo2ErrorIsWrapped(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := multimodalNewRuntime
	multimodalNewRuntime = func(context.Context, api.Options) (multimodalRuntime, error) {
		return &stubRuntime{failAt: 2}, nil
	}
	t.Cleanup(func() { multimodalNewRuntime = oldNew })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "demo2:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Demo3ErrorIsWrapped(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := multimodalNewRuntime
	multimodalNewRuntime = func(context.Context, api.Options) (multimodalRuntime, error) {
		return &stubRuntime{failAt: 3}, nil
	}
	t.Cleanup(func() { multimodalNewRuntime = oldNew })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "demo3:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_GeneratePNGErrorIsWrapped(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := multimodalNewRuntime
	multimodalNewRuntime = func(context.Context, api.Options) (multimodalRuntime, error) {
		return &stubRuntime{}, nil
	}
	t.Cleanup(func() { multimodalNewRuntime = oldNew })

	oldEncode := multimodalPNGEncode
	multimodalPNGEncode = func(_ io.Writer, _ image.Image) error { return errors.New("encode boom") }
	t.Cleanup(func() { multimodalPNGEncode = oldEncode })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "generate png:") {
		t.Fatalf("err=%v", err)
	}
}

func TestGenerateTestPNG_EncodeError(t *testing.T) {
	old := multimodalPNGEncode
	multimodalPNGEncode = func(_ io.Writer, _ image.Image) error { return errors.New("encode boom") }
	t.Cleanup(func() { multimodalPNGEncode = old })

	if _, err := generateTestPNG(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMain_DoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldNew := multimodalNewRuntime
	multimodalNewRuntime = func(context.Context, api.Options) (multimodalRuntime, error) { return &stubRuntime{}, nil }
	t.Cleanup(func() { multimodalNewRuntime = oldNew })

	oldFatal := multimodalFatal
	multimodalFatal = func(_ ...any) { t.Fatalf("unexpected fatal") }
	t.Cleanup(func() { multimodalFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"12-multimodal"}

	main()
}

func TestMain_FatalsOnError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := multimodalFatal
	var called bool
	multimodalFatal = func(_ ...any) { called = true }
	t.Cleanup(func() { multimodalFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"12-multimodal"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

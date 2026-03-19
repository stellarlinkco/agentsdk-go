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
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type multimodalSeqModel struct {
	failAt int
	calls  int
}

func (m *multimodalSeqModel) Complete(ctx context.Context, _ modelpkg.Request) (*modelpkg.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.calls++
	if m.failAt > 0 && m.calls == m.failAt {
		return nil, errors.New("boom")
	}
	return &modelpkg.Response{Message: modelpkg.Message{Role: "assistant", Content: "ok"}, StopReason: "stop"}, nil
}

func (m *multimodalSeqModel) CompleteStream(ctx context.Context, req modelpkg.Request, cb modelpkg.StreamHandler) error {
	if cb == nil {
		return nil
	}
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	return cb(modelpkg.StreamResult{Final: true, Response: resp})
}

func TestRunOffline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := run(ctx, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunOnlineRequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := run(ctx, []string{"--online"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_BuildRuntimeErrorIsWrapped(t *testing.T) {
	old := multimodalNewRuntime
	multimodalNewRuntime = func(_ context.Context, _ api.Options) (*api.Runtime, error) {
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

func TestRun_Online_BuildRuntimeErrorIsWrapped(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := multimodalNewRuntime
	multimodalNewRuntime = func(_ context.Context, _ api.Options) (*api.Runtime, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { multimodalNewRuntime = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := run(ctx, []string{"--online"})
	if err == nil || !strings.Contains(err.Error(), "build runtime:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Demo1ErrorIsWrapped(t *testing.T) {
	old := multimodalOfflineModel
	multimodalOfflineModel = &multimodalSeqModel{failAt: 1}
	t.Cleanup(func() { multimodalOfflineModel = old })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "demo1:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Demo2ErrorIsWrapped(t *testing.T) {
	old := multimodalOfflineModel
	multimodalOfflineModel = &multimodalSeqModel{failAt: 2}
	t.Cleanup(func() { multimodalOfflineModel = old })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "demo2:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Demo3ErrorIsWrapped(t *testing.T) {
	old := multimodalOfflineModel
	multimodalOfflineModel = &multimodalSeqModel{failAt: 3}
	t.Cleanup(func() { multimodalOfflineModel = old })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "demo3:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_GeneratePNGErrorIsWrapped(t *testing.T) {
	oldModel := multimodalOfflineModel
	multimodalOfflineModel = &multimodalSeqModel{}
	t.Cleanup(func() { multimodalOfflineModel = oldModel })

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

func TestMain_OfflineDoesNotFatal(t *testing.T) {
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
	os.Args = []string{"12-multimodal", "--online"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

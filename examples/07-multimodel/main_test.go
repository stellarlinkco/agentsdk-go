package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type multimodelErrModel struct{ err error }

func (m multimodelErrModel) Complete(_ context.Context, _ modelpkg.Request) (*modelpkg.Response, error) {
	return nil, m.err
}

func (m multimodelErrModel) CompleteStream(_ context.Context, _ modelpkg.Request, _ modelpkg.StreamHandler) error {
	return m.err
}

func requireAPIKey(t *testing.T) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
}

func TestRun_Default(t *testing.T) {
	requireAPIKey(t)

	old := multimodelAnthropicModelFactory
	multimodelAnthropicModelFactory = func(_ context.Context, _, _ string) (modelpkg.Model, error) {
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := run(ctx, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRun_MidModelError(t *testing.T) {
	requireAPIKey(t)

	old := multimodelAnthropicModelFactory
	multimodelAnthropicModelFactory = func(_ context.Context, _, modelName string) (modelpkg.Model, error) {
		if modelName == "claude-sonnet-4-20250514" {
			return multimodelErrModel{err: errors.New("boom")}, nil
		}
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "run:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_OverrideLowTierError(t *testing.T) {
	requireAPIKey(t)

	old := multimodelAnthropicModelFactory
	multimodelAnthropicModelFactory = func(_ context.Context, _, modelName string) (modelpkg.Model, error) {
		if modelName == "claude-3-5-haiku-20241022" {
			return multimodelErrModel{err: errors.New("boom")}, nil
		}
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "run override:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_BuildRuntimeErrorIsWrapped(t *testing.T) {
	requireAPIKey(t)

	old := multimodelAnthropicModelFactory
	multimodelAnthropicModelFactory = func(_ context.Context, _, modelName string) (modelpkg.Model, error) {
		if modelName == "claude-sonnet-4-20250514" {
			return nil, nil
		}
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "build runtime:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_RequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildOptions_Defaults(t *testing.T) {
	requireAPIKey(t)

	old := multimodelAnthropicModelFactory
	multimodelAnthropicModelFactory = func(_ context.Context, _, _ string) (modelpkg.Model, error) {
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	opts, err := buildOptions(ctx, nil)
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if opts.Model == nil {
		t.Fatalf("expected Model")
	}
	if opts.ModelPool == nil {
		t.Fatalf("expected ModelPool")
	}
	if opts.ModelPool[api.ModelTierLow] == nil || opts.ModelPool[api.ModelTierMid] == nil || opts.ModelPool[api.ModelTierHigh] == nil {
		t.Fatalf("missing tiers: %+v", opts.ModelPool)
	}
	if opts.ModelPool[api.ModelTierMid] != opts.Model {
		t.Fatalf("expected Model == mid tier")
	}
	if opts.SubagentModelMapping["plan"] != api.ModelTierHigh {
		t.Fatalf("unexpected mapping: %+v", opts.SubagentModelMapping)
	}
}

func TestBuildOptions_BuildsProvidersWithoutNetwork(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	opts, err := buildOptions(ctx, nil)
	if err != nil {
		t.Fatalf("buildOptions: %v", err)
	}
	if opts.Model == nil {
		t.Fatalf("expected Model")
	}
	if _, ok := opts.Model.(*demomodel.EchoModel); ok {
		t.Fatalf("expected online model, got EchoModel")
	}
	if opts.ModelPool == nil {
		t.Fatalf("expected ModelPool")
	}
	if opts.ModelPool[api.ModelTierMid] == nil || opts.Model != opts.ModelPool[api.ModelTierMid] {
		t.Fatalf("expected Model == mid tier")
	}
}

func TestBuildOptions_Online_ModelFactoryError_Haiku(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := multimodelAnthropicModelFactory
	multimodelAnthropicModelFactory = func(_ context.Context, _, _ string) (modelpkg.Model, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := buildOptions(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "create haiku model:") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildOptions_Online_ModelFactoryError_Sonnet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := multimodelAnthropicModelFactory
	var calls int
	multimodelAnthropicModelFactory = func(_ context.Context, _, _ string) (modelpkg.Model, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("boom")
		}
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := buildOptions(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "create sonnet model:") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildOptions_Online_ModelFactoryError_Opus(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	old := multimodelAnthropicModelFactory
	var calls int
	multimodelAnthropicModelFactory = func(_ context.Context, _, _ string) (modelpkg.Model, error) {
		calls++
		if calls == 3 {
			return nil, errors.New("boom")
		}
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := buildOptions(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "create opus model:") {
		t.Fatalf("err=%v", err)
	}
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
	requireAPIKey(t)

	oldFactory := multimodelAnthropicModelFactory
	multimodelAnthropicModelFactory = func(_ context.Context, _, _ string) (modelpkg.Model, error) {
		return &demomodel.EchoModel{Prefix: "ok"}, nil
	}
	t.Cleanup(func() { multimodelAnthropicModelFactory = oldFactory })

	oldFatal := multimodelFatal
	multimodelFatal = func(_ ...any) { t.Fatalf("unexpected fatal") }
	t.Cleanup(func() { multimodelFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"07-multimodel"}

	main()
}

func TestMain_FatalsOnError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := multimodelFatal
	var called bool
	multimodelFatal = func(_ ...any) { called = true }
	t.Cleanup(func() { multimodelFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"07-multimodel"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

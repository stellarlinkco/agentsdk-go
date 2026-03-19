package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type reasoningErrCompleteModel struct {
	completeErr error
	streamErr   error
}

func (m reasoningErrCompleteModel) Complete(_ context.Context, _ model.Request) (*model.Response, error) {
	return nil, m.completeErr
}

func (m reasoningErrCompleteModel) CompleteStream(_ context.Context, _ model.Request, _ model.StreamHandler) error {
	return m.streamErr
}

type reasoningErrStreamModel struct{ streamErr error }

func (reasoningErrStreamModel) Complete(_ context.Context, _ model.Request) (*model.Response, error) {
	return &model.Response{Message: model.Message{Role: "assistant", Content: "ok"}, StopReason: "stop"}, nil
}

func (m reasoningErrStreamModel) CompleteStream(_ context.Context, _ model.Request, _ model.StreamHandler) error {
	return m.streamErr
}

type stubReasoningModel struct{}

func (stubReasoningModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return &model.Response{
		Message:    model.Message{Role: "assistant", Content: "ok", ReasoningContent: "reason"},
		StopReason: "stop",
	}, nil
}

func (stubReasoningModel) CompleteStream(context.Context, model.Request, model.StreamHandler) error {
	return nil
}

func TestRun_RequiresKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseProvider(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: nil, want: "openai"},
		{args: []string{"--provider=anthropic"}, want: "anthropic"},
		{args: []string{"-p=anthropic"}, want: "anthropic"},
		{args: []string{"--provider", "anthropic"}, want: "anthropic"},
		{args: []string{"-p", "anthropic"}, want: "anthropic"},
		{args: []string{"--provider"}, want: "openai"},
		{args: []string{"-p"}, want: "openai"},
	}
	for _, tc := range cases {
		if got := parseProvider(tc.args); got != tc.want {
			t.Fatalf("args=%v got=%q want=%q", tc.args, got, tc.want)
		}
	}
}

func TestPrintResponse_Nil(t *testing.T) {
	printResponse(nil)
}

func TestCreateOnlineModel(t *testing.T) {
	if _, err := createOnlineModel("", "openai"); err == nil {
		t.Fatalf("expected error")
	}
	if got, err := createOnlineModel("dummy", "openai"); err != nil || got == nil {
		t.Fatalf("expected model")
	}
	if got, err := createOnlineModel("dummy", "anthropic"); err != nil || got == nil {
		t.Fatalf("expected model")
	}
}

func TestCreateOnlineModel_OpenAIConstructorErrorIsWrapped(t *testing.T) {
	old := reasoningNewOpenAI
	reasoningNewOpenAI = func(model.OpenAIConfig) (model.Model, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { reasoningNewOpenAI = old })

	_, err := createOnlineModel("dummy", "openai")
	if err == nil || !strings.Contains(err.Error(), "create openai model:") {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateOnlineModel_AnthropicConstructorErrorIsWrapped(t *testing.T) {
	old := reasoningNewAnthropic
	reasoningNewAnthropic = func(model.AnthropicConfig) (model.Model, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { reasoningNewAnthropic = old })

	_, err := createOnlineModel("dummy", "anthropic")
	if err == nil || !strings.Contains(err.Error(), "create anthropic model:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Online_UsesInjectedModel(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "dummy")

	old := reasoningOnlineModel
	reasoningOnlineModel = func(_ string, _ string) (model.Model, error) { return stubReasoningModel{}, nil }
	t.Cleanup(func() { reasoningOnlineModel = old })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := run(ctx, []string{"--provider=anthropic"}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRun_Online_ModelFactoryErrorPropagates(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "dummy")

	old := reasoningOnlineModel
	reasoningOnlineModel = func(_ string, _ string) (model.Model, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { reasoningOnlineModel = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_Online_CompleteErrorIsWrapped(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "dummy")

	completeErr := errors.New("complete boom")
	streamErr := errors.New("stream boom")

	old := reasoningOnlineModel
	reasoningOnlineModel = func(_ string, _ string) (model.Model, error) {
		return reasoningErrCompleteModel{completeErr: completeErr, streamErr: streamErr}, nil
	}
	t.Cleanup(func() { reasoningOnlineModel = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "Complete:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_Online_CompleteStreamErrorIsWrapped(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "dummy")

	streamErr := errors.New("stream boom")

	old := reasoningOnlineModel
	reasoningOnlineModel = func(_ string, _ string) (model.Model, error) {
		return reasoningErrStreamModel{streamErr: streamErr}, nil
	}
	t.Cleanup(func() { reasoningOnlineModel = old })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := run(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "CompleteStream:") {
		t.Fatalf("err=%v", err)
	}
}

func TestMain_DoesNotFatal(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "dummy")

	oldModel := reasoningOnlineModel
	reasoningOnlineModel = func(_ string, _ string) (model.Model, error) { return stubReasoningModel{}, nil }
	t.Cleanup(func() { reasoningOnlineModel = oldModel })

	oldFatal := reasoningFatal
	reasoningFatal = func(_ ...any) { t.Fatalf("unexpected fatal") }
	t.Cleanup(func() { reasoningFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"11-reasoning"}

	main()
}

func TestMain_FatalsOnError(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")

	oldFatal := reasoningFatal
	var called bool
	reasoningFatal = func(_ ...any) { called = true }
	t.Cleanup(func() { reasoningFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"11-reasoning"}

	main()
	if !called {
		t.Fatalf("expected fatal")
	}
}

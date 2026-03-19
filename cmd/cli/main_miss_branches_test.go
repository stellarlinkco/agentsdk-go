package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type noOpModel struct{}

func (noOpModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return &model.Response{Message: model.Message{Role: "assistant", Content: "ok"}, StopReason: "end_turn"}, nil
}

func (noOpModel) CompleteStream(context.Context, model.Request, model.StreamHandler) error {
	return nil
}

func TestNewRuntimeUsesAPIConstructor(t *testing.T) {
	rt, err := newRuntime(context.Background(), api.Options{
		ProjectRoot: t.TempDir(),
		ModelFactory: api.ModelFactoryFunc(func(context.Context) (model.Model, error) {
			return noOpModel{}, nil
		}),
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if rt == nil {
		t.Fatalf("expected non-nil runtime")
	}
	_ = rt.Close()
}

func TestRunFlagParseError(t *testing.T) {
	err := run([]string{"--not-a-flag"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestRunAgentsDirSetsSettingsPath(t *testing.T) {
	orig := newRuntime
	t.Cleanup(func() { newRuntime = orig })

	agentsDir := t.TempDir()
	got := ""
	newRuntime = func(ctx context.Context, options api.Options) (runtimeRunner, error) {
		_ = ctx
		got = options.SettingsPath
		return fakeRuntime{
			runFn: func(context.Context, api.Request) (*api.Response, error) {
				return &api.Response{
					Mode: api.ModeContext{EntryPoint: api.EntryPointCLI},
					Result: &api.Result{
						Output:     "ok",
						StopReason: "done",
					},
				}, nil
			},
		}, nil
	}

	if err := run([]string{"--prompt=hi", "--agents", agentsDir}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := filepath.Join(agentsDir, "settings.json")
	if got != want {
		t.Fatalf("settingsPath=%q, want %q", got, want)
	}
}

func TestRunRejectsWhitespacePromptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("   \n\t"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := run([]string{"--prompt-file", path}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "prompt is empty") {
		t.Fatalf("err=%v, want prompt is empty", err)
	}
}

func TestRunPropagatesRuntimeRunError(t *testing.T) {
	orig := newRuntime
	t.Cleanup(func() { newRuntime = orig })

	newRuntime = func(context.Context, api.Options) (runtimeRunner, error) {
		return fakeRuntime{
			runFn: func(context.Context, api.Request) (*api.Response, error) {
				return nil, errors.New("boom")
			},
		}, nil
	}

	var stdout bytes.Buffer
	err := run([]string{"--prompt=hi"}, &stdout, io.Discard)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err=%v, want boom", err)
	}
}

func TestStreamRunPropagatesRunStreamError(t *testing.T) {
	rt := fakeRuntime{
		runStreamFn: func(context.Context, api.Request) (<-chan api.StreamEvent, error) {
			return nil, errors.New("stream boom")
		},
	}
	err := streamRun(rt, api.Request{Prompt: "x"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "stream boom") {
		t.Fatalf("err=%v, want stream boom", err)
	}
}

func TestResolvePromptPropagatesStdinStatError(t *testing.T) {
	old := os.Stdin
	t.Cleanup(func() { os.Stdin = old })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()
	_ = r.Close()
	os.Stdin = r

	_, statErr := resolvePrompt("", "", nil)
	if statErr == nil {
		t.Fatalf("expected stat error")
	}
}

func TestPrintResponseNilGuards(t *testing.T) {
	printResponse(nil, nil)
	printResponse(&api.Response{}, nil)
}

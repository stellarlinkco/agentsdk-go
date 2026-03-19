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
)

type fakeRuntime struct {
	runFn       func(context.Context, api.Request) (*api.Response, error)
	runStreamFn func(context.Context, api.Request) (<-chan api.StreamEvent, error)
	closeFn     func() error
}

func (f fakeRuntime) Run(ctx context.Context, req api.Request) (*api.Response, error) {
	if f.runFn == nil {
		return nil, errors.New("no Run")
	}
	return f.runFn(ctx, req)
}

func (f fakeRuntime) RunStream(ctx context.Context, req api.Request) (<-chan api.StreamEvent, error) {
	if f.runStreamFn == nil {
		return nil, errors.New("no RunStream")
	}
	return f.runStreamFn(ctx, req)
}

func (f fakeRuntime) Close() error {
	if f.closeFn == nil {
		return nil
	}
	return f.closeFn()
}

func TestResolvePrompt_Precedence(t *testing.T) {
	got, err := resolvePrompt("literal", "", []string{"tail"})
	if err != nil || got != "literal" {
		t.Fatalf("got=%q err=%v", got, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err = resolvePrompt("", path, []string{"tail"})
	if err != nil || got != "from-file" {
		t.Fatalf("got=%q err=%v", got, err)
	}

	got, err = resolvePrompt("", "", []string{"a", "b"})
	if err != nil || got != "a b" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestResolvePrompt_FromStdin(t *testing.T) {
	old := os.Stdin
	t.Cleanup(func() { os.Stdin = old })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	if _, err := w.WriteString("a\nb\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	got, err := resolvePrompt("", "", nil)
	if err != nil || got != "a\nb" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestResolvePrompt_PromptFileError(t *testing.T) {
	_, err := resolvePrompt("", "/path/does-not-exist", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseTags(t *testing.T) {
	if got := parseTags(nil); got != nil {
		t.Fatalf("expected nil, got=%v", got)
	}
	got := parseTags(multiValue{"a=b", "flag", " =ignored", "a=c"})
	if got["a"] != "c" || got["flag"] != "true" {
		t.Fatalf("unexpected tags=%v", got)
	}
}

func TestMultiValue(t *testing.T) {
	var mv multiValue
	if err := mv.Set("a"); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := mv.Set("b"); err != nil {
		t.Fatalf("set b: %v", err)
	}
	if mv.String() != "a,b" {
		t.Fatalf("String=%q", mv.String())
	}
}

func TestStreamRun_EncodeError(t *testing.T) {
	rt := fakeRuntime{
		runStreamFn: func(ctx context.Context, req api.Request) (<-chan api.StreamEvent, error) {
			ch := make(chan api.StreamEvent, 1)
			ch <- api.StreamEvent{Type: api.EventPing, Output: "x"}
			close(ch)
			return ch, nil
		},
	}
	err := streamRun(rt, api.Request{Prompt: "x"}, errWriter{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NonACP_StreamAndNonStream(t *testing.T) {
	orig := newRuntime
	t.Cleanup(func() { newRuntime = orig })

	newRuntime = func(ctx context.Context, options api.Options) (runtimeRunner, error) {
		_ = options
		return fakeRuntime{
			runFn: func(ctx context.Context, req api.Request) (*api.Response, error) {
				_ = ctx
				if strings.TrimSpace(req.Prompt) == "" {
					return nil, errors.New("empty")
				}
				return &api.Response{
					Mode: api.ModeContext{EntryPoint: api.EntryPointCLI},
					Result: &api.Result{
						Output:     "ok",
						StopReason: "done",
					},
				}, nil
			},
			runStreamFn: func(ctx context.Context, req api.Request) (<-chan api.StreamEvent, error) {
				_ = ctx
				_ = req
				ch := make(chan api.StreamEvent, 2)
				ch <- api.StreamEvent{Type: api.EventAgentStart, Output: "p"}
				ch <- api.StreamEvent{Type: api.EventMessageStop, Output: "d"}
				close(ch)
				return ch, nil
			},
			closeFn: func() error { return nil },
		}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--prompt=hi", "--tag", "a=b"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "agentsdk run") {
		t.Fatalf("stdout=%q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"--prompt=hi", "--stream=true"}, &stdout, &stderr); err != nil {
		t.Fatalf("run stream: %v", err)
	}
	if !strings.Contains(stdout.String(), "\"type\"") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRun_NewRuntimeError(t *testing.T) {
	orig := newRuntime
	t.Cleanup(func() { newRuntime = orig })

	newRuntime = func(ctx context.Context, options api.Options) (runtimeRunner, error) {
		_ = ctx
		_ = options
		return nil, errors.New("boom")
	}

	err := run([]string{"--prompt=hi"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "create runtime") {
		t.Fatalf("err=%v", err)
	}
}

func TestMain_ExitOnError(t *testing.T) {
	origExit := osExit
	origArgs := os.Args
	origRuntime := newRuntime
	origStdout := os.Stdout
	origStderr := os.Stderr
	t.Cleanup(func() {
		osExit = origExit
		os.Args = origArgs
		newRuntime = origRuntime
		os.Stdout = origStdout
		os.Stderr = origStderr
	})

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	_ = rOut.Close()
	os.Stdout = wOut

	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	_ = rErr.Close()
	os.Stderr = wErr

	os.Args = []string{"agentsdk-cli", "--prompt="} // prompt empty -> error -> exit(1)
	newRuntime = func(ctx context.Context, options api.Options) (runtimeRunner, error) {
		_ = ctx
		_ = options
		return fakeRuntime{}, nil
	}

	exitCode := 0
	osExit = func(code int) { exitCode = code }

	main()
	if exitCode != 1 {
		t.Fatalf("exitCode=%d", exitCode)
	}
	_ = wOut.Close()
	_ = wErr.Close()
}

func TestMain_NoExitOnSuccess(t *testing.T) {
	origExit := osExit
	origArgs := os.Args
	origRuntime := newRuntime
	origStdout := os.Stdout
	origStderr := os.Stderr
	t.Cleanup(func() {
		osExit = origExit
		os.Args = origArgs
		newRuntime = origRuntime
		os.Stdout = origStdout
		os.Stderr = origStderr
	})

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	_ = rOut.Close()
	os.Stdout = wOut

	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	_ = rErr.Close()
	os.Stderr = wErr

	os.Args = []string{"agentsdk-cli", "--prompt=hi"}
	newRuntime = func(ctx context.Context, options api.Options) (runtimeRunner, error) {
		_ = ctx
		_ = options
		return fakeRuntime{
			runFn: func(ctx context.Context, req api.Request) (*api.Response, error) {
				_ = ctx
				_ = req
				return &api.Response{Mode: api.ModeContext{EntryPoint: api.EntryPointCLI}}, nil
			},
		}, nil
	}

	exitCalled := false
	osExit = func(code int) { _ = code; exitCalled = true }

	main()
	if exitCalled {
		t.Fatalf("unexpected exit")
	}
	_ = wOut.Close()
	_ = wErr.Close()
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

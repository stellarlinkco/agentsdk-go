package toolbuiltin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBashToolStreamExecute(t *testing.T) {
	t.Parallel()

	tool := NewBashToolWithSandbox("", nil)
	tool.SetOutputThresholdBytes(1)

	var out []string
	res, err := tool.StreamExecute(context.Background(), map[string]interface{}{
		"command": "echo hello",
	}, func(chunk string, _ bool) {
		out = append(out, chunk)
	})
	if err != nil {
		t.Fatalf("stream execute failed: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	if res.Output == "" {
		data, ok := res.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected data map, got %T", res.Data)
		}
		if _, ok := data["output_file"]; !ok {
			t.Fatalf("expected output text or output_file reference")
		}
	}
}

func TestOpenCommandPipesErrors(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("bash", "-c", "echo ok")
	cmd.Stdout = &bytes.Buffer{}
	if _, _, err := openCommandPipes(cmd); err == nil {
		t.Fatalf("expected stdout pipe error")
	}

	cmd = exec.Command("bash", "-c", "echo ok")
	cmd.Stderr = &bytes.Buffer{}
	if _, _, err := openCommandPipes(cmd); err == nil {
		t.Fatalf("expected stderr pipe error")
	}
}

func TestBashToolStreamExecuteOpenPipesError(t *testing.T) {
	t.Parallel()

	tool := NewBashToolWithSandbox("", nil)
	tool.openPipes = func(*exec.Cmd) (io.ReadCloser, io.ReadCloser, error) {
		return nil, nil, errors.New("pipes failed")
	}
	if _, err := tool.StreamExecute(context.Background(), map[string]any{"command": "echo hi"}, nil); err == nil || !strings.Contains(err.Error(), "pipes failed") {
		t.Fatalf("expected open pipes error, got %v", err)
	}
}

func TestBashToolStreamExecuteJoinsReadErrors(t *testing.T) {
	t.Parallel()

	tool := NewBashToolWithSandbox("", nil)
	tool.openPipes = func(*exec.Cmd) (io.ReadCloser, io.ReadCloser, error) {
		return &errReadCloser{err: errors.New("stdout failed")}, &errReadCloser{err: errors.New("stderr failed")}, nil
	}
	_, err := tool.StreamExecute(context.Background(), map[string]any{"command": "echo hi"}, nil)
	if err == nil || !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("expected command failed error, got %v", err)
	}
}

func TestBashToolStreamExecuteErrors(t *testing.T) {
	t.Parallel()

	if _, err := (*BashTool)(nil).StreamExecute(context.Background(), nil, nil); err == nil {
		t.Fatalf("expected nil tool error")
	}
	if _, err := NewBashToolWithSandbox("", nil).StreamExecute(nil, nil, nil); err == nil {
		t.Fatalf("expected nil context error")
	}
	if _, err := NewBashToolWithSandbox("", nil).StreamExecute(context.Background(), map[string]any{}, nil); err == nil {
		t.Fatalf("expected missing command error")
	}

	root := t.TempDir()
	if _, err := NewBashToolWithRoot(root).StreamExecute(context.Background(), map[string]any{
		"command": "echo ok | cat",
	}, nil); err == nil {
		t.Fatalf("expected command error")
	}

	if _, err := NewBashToolWithSandbox("", nil).StreamExecute(context.Background(), map[string]any{
		"command": "printf 'hi'",
		"timeout": "bad",
	}, nil); err == nil {
		t.Fatalf("expected timeout parsing error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewBashToolWithSandbox("", nil).StreamExecute(ctx, map[string]interface{}{
		"command": "printf 'hi'",
	}, nil)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestBashToolStreamExecuteCancelAfterStart(t *testing.T) {
	t.Parallel()

	tool := NewBashToolWithSandbox("", nil)
	tool.AllowShellMetachars(true)

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		_, err := tool.StreamExecute(ctx, map[string]any{"command": "echo start; sleep 10"}, func(string, bool) {
			select {
			case <-started:
			default:
				close(started)
			}
		})
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("command did not start producing output")
	}
	cancel()

	select {
	case err := <-errCh:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("StreamExecute did not return after cancel")
	}
}

func TestBashToolStreamExecuteCommandFailed(t *testing.T) {
	t.Parallel()

	tool := NewBashToolWithSandbox("", nil)
	_, err := tool.StreamExecute(context.Background(), map[string]any{"command": "exit 7"}, nil)
	if err == nil || !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("expected command failed error, got %v", err)
	}
}

func TestBashToolStreamExecuteTimeoutDoesNotHangWithBackgroundChild(t *testing.T) {
	t.Parallel()

	tool := NewBashToolWithSandbox("", nil)
	tool.AllowShellMetachars(true)

	started := time.Now()
	res, err := tool.StreamExecute(context.Background(), map[string]interface{}{
		"command": "sleep 6 & while true; do sleep 1; done",
		"timeout": 0.1,
	}, nil)
	elapsed := time.Since(started)

	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if res == nil || res.Success {
		t.Fatalf("expected failed result, got %#v", res)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("stream execute drained too slowly after timeout: %s", elapsed)
	}
}

func TestConsumeStreamBreaksOnCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10 {
			if _, err := pw.Write([]byte("line\n")); err != nil {
				return
			}
		}
		_ = pw.Close()
	}()

	seen := 0
	err := consumeStream(ctx, pr, func(string, bool) {
		seen++
		if seen == 2 {
			cancel()
		}
	}, nil, false)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	<-done
}

func TestBashToolStreamExecuteInvalidWorkdir(t *testing.T) {
	tool := NewBashToolWithSandbox("", nil)
	if _, err := tool.StreamExecute(context.Background(), map[string]interface{}{
		"command": "printf 'hi'",
		"workdir": "/path/does-not-exist",
	}, nil); err == nil {
		t.Fatalf("expected workdir error")
	}
}

func TestConsumeStreamReadError(t *testing.T) {
	reader := &errReadCloser{err: errors.New("read failed")}
	if err := consumeStream(context.Background(), reader, nil, nil, false); err == nil {
		t.Fatalf("expected read error")
	}
}

type errReadCloser struct {
	err error
}

func (e *errReadCloser) Read([]byte) (int, error) { return 0, e.err }
func (e *errReadCloser) Close() error             { return nil }

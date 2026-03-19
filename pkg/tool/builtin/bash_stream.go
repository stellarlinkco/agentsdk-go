package toolbuiltin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

func openCommandPipes(cmd *exec.Cmd) (io.ReadCloser, io.ReadCloser, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}
	return stdoutPipe, stderrPipe, nil
}

// StreamExecute runs the bash command while emitting incremental output. It
// preserves backwards compatibility by sharing validation and metadata with
// Execute, and spools output to disk after crossing the configured threshold.
func (b *BashTool) StreamExecute(ctx context.Context, params map[string]interface{}, emit func(chunk string, isStderr bool)) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if b == nil || b.validator == nil {
		return nil, errors.New("bash tool is not initialised")
	}

	command, err := extractCommand(params)
	if err != nil {
		return nil, err
	}
	if err := b.validator.Validate(command); err != nil {
		return nil, err
	}
	workdir, err := b.resolveWorkdir(params)
	if err != nil {
		return nil, err
	}
	timeout, err := b.resolveTimeout(params)
	if err != nil {
		return nil, err
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, "bash", "-c", command)
	cmd.Env = os.Environ()
	cmd.Dir = workdir

	openPipes := b.openPipes
	if openPipes == nil {
		openPipes = openCommandPipes
	}
	stdoutPipe, stderrPipe, err := openPipes(cmd)
	if err != nil {
		return nil, err
	}

	spool := newBashOutputSpool(ctx, b.effectiveOutputThresholdBytes())
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	readCtx, stopReads := context.WithCancel(execCtx)
	defer stopReads()
	defer stdoutPipe.Close()
	defer stderrPipe.Close()

	var closePipesOnce sync.Once
	closePipes := func() {
		closePipesOnce.Do(func() {
			_ = stdoutPipe.Close()
			_ = stderrPipe.Close()
		})
	}

	cancelWatcherDone := make(chan struct{})
	go func() {
		select {
		case <-execCtx.Done():
			// Unblock scanners promptly when timeout/cancel fires. This keeps the
			// old read-before-wait ordering (better output reliability) while
			// preventing wg.Wait from hanging on inherited child FDs.
			stopReads()
			closePipes()
		case <-cancelWatcherDone:
		}
	}()

	var stdoutErr, stderrErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		stdoutErr = consumeStream(readCtx, stdoutPipe, emit, spool, false)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		stderrErr = consumeStream(readCtx, stderrPipe, emit, spool, true)
	}()

	wg.Wait()
	close(cancelWatcherDone)
	waitErr := cmd.Wait()

	duration := time.Since(start)

	runErr := waitErr
	if stdoutErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("stdout read: %w", stdoutErr))
	}
	if stderrErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("stderr read: %w", stderrErr))
	}

	output, outputFile, spoolErr := spool.Finalize()

	data := map[string]interface{}{
		"workdir":     workdir,
		"duration_ms": duration.Milliseconds(),
		"timeout_ms":  timeout.Milliseconds(),
	}
	if outputFile != "" {
		data["output_file"] = outputFile
	}
	if spoolErr != nil {
		data["spool_error"] = spoolErr.Error()
	}

	result := &tool.ToolResult{
		Success: runErr == nil,
		Output:  output,
		Data:    data,
	}

	if runErr != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return result, fmt.Errorf("command timeout after %s", timeout)
		}
		if errors.Is(execCtx.Err(), context.Canceled) {
			return result, execCtx.Err()
		}
		return result, fmt.Errorf("command failed: %w", runErr)
	}
	return result, nil
}

func consumeStream(ctx context.Context, r io.ReadCloser, emit func(chunk string, isStderr bool), spool *bashOutputSpool, isStderr bool) error {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if emit != nil {
			emit(line, isStderr)
		}
		if spool != nil {
			_ = spool.Append(line, isStderr) //nolint:errcheck // best-effort spool
			_ = spool.Append("\n", isStderr) //nolint:errcheck // best-effort spool
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		if ctx.Err() != nil || errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
			return nil
		}
		return err
	}
	return nil
}

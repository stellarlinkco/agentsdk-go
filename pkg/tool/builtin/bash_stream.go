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

	"github.com/cexll/agentsdk-go/pkg/tool"
)

// StreamExecute runs the bash command while emitting incremental output. It
// preserves backwards compatibility by sharing validation and metadata with
// Execute, and spools output to disk after crossing the configured threshold.
func (b *BashTool) StreamExecute(ctx context.Context, params map[string]interface{}, emit func(chunk string, isStderr bool)) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if b == nil || b.sandbox == nil {
		return nil, errors.New("bash tool is not initialised")
	}

	command, err := extractCommand(params)
	if err != nil {
		return nil, err
	}
	if err := b.sandbox.ValidateCommand(command); err != nil {
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

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	spool := newBashOutputSpool(ctx, b.effectiveOutputThresholdBytes())
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	var stdoutErr, stderrErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		stdoutErr = consumeStream(execCtx, stdoutPipe, emit, spool, false)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		stderrErr = consumeStream(execCtx, stderrPipe, emit, spool, true)
	}()

	wg.Wait()
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
			_ = spool.Append(line, isStderr)
			_ = spool.Append("\n", isStderr)
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

package toolbuiltin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
)

func TestBashToolSetCommandLimitsAffectsValidator(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewBashToolWithRoot(dir)

	tool.SetCommandLimits(0, 2)
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo a b c"}); err == nil || !strings.Contains(err.Error(), "too many arguments") {
		t.Fatalf("expected args limit error, got %v", err)
	}

	tool.SetCommandLimits(0, 0)
	res, err := tool.Execute(context.Background(), map[string]any{"command": "echo ok"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res == nil || !res.Success {
		t.Fatalf("res=%v", res)
	}
}

func TestBashToolExecuteInvalidTimeout(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewBashToolWithRoot(dir)
	_, err := tool.Execute(context.Background(), map[string]any{"command": "echo ok", "timeout": struct{}{}})
	if err == nil || !strings.Contains(err.Error(), "invalid timeout") {
		t.Fatalf("err=%v", err)
	}
}

func TestBashToolExecuteZeroTimeoutUsesDefault(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewBashToolWithRoot(dir)

	res, err := tool.Execute(context.Background(), map[string]any{"command": "echo ok", "timeout": 0})
	if err != nil || res == nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("data=%T", res.Data)
	}
	timeoutMS, ok := data["timeout_ms"].(int64)
	if !ok || timeoutMS != defaultBashTimeout.Milliseconds() {
		t.Fatalf("timeout_ms=%v", data["timeout_ms"])
	}
}

func TestBashToolExecuteSpoolsLargeOutputToFile(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewBashToolWithRoot(dir)
	tool.SetOutputThresholdBytes(1)

	ctx := context.WithValue(context.Background(), middleware.SessionIDContextKey, "spool-large")
	res, err := tool.Execute(ctx, map[string]any{"command": "printf hello"})
	if err != nil || res == nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("data=%T", res.Data)
	}
	path, ok := data["output_file"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		t.Fatalf("output_file=%v", data["output_file"])
	}
	if !strings.Contains(res.Output, "Output saved") {
		t.Fatalf("output=%q", res.Output)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("stat output file: %v", statErr)
	}
}

func TestBashToolExecuteIncludesSpoolError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only base dir")
	}
	dir := cleanTempDir(t)
	tool := NewBashToolWithRoot(dir)
	tool.SetOutputThresholdBytes(1)

	sessionID := "spool-error-" + time.Now().Format("150405.000000000")
	base := filepath.Join(string(filepath.Separator), "tmp", "agentsdk", "bash-output")
	sessionPath := filepath.Join(base, sanitizePathComponent(sessionID))
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	if err := os.WriteFile(sessionPath, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write session marker: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(sessionPath) })

	ctx := context.WithValue(context.Background(), middleware.SessionIDContextKey, sessionID)
	res, err := tool.Execute(ctx, map[string]any{"command": "printf hello"})
	if err != nil || res == nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("data=%T", res.Data)
	}
	spoolErr, ok := data["spool_error"].(string)
	if !ok || strings.TrimSpace(spoolErr) == "" {
		t.Fatalf("expected spool_error, data=%v", data)
	}
}

func TestBashToolExecuteCommandFailure(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewBashToolWithRoot(dir)

	res, err := tool.Execute(context.Background(), map[string]any{"command": "false"})
	if err == nil || res == nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if res.Success {
		t.Fatalf("expected Success=false")
	}
	if !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("err=%v", err)
	}
}

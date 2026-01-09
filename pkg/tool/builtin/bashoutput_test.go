package toolbuiltin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/middleware"
)

func TestBashOutputReturnsNewLines(t *testing.T) {
	store := newShellStore()
	handle, err := store.Register("shell-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := handle.Append(ShellStreamStdout, "line1\nline2"); err != nil {
		t.Fatalf("append stdout: %v", err)
	}
	if err := handle.Append(ShellStreamStderr, "err1"); err != nil {
		t.Fatalf("append stderr: %v", err)
	}
	tool := NewBashOutputTool(store)
	res, err := tool.Execute(context.Background(), map[string]interface{}{"bash_id": "shell-1"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Output, "line1") || !strings.Contains(res.Output, "[stderr] err1") {
		t.Fatalf("unexpected output:\n%s", res.Output)
	}
	data := res.Data.(map[string]interface{})
	if stdout, _ := data["stdout"].(string); !strings.Contains(stdout, "line1") {
		t.Fatalf("stdout missing: %#v", stdout)
	}
	if stderr, _ := data["stderr"].(string); stderr != "err1" {
		t.Fatalf("stderr mismatch: %q", stderr)
	}
	// Second call should report no new output.
	res, err = tool.Execute(context.Background(), map[string]interface{}{"bash_id": "shell-1"})
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if !strings.Contains(res.Output, "no new output") {
		t.Fatalf("expected no new output, got %s", res.Output)
	}
}

func TestBashOutputAppliesFilterAndDropsLines(t *testing.T) {
	store := newShellStore()
	handle, err := store.Register("shell-2")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := handle.Append(ShellStreamStdout, "match\nskip\nmatch-again"); err != nil {
		t.Fatalf("append: %v", err)
	}
	tool := NewBashOutputTool(store)
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"bash_id": "shell-2",
		"filter":  "match",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	data := res.Data.(map[string]interface{})
	lines, ok := data["lines"].([]ShellLine)
	if !ok {
		t.Fatalf("expected []ShellLine, got %T", data["lines"])
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if dropped, _ := data["dropped_lines"].(int); dropped != 1 {
		t.Fatalf("expected dropped=1, got %v", data["dropped_lines"])
	}
}

func TestBashOutputUnknownShell(t *testing.T) {
	tool := NewBashOutputTool(newShellStore())
	_, err := tool.Execute(context.Background(), map[string]interface{}{"bash_id": "missing"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestBashOutputMetadataAndDefaults(t *testing.T) {
	tool := NewBashOutputTool(nil)
	if tool.Name() != "BashOutput" {
		t.Fatalf("unexpected name %q", tool.Name())
	}
	if tool.Schema() == nil || tool.Description() == "" {
		t.Fatalf("missing schema or description")
	}
	if DefaultShellStore() == nil {
		t.Fatalf("default shell store is nil")
	}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{}); err == nil {
		t.Fatalf("expected error for missing bash_id")
	}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"bash_id": "x", "filter": "["}); err == nil {
		t.Fatalf("expected regex error")
	}
}

func TestShellStoreFail(t *testing.T) {
	store := newShellStore()
	handle, err := store.Register("shell-err")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := handle.Fail(errors.New("boom")); err != nil {
		t.Fatalf("handle fail: %v", err)
	}
	tool := NewBashOutputTool(store)
	res, err := tool.Execute(context.Background(), map[string]interface{}{"bash_id": "shell-err"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Output, "failed") {
		t.Fatalf("expected failure output, got %s", res.Output)
	}
}

func TestShellStoreConcurrentAppend(t *testing.T) {
	store := newShellStore()
	handle, err := store.Register("shell-3")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(5)
	chunks := []string{"a\nb", "c", "d\ne", "f", "g"}
	for idx := range chunks {
		chunk := chunks[idx]
		go func() {
			defer wg.Done()
			if err := handle.Append(ShellStreamStdout, chunk); err != nil {
				t.Errorf("append error: %v", err)
			}
		}()
	}
	wg.Wait()
	handle.Close(0)
	tool := NewBashOutputTool(store)
	res, err := tool.Execute(context.Background(), map[string]interface{}{"bash_id": "shell-3"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	data := res.Data.(map[string]interface{})
	lines, ok := data["lines"].([]ShellLine)
	if !ok {
		t.Fatalf("expected []ShellLine type, got %T", data["lines"])
	}
	if len(lines) != len(strings.Split(strings.Join(chunks, "\n"), "\n")) {
		t.Fatalf("expected %d lines, got %d", len(strings.Split(strings.Join(chunks, "\n"), "\n")), len(lines))
	}
}

func TestShellHandleCloseAndFail(t *testing.T) {
	store := newShellStore()
	handle, err := store.Register("shell-handle")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := handle.Close(0); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := handle.Fail(nil); err != nil {
		t.Fatalf("fail: %v", err)
	}
	var nilHandle *ShellHandle
	if err := nilHandle.Close(0); err == nil {
		t.Fatalf("expected error for nil handle close")
	}
	if err := nilHandle.Fail(nil); err == nil {
		t.Fatalf("expected error for nil handle fail")
	}
}

func TestBashOutputExecuteErrors(t *testing.T) {
	tool := &BashOutputTool{}
	if _, err := tool.Execute(nil, map[string]interface{}{}); err == nil {
		t.Fatalf("expected context error")
	}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"bash_id": "   "}); err == nil {
		t.Fatalf("expected whitespace id error")
	}
}

func TestBashOutputReadsAsyncTaskOutput(t *testing.T) {
	skipIfWindows(t)
	defaultAsyncTaskManager = newAsyncTaskManager()
	dir := cleanTempDir(t)
	bash := NewBashToolWithRoot(dir)
	asyncRes, err := bash.Execute(context.Background(), map[string]interface{}{
		"command": "echo async-line",
		"async":   true,
	})
	if err != nil {
		t.Fatalf("start async bash: %v", err)
	}
	id := asyncRes.Data.(map[string]interface{})["task_id"].(string)

	outTool := NewBashOutputTool(newShellStore())
	var got string
	for i := 0; i < 50; i++ {
		res, err := outTool.Execute(context.Background(), map[string]interface{}{"task_id": id})
		if err != nil {
			t.Fatalf("bashoutput: %v", err)
		}
		data := res.Data.(map[string]interface{})
		if chunk, _ := data["output"].(string); strings.Contains(chunk, "async-line") {
			got = chunk
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(got, "async-line") {
		t.Fatalf("expected async output, got %q", got)
	}

	// BashOutput should also accept bash_id for async tasks.
	res, err := outTool.Execute(context.Background(), map[string]interface{}{"bash_id": id})
	if err != nil {
		t.Fatalf("bashoutput with bash_id: %v", err)
	}
	if status, _ := res.Data.(map[string]interface{})["status"].(string); status == "" {
		t.Fatalf("expected status in async read")
	}
}

func TestAsyncBashOutputReturnsPathReferenceForLargeOutput(t *testing.T) {
	skipIfWindows(t)
	defaultAsyncTaskManager = newAsyncTaskManager()
	ctx := context.WithValue(context.Background(), middleware.SessionIDContextKey, "session-bashoutput")
	command := fmt.Sprintf("yes B | head -c %d", maxAsyncOutputLen+4096)

	if err := defaultAsyncTaskManager.startWithContext(ctx, "task-large-out", command, "", 0); err != nil {
		t.Fatalf("start task: %v", err)
	}
	task, _ := defaultAsyncTaskManager.lookup("task-large-out")
	select {
	case <-task.Done:
	case <-time.After(5 * time.Second):
		t.Fatalf("task did not complete")
	}

	outTool := NewBashOutputTool(newShellStore())
	res, err := outTool.Execute(ctx, map[string]interface{}{"task_id": "task-large-out"})
	if err != nil {
		t.Fatalf("bashoutput: %v", err)
	}
	if !strings.Contains(res.Output, "Output saved to:") {
		t.Fatalf("expected output reference, got %q", res.Output)
	}
	data := res.Data.(map[string]interface{})
	if chunk, _ := data["output"].(string); chunk != "" {
		t.Fatalf("expected empty output chunk, got %d bytes", len(chunk))
	}
	outputFile, _ := data["output_file"].(string)
	if strings.TrimSpace(outputFile) == "" {
		t.Fatalf("expected output_file in result data")
	}
	t.Cleanup(func() { _ = os.Remove(outputFile) })

	wantDir := filepath.Join(bashOutputBaseDir(), "session-bashoutput") + string(filepath.Separator)
	if !strings.Contains(filepath.Clean(outputFile)+string(filepath.Separator), wantDir) {
		t.Fatalf("expected output file under %q, got %q", wantDir, outputFile)
	}
	info, err := os.Stat(outputFile)
	if err != nil {
		t.Fatalf("stat output file: %v", err)
	}
	if info.Size() <= int64(maxAsyncOutputLen) {
		t.Fatalf("expected output file > %d bytes, got %d", maxAsyncOutputLen, info.Size())
	}
	f, err := os.Open(outputFile)
	if err != nil {
		t.Fatalf("open output file: %v", err)
	}
	defer f.Close()
	var buf [1]byte
	if _, err := io.ReadFull(f, buf[:]); err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if buf[0] != 'B' {
		t.Fatalf("unexpected file prefix %q", string(buf[:]))
	}
}

func TestShellStoreDuplicateRegister(t *testing.T) {
	store := newShellStore()
	if _, err := store.Register("dup"); err != nil {
		t.Fatalf("register dup: %v", err)
	}
	if _, err := store.Register("dup"); err == nil {
		t.Fatalf("expected duplicate error")
	}
}

func TestShellStoreAppendAfterClose(t *testing.T) {
	store := newShellStore()
	if err := store.Append("auto", ShellStreamStdout, ""); err != nil {
		t.Fatalf("append empty chunk: %v", err)
	}
	handle, err := store.Register("app")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := handle.Close(0); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := handle.Append(ShellStreamStdout, "line"); err != nil {
		t.Fatalf("append after close: %v", err)
	}
	res, err := store.Consume("app", nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if len(res.Lines) != 1 {
		t.Fatalf("expected single line, got %d", len(res.Lines))
	}
}

func TestSplitLinesHandlesWindowsNewlines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("already covered by native newline handling")
	}
	lines := splitLines("a\r\nb\rc")
	if len(lines) != 3 || lines[1] != "b" {
		t.Fatalf("unexpected split result %v", lines)
	}
	store := newShellStore()
	if err := store.Close("missing", 0); err == nil {
		t.Fatalf("expected error closing missing shell")
	}
	if err := store.Fail("missing", errors.New("boom")); err == nil {
		t.Fatalf("expected error failing missing shell")
	}
}

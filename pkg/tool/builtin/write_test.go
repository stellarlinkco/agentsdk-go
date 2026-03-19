package toolbuiltin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteToolCreatesFile(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewWriteToolWithRoot(dir)

	target := filepath.Join("nested", "note.txt")
	content := "payload"
	res, err := tool.Execute(context.Background(), map[string]any{
		"file_path": target,
		"content":   content,
	})
	if err != nil {
		t.Fatalf("write execute failed: %v", err)
	}
	if !strings.Contains(res.Output, "wrote") {
		t.Fatalf("unexpected output: %q", res.Output)
	}
	abs := filepath.Join(dir, target)
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if string(data) != content {
		t.Fatalf("content mismatch: got %q", string(data))
	}
}

func TestWriteToolValidationErrors(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewWriteToolWithRoot(dir)

	testCases := []struct {
		name    string
		params  map[string]any
		message string
	}{
		{"nil params", nil, "params"},
		{"missing path", map[string]any{"content": "x"}, "file_path"},
		{"missing content", map[string]any{"file_path": "file.txt"}, "content"},
		{"content type", map[string]any{"file_path": "file.txt", "content": 123}, "content must be string"},
		{"sandbox", map[string]any{"file_path": filepath.Join("..", "escape.txt"), "content": "x"}, "path denied"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected error containing %q, got %v", tc.message, err)
			}
		})
	}

	if _, err := tool.Execute(nil, map[string]any{"file_path": "file.txt", "content": "x"}); err == nil {
		t.Fatalf("expected nil context error")
	}

	var uninitialised WriteTool
	if _, err := uninitialised.Execute(context.Background(), map[string]any{"file_path": "file.txt", "content": "x"}); err == nil {
		t.Fatalf("expected not initialised error")
	}
}

func TestWriteToolSizeLimitAndCancellation(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewWriteToolWithRoot(dir)
	tool.base.maxBytes = 1

	if _, err := tool.Execute(context.Background(), map[string]any{"file_path": "file.txt", "content": "too long"}); err == nil {
		t.Fatalf("expected size limit error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, map[string]any{"file_path": "file.txt", "content": "x"}); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
}

func TestWriteToolMetadata(t *testing.T) {
	tool := NewWriteTool()
	if tool.Name() != "write" {
		t.Fatalf("unexpected tool name %q", tool.Name())
	}
	if tool.Description() == "" || tool.Schema() == nil {
		t.Fatalf("metadata should be populated")
	}
}

func TestWriteToolHelperErrors(t *testing.T) {
	tool := NewWriteTool()
	if _, err := tool.parseContent(nil); err == nil {
		t.Fatalf("expected error for nil params")
	}
	if _, err := tool.parseContent(map[string]any{"content": 1}); err == nil {
		t.Fatalf("expected error for non-string content")
	}
}

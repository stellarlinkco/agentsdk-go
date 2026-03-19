package toolbuiltin

import (
	"context"
	"strings"
	"testing"
)

func TestBashToolRejectsUnsafeCommand(t *testing.T) {
	skipIfWindows(t)
	dir := cleanTempDir(t)
	tool := NewBashToolWithRoot(dir)
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo ok | cat"}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "metacharacters") {
		t.Fatalf("expected metacharacters rejection, got %v", err)
	}
}

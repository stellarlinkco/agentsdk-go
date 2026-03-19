package tool

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOutputPersisterMaybePersist_NilReceiverOrNilResult(t *testing.T) {
	t.Parallel()

	var p *OutputPersister
	if err := p.MaybePersist(Call{Name: "tool"}, &ToolResult{Output: "x"}); err != nil {
		t.Fatalf("expected nil error for nil persister, got %v", err)
	}

	p = &OutputPersister{BaseDir: t.TempDir(), DefaultThresholdBytes: 1}
	if err := p.MaybePersist(Call{Name: "tool"}, nil); err != nil {
		t.Fatalf("expected nil error for nil result, got %v", err)
	}
}

func TestOutputPersisterThresholdFor_NilReceiver(t *testing.T) {
	t.Parallel()

	var p *OutputPersister
	if got := p.thresholdFor("tool"); got != 0 {
		t.Fatalf("expected 0 threshold for nil receiver, got %d", got)
	}
}

func TestCreateToolOutputFileWithTimestamp_ErrExistThenCreate(t *testing.T) {
	dir := t.TempDir()
	ts := int64(1000)

	if err := os.WriteFile(filepath.Join(dir, "1000.output"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	f, path, err := createToolOutputFileWithTimestamp(dir, ts)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_ = f.Close()
	if !strings.HasSuffix(path, "1001.output") {
		t.Fatalf("expected next filename, got %q", path)
	}
}

func TestCreateToolOutputFileWithTimestamp_Collision(t *testing.T) {
	dir := t.TempDir()
	ts := int64(2000)

	for i := 0; i < 16; i++ {
		name := filepath.Join(dir, strconv.FormatInt(ts+int64(i), 10)+".output")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed collision file: %v", err)
		}
	}

	_, _, err := createToolOutputFileWithTimestamp(dir, ts)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("expected collision error, got %v", err)
	}
}

type failingToolOutputFile struct {
	writeErr error
	closeErr error
}

func (f failingToolOutputFile) WriteString(string) (int, error) { return 0, f.writeErr }
func (f failingToolOutputFile) Close() error                    { return f.closeErr }

func TestFinalizeToolOutputFile_RemovesOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.output")
	if err := os.WriteFile(path, []byte("seed"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := finalizeToolOutputFile(failingToolOutputFile{writeErr: errors.New("write")}, path, "out")
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("expected write error, got %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected file removed, stat err=%v", statErr)
	}

	if err := os.WriteFile(path, []byte("seed"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err = finalizeToolOutputFile(failingToolOutputFile{closeErr: errors.New("close")}, path, "out")
	if err == nil || !strings.Contains(err.Error(), "close") {
		t.Fatalf("expected close error, got %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected file removed, stat err=%v", statErr)
	}
}

func TestSanitizePathComponent_Branches(t *testing.T) {
	t.Parallel()

	if got := sanitizePathComponent(" "); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
	if got := sanitizePathComponent("a/b"); got != "a-b" {
		t.Fatalf("expected sanitized component, got %q", got)
	}
	if got := sanitizePathComponent("!!!"); got != "default" {
		t.Fatalf("expected fallback for empty sanitized, got %q", got)
	}
}

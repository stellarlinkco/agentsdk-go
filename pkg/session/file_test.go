package session

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/wal"
)

func TestFileSessionCheckpointResume(t *testing.T) {
	dir := t.TempDir()
	sess := newTestFileSession(t, "chat", dir, wal.WithSegmentBytes(1<<12))
	t.Cleanup(func() { _ = sess.Close() })

	m1 := Message{Role: "user", Content: "hello"}
	m2 := Message{Role: "assistant", Content: "hi"}
	if err := sess.Append(m1); err != nil {
		t.Fatalf("append m1: %v", err)
	}
	if err := sess.Append(m2); err != nil {
		t.Fatalf("append m2: %v", err)
	}
	if err := sess.Checkpoint("cp1"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := sess.Append(Message{Role: "user", Content: "after"}); err != nil {
		t.Fatalf("append after cp: %v", err)
	}

	if err := sess.Resume("cp1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	msgs, err := sess.List(Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d want 2", len(msgs))
	}
	if msgs[0].Content != "hello" || msgs[1].Content != "hi" {
		t.Fatalf("unexpected messages %+v", msgs)
	}
}

func TestFileSessionForkIsolation(t *testing.T) {
	dir := t.TempDir()
	parent := newTestFileSession(t, "root", dir, wal.WithSegmentBytes(1<<12))
	t.Cleanup(func() { _ = parent.Close() })

	if err := parent.Append(Message{Role: "user", Content: "seed"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	child, err := parent.Fork("branch")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })
	if err := child.Append(Message{Role: "assistant", Content: "branch reply"}); err != nil {
		t.Fatalf("child append: %v", err)
	}
	msgParent, _ := parent.List(Filter{})
	msgChild, _ := child.List(Filter{})
	if len(msgParent) != 1 {
		t.Fatalf("parent mutated, len %d", len(msgParent))
	}
	if len(msgChild) != 2 {
		t.Fatalf("child len %d want 2", len(msgChild))
	}
}

func TestFileSessionCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	{
		sess := newTestFileSession(t, "recover", dir, wal.WithSegmentBytes(1<<12))
		if err := sess.Append(Message{Role: "user", Content: "persist"}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if err := sess.Checkpoint("stable"); err != nil {
			t.Fatalf("checkpoint: %v", err)
		}
		if err := sess.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	sess, err := NewFileSession("recover", dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer sess.Close()

	msgs, err := sess.List(Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "persist" {
		t.Fatalf("msgs after recovery %+v", msgs)
	}
	if err := sess.Resume("stable"); err != nil {
		t.Fatalf("resume checkpoint: %v", err)
	}
}

func TestFileSessionConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	sess := newTestFileSession(t, "concurrent", dir, wal.WithSegmentBytes(1<<13), wal.WithDisabledSync())
	t.Cleanup(func() { _ = sess.Close() })

	var wg sync.WaitGroup
	workers := runtime.NumCPU()
	perWorker := 16
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				_ = sess.Append(Message{Role: "user", Content: fmt.Sprintf("%d-%d", id, j)})
			}
		}(i)
	}
	wg.Wait()
	msgs, err := sess.List(Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != workers*perWorker {
		t.Fatalf("messages len %d want %d", len(msgs), workers*perWorker)
	}
}

func TestFileSessionGCRetainsCheckpoints(t *testing.T) {
	dir := t.TempDir()
	sess := newTestFileSession(t, "gc", dir, wal.WithSegmentBytes(512))
	t.Cleanup(func() { _ = sess.Close() })
	for i := 0; i < 20; i++ {
		if err := sess.Append(Message{Role: "user", Content: fmt.Sprintf("m-%d", i)}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if i == 5 || i == 15 {
			if err := sess.Checkpoint(fmt.Sprintf("cp-%d", i)); err != nil {
				t.Fatalf("checkpoint: %v", err)
			}
		}
	}
	// trigger GC via checkpoint
	if err := sess.Checkpoint("tail"); err != nil {
		t.Fatalf("cp tail: %v", err)
	}
	sess.Close()

	segments, err := filepath.Glob(filepath.Join(dir, "gc", "wal", "segment-*.wal"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(segments) == 0 {
		t.Fatalf("no segments on disk")
	}
}

func newTestFileSession(t *testing.T, id, dir string, opts ...wal.Option) *FileSession {
	t.Helper()
	opts = append(opts, wal.WithFileMode(0o600))
	fs, err := NewFileSession(id, dir, opts...)
	if err != nil {
		t.Fatalf("new file session: %v", err)
	}
	fs.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return fs
}

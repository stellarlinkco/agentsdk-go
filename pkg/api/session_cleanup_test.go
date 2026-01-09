package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionEvictionCleansTempDir(t *testing.T) {
	store := newHistoryStore(1)
	var evicted []string
	var cleanupErr error

	sessionID := "session-to-evict"
	dir := bashOutputSessionDir(sessionID)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write dummy output: %v", err)
	}

	store.onEvict = func(id string) {
		evicted = append(evicted, id)
		cleanupErr = cleanupBashOutputSessionDir(id)
	}

	store.Get(sessionID)
	time.Sleep(100 * time.Microsecond)
	store.Get("session-to-keep")

	if cleanupErr != nil {
		t.Fatalf("cleanup evicted session dir: %v", cleanupErr)
	}
	if len(evicted) != 1 || evicted[0] != sessionID {
		t.Fatalf("evicted=%v, want [%q]", evicted, sessionID)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected session dir removed, stat=%v", err)
	}
}

func TestSessionCloseCleansTempDir(t *testing.T) {
	rt := &Runtime{histories: newHistoryStore(0)}

	sessions := []string{"sess-a", "sess-b"}
	for _, sessionID := range sessions {
		rt.histories.Get(sessionID)
		dir := bashOutputSessionDir(sessionID)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })

		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir session dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stdout.txt"), []byte("ok"), 0o600); err != nil {
			t.Fatalf("write dummy output: %v", err)
		}
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, sessionID := range sessions {
		dir := bashOutputSessionDir(sessionID)
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected session dir %q removed, stat=%v", sessionID, err)
		}
	}
}

package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionEvictionDoesNotCleanTempDir(t *testing.T) {
	store := newHistoryStore(1)
	sessionID := "session-to-evict"
	dir := bashOutputSessionDir(sessionID)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write dummy output: %v", err)
	}

	store.Get(sessionID)
	time.Sleep(100 * time.Microsecond)
	store.Get("session-to-keep")

	ids := store.SessionIDs()
	if len(ids) != 1 || ids[0] != "session-to-keep" {
		t.Fatalf("expected store to retain only session-to-keep, got %v", ids)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected session dir to remain after eviction, stat=%v", err)
	}
}

func TestSessionEvictionInvokesCallbackWhenPresent(t *testing.T) {
	store := newHistoryStore(1)
	var evicted []string

	store.onEvict = func(id string) {
		evicted = append(evicted, id)
	}

	store.Get("session-to-evict")
	time.Sleep(100 * time.Microsecond)
	store.Get("session-to-keep")

	if len(evicted) != 1 || evicted[0] != "session-to-evict" {
		t.Fatalf("evicted=%v, want [session-to-evict]", evicted)
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

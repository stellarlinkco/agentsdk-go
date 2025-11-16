package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type bookmarkState struct {
	Stage string `json:"stage"`
	Step  int    `json:"step"`
}

func TestBookmarkStoreCheckpointResumeSerialize(t *testing.T) {
	t.Parallel()
	store := NewBookmarkStore()
	state := bookmarkState{Stage: "draft", Step: 1}
	bm, err := store.Checkpoint("cp-init", 128, state)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	var restored bookmarkState
	pos, err := store.Resume("cp-init", &restored)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if pos != 128 || restored != state {
		t.Fatalf("resume mismatch: pos=%d state=%+v", pos, restored)
	}

	if err := bm.Advance(256); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if err := bm.Snapshot(json.RawMessage(`{"stage":"done","step":2}`)); err != nil {
		t.Fatalf("snapshot raw: %v", err)
	}
	payload, err := bm.Serialize()
	if err != nil {
		t.Fatalf("serialize bookmark: %v", err)
	}
	decoded, err := DeserializeBookmark(payload)
	if err != nil {
		t.Fatalf("deserialize bookmark: %v", err)
	}
	if decoded.Position != 256 {
		t.Fatalf("decoded position mismatch: %d", decoded.Position)
	}

	storePayload, err := store.Serialize()
	if err != nil {
		t.Fatalf("serialize store: %v", err)
	}
	restoredStore, err := DeserializeBookmarkStore(storePayload)
	if err != nil {
		t.Fatalf("deserialize store: %v", err)
	}
	var again bookmarkState
	pos, err = restoredStore.Resume("cp-init", &again)
	if err != nil {
		t.Fatalf("resume restored: %v", err)
	}
	if pos != 128 || again != state {
		t.Fatalf("restored store mismatch: pos=%d state=%+v", pos, again)
	}

	if _, err := store.Resume("missing", nil); !errors.Is(err, ErrBookmarkNotFound) {
		t.Fatalf("expected ErrBookmarkNotFound, got %v", err)
	}
}

func TestBookmarkStoreConcurrentCheckpoint(t *testing.T) {
	const workers = 8
	const iterations = 64
	store := NewBookmarkStore()
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("worker-%d", w)
				state := bookmarkState{Stage: "loop", Step: i}
				if _, err := store.Checkpoint(id, int64(i), state); err != nil {
					errCh <- fmt.Errorf("checkpoint %s: %w", id, err)
					return
				}
				var got bookmarkState
				pos, err := store.Resume(id, &got)
				if err != nil {
					errCh <- fmt.Errorf("resume %s: %w", id, err)
					return
				}
				if pos != int64(i) || got.Step != i || got.Stage != "loop" {
					errCh <- fmt.Errorf("state mismatch worker=%d step=%d got=%+v", w, i, got)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent checkpoint error: %v", err)
	}

	data, err := store.Serialize()
	if err != nil {
		t.Fatalf("serialize after concurrency: %v", err)
	}
	reloaded, err := DeserializeBookmarkStore(data)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	for w := 0; w < workers; w++ {
		var got bookmarkState
		if _, err := reloaded.Resume(fmt.Sprintf("worker-%d", w), &got); err != nil {
			t.Fatalf("resume reloaded worker %d: %v", w, err)
		}
		if got.Stage != "loop" {
			t.Fatalf("unexpected stage for worker %d: %+v", w, got)
		}
	}
}

func TestBookmarkValidationErrors(t *testing.T) {
	t.Parallel()
	if _, err := NewBookmark("", 0, nil); err == nil {
		t.Fatal("expected error for empty id")
	}
	if _, err := NewBookmark("neg", -1, nil); err == nil {
		t.Fatal("expected error for negative position")
	}
	raw := []byte(`{"ok":true}`)
	bm, err := NewBookmark("raw", 1, raw)
	if err != nil {
		t.Fatalf("bookmark raw: %v", err)
	}
	if string(bm.State) != string(raw) {
		t.Fatalf("state mismatch: %s", bm.State)
	}
	if err := bm.Advance(0); err == nil {
		t.Fatal("advance should reject rollback")
	}
	if err := (*Bookmark)(nil).Advance(0); err == nil {
		t.Fatal("advance nil bookmark should fail")
	}
	if _, err := (*Bookmark)(nil).Serialize(); err == nil {
		t.Fatal("serialize nil bookmark should fail")
	}
	if err := (*Bookmark)(nil).Snapshot(nil); err == nil {
		t.Fatal("snapshot nil bookmark should fail")
	}
	if err := (*Bookmark)(nil).Restore(&bookmarkState{}); err != nil {
		t.Fatalf("nil restore should be no-op: %v", err)
	}
	if _, err := DeserializeBookmark([]byte("   ")); err == nil {
		t.Fatal("deserialize empty payload should fail")
	}
	bmNil, err := NewBookmark("nil", 0, nil)
	if err != nil {
		t.Fatalf("bookmark nil: %v", err)
	}
	if _, err := bmNil.Resume(nil); err != nil {
		t.Fatalf("resume nil target: %v", err)
	}
	emptyState := &Bookmark{ID: "empty"}
	if err := emptyState.Restore(&bookmarkState{}); err != nil {
		t.Fatalf("restore empty state: %v", err)
	}
	if _, err := emptyState.Resume(nil); err != nil {
		t.Fatalf("resume empty state: %v", err)
	}
	var store *BookmarkStore
	if _, err := store.Serialize(); err == nil {
		t.Fatal("serialize nil store should fail")
	}
	if _, err := store.Resume("id", nil); err == nil {
		t.Fatal("resume nil store should fail")
	}
	if _, err := store.Checkpoint("id", 0, nil); err == nil {
		t.Fatal("checkpoint nil store should fail")
	}
	var nilBookmark *Bookmark
	if _, err := nilBookmark.Resume(nil); err == nil {
		t.Fatal("expected error for nil bookmark resume")
	}
	customStore := &BookmarkStore{}
	if _, err := customStore.Checkpoint("late", 42, nil); err != nil {
		t.Fatalf("checkpoint with nil map: %v", err)
	}
	emptyStore, err := DeserializeBookmarkStore([]byte("   "))
	if err != nil || emptyStore == nil {
		t.Fatalf("deserialize empty store: %v", err)
	}
}

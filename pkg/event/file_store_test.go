package event

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFileEventStoreAppendAndRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewFileEventStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var events []Event
	for i := 0; i < 3; i++ {
		evt := NewEvent(EventProgress, "sess", map[string]int{"idx": i})
		evt.Bookmark = &Bookmark{Seq: int64(i + 1), Timestamp: time.Now().UTC()}
		if err := store.Append(evt); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		events = append(events, evt)
	}

	got, err := store.ReadSince(nil)
	if err != nil {
		t.Fatalf("read since nil: %v", err)
	}
	if len(got) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(got))
	}

	since, err := store.ReadSince(&Bookmark{Seq: 1})
	if err != nil {
		t.Fatalf("read since 1: %v", err)
	}
	if len(since) != 2 || since[0].Bookmark.Seq != 2 {
		t.Fatalf("unexpected since result: %+v", since)
	}

	rng, err := store.ReadRange(&Bookmark{Seq: 1}, &Bookmark{Seq: 2})
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	if len(rng) != 1 || rng[0].Bookmark.Seq != 2 {
		t.Fatalf("unexpected range: %+v", rng)
	}

	last, err := store.LastBookmark()
	if err != nil {
		t.Fatalf("last bookmark: %v", err)
	}
	if last == nil || last.Seq != 3 {
		t.Fatalf("unexpected last bookmark: %+v", last)
	}

	reopened, err := NewFileEventStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	replay, err := reopened.ReadSince(nil)
	if err != nil {
		t.Fatalf("replay after reopen: %v", err)
	}
	if !reflect.DeepEqual(replay, got) {
		t.Fatalf("replay mismatch after reopen")
	}
}

func TestFileEventStoreValidation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileEventStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Append(NewEvent(EventProgress, "s", nil)); err == nil {
		t.Fatal("append without bookmark should fail")
	}

	store.Close()
	evt := NewEvent(EventProgress, "s", nil)
	evt.Bookmark = &Bookmark{Seq: 1, Timestamp: time.Now()}
	if err := store.Append(evt); err == nil {
		t.Fatal("append on closed store should fail")
	}
}

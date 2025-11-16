package wal

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestWALAppendReplay(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}

	input := []Entry{
		{Type: "msg", Data: []byte("hello")},
		{Type: "msg", Data: []byte("world")},
		{Type: "ckp", Data: []byte(`{"name":"alpha"}`)},
	}
	var positions []Position
	for _, entry := range input {
		pos, err := w.Append(entry)
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		positions = append(positions, pos)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer w.Close()

	var replay []Entry
	if err := w.Replay(func(e Entry) error {
		replay = append(replay, e)
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}

	if len(replay) != len(input) {
		t.Fatalf("replayed %d entries, want %d", len(replay), len(input))
	}
	for i, entry := range replay {
		if string(entry.Data) != string(input[i].Data) {
			t.Fatalf("entry %d data = %q want %q", i, string(entry.Data), string(input[i].Data))
		}
		if entry.Position != positions[i] {
			t.Fatalf("entry %d position = %d want %d", i, entry.Position, positions[i])
		}
	}
}

func TestWALRotatesSegments(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, WithSegmentBytes(256))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer w.Close()

	for i := 0; i < 32; i++ {
		payload := make([]byte, 32)
		payload[0] = byte(i)
		if _, err := w.Append(Entry{Type: "msg", Data: payload}); err != nil {
			t.Fatalf("append #%d: %v", i, err)
		}
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "segment-*.wal"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected rotation, found %d segments", len(files))
	}
}

func TestWALCrashRecoveryTruncatesPartialEntry(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, err := w.Append(Entry{Type: "msg", Data: []byte("persisted")}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if w.current == nil {
		t.Fatalf("current segment nil")
	}
	f, err := os.OpenFile(w.current.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if _, err := f.Write([]byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	f.Close()
	w.Close()

	w, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer w.Close()

	var replay []Entry
	if err := w.Replay(func(e Entry) error {
		replay = append(replay, e)
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(replay) != 1 || string(replay[0].Data) != "persisted" {
		t.Fatalf("replay after crash = %+v", replay)
	}
}

func TestWALTruncateRemovesOldSegments(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, WithSegmentBytes(512))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	var pos []Position
	for i := 0; i < 10; i++ {
		p, err := w.Append(Entry{Type: "msg", Data: []byte{byte(i)}})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		pos = append(pos, p)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	cut := pos[4]
	if err := w.Truncate(cut); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	w.Close()

	w, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer w.Close()

	var replay []Entry
	if err := w.Replay(func(e Entry) error {
		replay = append(replay, e)
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(replay) != len(pos)-4 {
		t.Fatalf("replayed %d entries, want %d", len(replay), len(pos)-4)
	}
	if replay[0].Position != cut {
		t.Fatalf("first position = %d want %d", replay[0].Position, cut)
	}
}

func BenchmarkWALAppend(b *testing.B) {
	if testing.Short() {
		b.Skip("short")
	}
	dir := b.TempDir()
	w, err := Open(dir, WithSegmentBytes(1<<20), WithDisabledSync())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer w.Close()

	payload := make([]byte, 256)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Append(Entry{Type: "msg", Data: payload}); err != nil {
			b.Fatalf("append: %v", err)
		}
	}
}

func TestWALConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, WithDisabledSync())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	workers := runtime.NumCPU()
	perWorker := 32
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				data := []byte{byte(id), byte(j)}
				if _, err := w.Append(Entry{Type: "msg", Data: data}); err != nil {
					t.Errorf("append worker %d: %v", id, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if err := w.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	count := 0
	if err := w.Replay(func(e Entry) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if count != workers*perWorker {
		t.Fatalf("replayed %d entries want %d", count, workers*perWorker)
	}
}

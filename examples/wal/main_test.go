package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/wal"
)

const (
	recordMagic  = 0xA17E57AA
	recordHeader = 11
	crcBytes     = 4
)

func TestWALCreateAndOpen(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, wal.WithSegmentBytes(512))
	if got := len(segmentFiles(t, dir)); got != 1 {
		t.Fatalf("segments=%d want 1", got)
	}
	mustClose(t, w)

	w = openTestWAL(t, dir, wal.WithSegmentBytes(512))
	entry := walEntry("order.create", `{"id":1,"state":"new"}`)
	if pos := mustAppend(t, w, entry); pos != 0 {
		t.Fatalf("first position=%d want 0", pos)
	}
	mustClose(t, w)

	assertEntriesEqual(t, replayEntries(t, dir), []wal.Entry{{Type: entry.Type, Data: entry.Data, Position: 0}})
}

func TestWALWriteAndSync(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, wal.WithSegmentBytes(512))
	entries := []wal.Entry{walEntry("inventory.reserve", `{"sku":"alpha","qty":2}`), walEntry("inventory.reserve", `{"sku":"beta","qty":5}`), walEntry("payment.authorize", `{"order":1,"amount":42}`)}
	appendEntries(t, w, entries)
	mustSync(t, w)
	files := segmentFiles(t, dir)
	idx := 0
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open segment %s: %v", path, err)
		}
		reader := bufio.NewReader(f)
		for {
			var header [recordHeader]byte
			n, err := io.ReadFull(reader, header[:])
			if err != nil {
				if err == io.EOF && n == 0 {
					break
				}
				t.Fatalf("read header %s: %v", path, err)
			}
			if binary.BigEndian.Uint32(header[0:4]) != recordMagic || header[4] != 1 {
				t.Fatalf("corrupt header in %s", path)
			}
			typeLen := int(binary.BigEndian.Uint16(header[5:7]))
			dataLen := int(binary.BigEndian.Uint32(header[7:11]))
			payload := make([]byte, typeLen+dataLen+crcBytes)
			if _, err := io.ReadFull(reader, payload); err != nil {
				t.Fatalf("read payload %s: %v", path, err)
			}
			sum := crc32.NewIEEE()
			sum.Write(header[4:])
			sum.Write(payload[:typeLen+dataLen])
			if binary.BigEndian.Uint32(payload[typeLen+dataLen:]) != sum.Sum32() {
				t.Fatalf("crc mismatch in %s", path)
			}
			if idx >= len(entries) {
				t.Fatalf("unexpected on-disk entry")
			}
			want := entries[idx]
			if want.Type != string(payload[:typeLen]) || string(payload[typeLen:typeLen+dataLen]) != string(want.Data) {
				t.Fatalf("entry #%d mismatch", idx)
			}
			idx++
		}
		f.Close()
	}
	if idx != len(entries) {
		t.Fatalf("decoded %d entries want %d", idx, len(entries))
	}
	mustClose(t, w)
}

func TestWALReplay(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, wal.WithSegmentBytes(512))
	entries := []wal.Entry{walEntry("order.create", `{"id":1}`), walEntry("order.ship", `{"id":1,"carrier":"gopher"}`), walEntry("order.complete", `{"id":1,"state":"done"}`)}
	positions := appendEntries(t, w, entries)
	for i := range entries {
		entries[i].Position = positions[i]
	}
	mustClose(t, w)

	assertEntriesEqual(t, replayEntries(t, dir), entries)
}

func TestWALTruncate(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, wal.WithSegmentBytes(256))
	entries := []wal.Entry{walEntry("evt", "one"), walEntry("evt", "two"), walEntry("evt", "three"), walEntry("evt", "four"), walEntry("evt", "five")}
	positions := appendEntries(t, w, entries)
	cutoff := positions[2]
	if err := w.Truncate(cutoff); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	mustClose(t, w)
	want := append([]wal.Entry(nil), entries[2:]...)
	for i := range want {
		want[i].Position = positions[i+2]
	}
	assertEntriesEqual(t, replayEntries(t, dir), want)
}

func TestWALSegmentManagement(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, wal.WithSegmentBytes(128))
	const total = 6
	var cutoff wal.Position
	for i := 0; i < total; i++ {
		pos := mustAppend(t, w, wal.Entry{
			Type: fmt.Sprintf("event-%02d", i),
			Data: bytes.Repeat([]byte{byte('a' + i)}, 80),
		})
		if i == 3 {
			cutoff = pos
		}
	}
	if got := len(segmentFiles(t, dir)); got != total {
		t.Fatalf("segment count=%d want %d", got, total)
	}

	if err := w.Truncate(cutoff); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if got := len(segmentFiles(t, dir)); got != total-3 {
		t.Fatalf("after truncate segments=%d want %d", got, total-3)
	}
	mustAppend(t, w, wal.Entry{Type: "event-tail", Data: bytes.Repeat([]byte("z"), 80)})
	mustClose(t, w)
}

func openTestWAL(t *testing.T, dir string, opts ...wal.Option) *wal.WAL {
	t.Helper()
	w, err := wal.Open(dir, opts...)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	return w
}

func walEntry(typ, data string) wal.Entry {
	return wal.Entry{Type: typ, Data: []byte(data)}
}

func mustAppend(t *testing.T, w *wal.WAL, entry wal.Entry) wal.Position {
	t.Helper()
	pos, err := w.Append(entry)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	return pos
}

func appendEntries(t *testing.T, w *wal.WAL, entries []wal.Entry) []wal.Position {
	t.Helper()
	pos := make([]wal.Position, len(entries))
	for i, entry := range entries {
		pos[i] = mustAppend(t, w, entry)
	}
	return pos
}

func mustSync(t *testing.T, w *wal.WAL) {
	t.Helper()
	if err := w.Sync(); err != nil {
		t.Fatalf("sync wal: %v", err)
	}
}

func mustClose(t *testing.T, w *wal.WAL) {
	t.Helper()
	if err := w.Close(); err != nil {
		t.Fatalf("close wal: %v", err)
	}
}

func replayEntries(t *testing.T, dir string) []wal.Entry {
	t.Helper()
	w := openTestWAL(t, dir)
	defer mustClose(t, w)

	var entries []wal.Entry
	if err := w.Replay(func(e wal.Entry) error {
		entries = append(entries, e)
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	return entries
}

func segmentFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "segment-*.wal"))
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	sort.Strings(files)
	return files
}

func assertEntriesEqual(t *testing.T, got, want []wal.Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("entries len=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Position != want[i].Position || got[i].Type != want[i].Type || !bytes.Equal(got[i].Data, want[i].Data) {
			t.Fatalf("entry %d mismatch got=%+v want=%+v", i, got[i], want[i])
		}
	}
}

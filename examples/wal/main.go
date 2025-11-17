package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/cexll/agentsdk-go/pkg/wal"
)

func main() {
	log.SetFlags(0)
	tempDir, err := os.MkdirTemp("", "agentsdk-wal-*")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer func() {
		log.Printf("cleaning up %s", tempDir)
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			log.Printf("remove temp dir: %v", rmErr)
		}
	}()
	log.Printf("WAL directory: %s", tempDir)
	store, err := wal.Open(tempDir, wal.WithSegmentBytes(512))
	if err != nil {
		log.Fatalf("open wal: %v", err)
	}
	log.Printf("opened WAL with force rotation at 512 bytes per segment")

	entries := []wal.Entry{
		{Type: "order.create", Data: []byte(`{"id":1,"item":"alpha"}`)},
		{Type: "inventory.reserve", Data: []byte(`{"id":1,"qty":2}`)},
		{Type: "payment.authorize", Data: []byte(`{"id":1,"amount":42}`)},
		{Type: "order.ship", Data: []byte(`{"id":1,"carrier":"gopher"}`)},
		{Type: "order.complete", Data: []byte(`{"id":1,"state":"done"}`)},
	}
	positions := make([]wal.Position, 0, len(entries))
	for i, entry := range entries {
		// Append persists the record to the active segment buffer.
		pos, err := store.Append(entry)
		if err != nil {
			log.Fatalf("append #%d: %v", i, err)
		}
		positions = append(positions, pos)
		log.Printf("appended entry #%d type=%s @position=%d", i, entry.Type, pos)
		if err := store.Sync(); err != nil {
			log.Fatalf("sync after entry #%d: %v", i, err)
		}
		log.Printf("fsync complete for entry #%d", i)
	}
	printSegments(tempDir)
	if err := store.Close(); err != nil {
		log.Fatalf("close wal: %v", err)
	}
	log.Printf("WAL closed -- simulating process shutdown")
	store, err = wal.Open(tempDir)
	if err != nil {
		log.Fatalf("reopen wal: %v", err)
	}
	log.Printf("reopened WAL, starting recovery")
	recovered := make([]wal.Entry, 0, len(entries))
	err = store.Replay(func(e wal.Entry) error {
		// Replay walks each segment sequentially, decoding entries lazily.
		log.Printf("replay: position=%d type=%s payload=%s", e.Position, e.Type, e.Data)
		recovered = append(recovered, e)
		return nil
	})
	if err != nil {
		log.Fatalf("replay: %v", err)
	}
	for i := range entries {
		if recovered[i].Position != positions[i] || recovered[i].Type != entries[i].Type || !bytes.Equal(recovered[i].Data, entries[i].Data) {
			log.Fatalf("recovery mismatch on index %d", i)
		}
	}
	log.Printf("all %d entries verified after recovery", len(recovered))
	cutoff := positions[2]
	log.Printf("truncating WAL before position %d", cutoff)
	if err := store.Truncate(cutoff); err != nil {
		log.Fatalf("truncate: %v", err)
	}
	if err := store.Sync(); err != nil {
		log.Fatalf("sync after truncate: %v", err)
	}
	log.Printf("truncate persisted; entries before %d discarded", cutoff)
	trimmed := make([]wal.Entry, 0, len(entries))
	if err := store.Replay(func(e wal.Entry) error {
		trimmed = append(trimmed, e)
		return nil
	}); err != nil {
		log.Fatalf("replay after truncate: %v", err)
	}
	log.Printf("replayed %d entries after truncate; first position now %d", len(trimmed), trimmed[0].Position)
	printSegments(tempDir)
	if err := store.Close(); err != nil {
		log.Fatalf("close after truncate: %v", err)
	}
	log.Printf("demo complete")
}

func printSegments(dir string) {
	files, err := filepath.Glob(filepath.Join(dir, "segment-*.wal"))
	if err != nil {
		log.Printf("list segments: %v", err)
		return
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			log.Printf("segment info %s: %v", file, err)
			continue
		}
		fmt.Printf("segment %s size=%d bytes\n", filepath.Base(file), info.Size())
	}
}

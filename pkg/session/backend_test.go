package session

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileBackendReadWriteListDelete(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("new file backend: %v", err)
	}
	path := "/sessions/chat/data.json"
	payload := []byte("hello wal")

	if err := b.Write(path, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	read, err := b.Read(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reflect.DeepEqual(read, payload) {
		t.Fatalf("read payload = %q want %q", string(read), string(payload))
	}

	list, err := b.List("/sessions")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{normalizePath(path)}
	if !reflect.DeepEqual(list, want) {
		t.Fatalf("list = %v want %v", list, want)
	}

	if err := b.Delete(path); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := b.Read(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestFileBackendPreventsEscape(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("new file backend: %v", err)
	}
	if err := b.Write("../evil", []byte("x")); err == nil {
		t.Fatalf("expected error on path escape")
	}
}

func TestCompositeBackendRoutesToFileBackend(t *testing.T) {
	dir := t.TempDir()
	fb, err := NewFileBackend(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("new file backend: %v", err)
	}
	cb := NewCompositeBackend()
	if err := cb.AddRoute("/sessions", fb); err != nil {
		t.Fatalf("add route: %v", err)
	}
	data := []byte("payload")
	if err := cb.Write("/sessions/demo/log.json", data); err != nil {
		t.Fatalf("composite write: %v", err)
	}
	out, err := cb.Read("/sessions/demo/log.json")
	if err != nil {
		t.Fatalf("composite read: %v", err)
	}
	if !reflect.DeepEqual(out, data) {
		t.Fatalf("read = %q want %q", string(out), string(data))
	}
}

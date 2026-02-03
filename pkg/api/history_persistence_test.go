package api

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/model"
)

func TestRuntime_PersistsAndLoadsHistory(t *testing.T) {
	dir := t.TempDir()
	sessionID := "sess-1"

	rt1, err := New(context.Background(), Options{
		ProjectRoot: dir,
		Model: &stubModel{responses: []*model.Response{
			{Message: model.Message{Role: "assistant", Content: "ok"}},
		}},
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt1.Close() })

	if _, err := rt1.Run(context.Background(), Request{Prompt: "hello", SessionID: sessionID}); err != nil {
		t.Fatalf("run 1: %v", err)
	}

	p := newDiskHistoryPersister(dir)
	if p == nil {
		t.Fatal("expected disk history persister")
	}
	msgs, err := p.Load(sessionID)
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "ok" {
		t.Fatalf("unexpected second message: %+v", msgs[1])
	}

	mdl2 := &stubModel{responses: []*model.Response{
		{Message: model.Message{Role: "assistant", Content: "ok2"}},
	}}
	rt2, err := New(context.Background(), Options{ProjectRoot: dir, Model: mdl2})
	if err != nil {
		t.Fatalf("new runtime 2: %v", err)
	}
	t.Cleanup(func() { _ = rt2.Close() })

	if _, err := rt2.Run(context.Background(), Request{Prompt: "again", SessionID: sessionID}); err != nil {
		t.Fatalf("run 2: %v", err)
	}

	if len(mdl2.requests) == 0 {
		t.Fatal("expected model request in second run")
	}
	gotReq := mdl2.requests[0]
	if len(gotReq.Messages) != 3 {
		t.Fatalf("expected 3 messages in request, got %d", len(gotReq.Messages))
	}
	if gotReq.Messages[0].Role != "user" || gotReq.Messages[0].Content != "hello" {
		t.Fatalf("unexpected request[0]: %+v", gotReq.Messages[0])
	}
	if gotReq.Messages[1].Role != "assistant" || gotReq.Messages[1].Content != "ok" {
		t.Fatalf("unexpected request[1]: %+v", gotReq.Messages[1])
	}
	if gotReq.Messages[2].Role != "user" || gotReq.Messages[2].Content != "again" {
		t.Fatalf("unexpected request[2]: %+v", gotReq.Messages[2])
	}

	msgs2, err := p.Load(sessionID)
	if err != nil {
		t.Fatalf("load persisted history after run 2: %v", err)
	}
	if len(msgs2) != 4 {
		t.Fatalf("expected 4 messages after second run, got %d", len(msgs2))
	}
}

func TestDiskHistoryPersister_CleanupRemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	p := newDiskHistoryPersister(dir)
	if p == nil {
		t.Fatal("expected disk history persister")
	}
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		t.Fatalf("mkdir history dir: %v", err)
	}
	path := p.filePath("old-session")
	if path == "" {
		t.Fatal("expected history file path")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"messages":[{"Role":"user","Content":"x"}]}`), 0o600); err != nil {
		t.Fatalf("write history: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := p.Cleanup(1); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected old history file removed, stat err=%v", err)
	}
}

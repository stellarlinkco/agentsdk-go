package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/message"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type historyCaptureModel struct {
	content  string
	called   bool
	requests []model.Request
}

func (m *historyCaptureModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return nil, errors.New("Complete should not be called directly")
}

func (m *historyCaptureModel) CompleteStream(_ context.Context, req model.Request, cb model.StreamHandler) error {
	m.called = true
	m.requests = append(m.requests, req)
	return cb(model.StreamResult{
		Final: true,
		Response: &model.Response{
			Message: model.Message{Role: "assistant", Content: m.content},
		},
	})
}

func TestHistoryLoaderRestoresSessionHistory(t *testing.T) {
	mdl := &historyCaptureModel{content: "ok"}
	rt, err := New(context.Background(), Options{
		ProjectRoot:         t.TempDir(),
		Model:               mdl,
		EnabledBuiltinTools: []string{},
		RulesEnabled:        boolPtr(false),
		HistoryLoader: func(sessionID string) ([]message.Message, error) {
			if sessionID != "sess" {
				t.Fatalf("sessionID = %q, want sess", sessionID)
			}
			return []message.Message{{Role: "user", Content: "previous"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if _, err := rt.Run(context.Background(), Request{SessionID: "sess", Prompt: "next"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(mdl.requests) != 1 {
		t.Fatalf("model requests = %d, want 1", len(mdl.requests))
	}
	msgs := mdl.requests[0].Messages
	if len(msgs) != 2 || msgs[0].Content != "previous" || msgs[1].Content != "next" {
		t.Fatalf("model messages = %+v", msgs)
	}

	snapshot, ok := rt.SessionHistory("sess")
	if !ok {
		t.Fatal("expected session snapshot")
	}
	if len(snapshot) != 3 || snapshot[0].Content != "previous" || snapshot[1].Content != "next" || snapshot[2].Content != "ok" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestHistoryLoaderErrorStopsRun(t *testing.T) {
	boom := errors.New("boom")
	mdl := &historyCaptureModel{content: "ok"}
	rt, err := New(context.Background(), Options{
		ProjectRoot:         t.TempDir(),
		Model:               mdl,
		EnabledBuiltinTools: []string{},
		RulesEnabled:        boolPtr(false),
		HistoryLoader: func(string) ([]message.Message, error) {
			return nil, boom
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	_, err = rt.Run(context.Background(), Request{SessionID: "sess", Prompt: "next"})
	if !errors.Is(err, boom) {
		t.Fatalf("Run error = %v, want boom", err)
	}
	if mdl.called {
		t.Fatal("model was called after loader failure")
	}
	if _, ok := rt.SessionHistory("sess"); ok {
		t.Fatal("loader failure should not create session history")
	}
}

func TestSessionHistoryDoesNotCreateMissingSession(t *testing.T) {
	rt := newTestRuntime(t, staticModel{content: "ok"}, CompactConfig{})

	if _, err := rt.Run(context.Background(), Request{SessionID: "keep", Prompt: "hello"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := rt.SessionHistory("missing"); ok {
		t.Fatal("missing session unexpectedly returned history")
	}

	ids := rt.histories.SessionIDs()
	if len(ids) != 1 || ids[0] != "keep" {
		t.Fatalf("session IDs = %v, want [keep]", ids)
	}
}

func TestSessionHistoryReturnsSnapshotClone(t *testing.T) {
	rt := newTestRuntime(t, staticModel{content: "ok"}, CompactConfig{})
	hist, err := rt.histories.Get("sess")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	hist.Append(message.Message{
		Role:    "user",
		Content: "original",
		Metadata: map[string]any{
			"key": "value",
		},
		ToolCalls: []message.ToolCall{{
			ID:        "call-1",
			Name:      "tool",
			Arguments: map[string]any{"arg": "value"},
		}},
	})

	snapshot, ok := rt.SessionHistory("sess")
	if !ok || len(snapshot) != 1 {
		t.Fatalf("snapshot = %+v, ok=%v", snapshot, ok)
	}
	snapshot[0].Content = "mutated"
	snapshot[0].Metadata["key"] = "mutated"
	snapshot[0].ToolCalls[0].Arguments["arg"] = "mutated"

	again, ok := rt.SessionHistory("sess")
	if !ok || len(again) != 1 {
		t.Fatalf("second snapshot = %+v, ok=%v", again, ok)
	}
	if again[0].Content != "original" || again[0].Metadata["key"] != "value" || again[0].ToolCalls[0].Arguments["arg"] != "value" {
		t.Fatalf("history was mutated through snapshot: %+v", again[0])
	}
	if strings.Contains(again[0].Content, "mutated") {
		t.Fatalf("unexpected mutated content: %+v", again[0])
	}
}

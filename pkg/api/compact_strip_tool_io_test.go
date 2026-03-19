package api

import (
	"context"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/message"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type recordingModel struct {
	requests []model.Request
}

func (m *recordingModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.requests = append(m.requests, req)
	return &model.Response{Message: model.Message{Role: "assistant", Content: "ok"}, StopReason: "stop"}, nil
}

func (m *recordingModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	if cb == nil {
		return nil
	}
	return cb(model.StreamResult{Final: true, Response: resp})
}

func TestCompactor_StripsToolIOFromCompressionInput(t *testing.T) {
	hist := message.NewHistory()
	hist.Append(message.Message{Role: "user", Content: strings.Repeat("u", 1200)})
	hist.Append(message.Message{
		Role: "assistant",
		ToolCalls: []message.ToolCall{{
			ID:        "t1",
			Name:      "bash",
			Arguments: map[string]any{"command": "echo tool"},
		}},
	})
	hist.Append(message.Message{
		Role: "tool",
		ToolCalls: []message.ToolCall{{
			ID:     "t1",
			Name:   "bash",
			Result: strings.Repeat("x", 1200),
		}},
	})
	hist.Append(message.Message{Role: "assistant", Content: "done"})
	hist.Append(message.Message{Role: "user", Content: strings.Repeat("y", 1200)})
	hist.Append(message.Message{Role: "assistant", Content: "final"})

	c := newCompactor(CompactConfig{Enabled: true, Threshold: 0.01, PreserveCount: 2}, 1)
	mdl := &recordingModel{}
	did, err := c.maybeCompact(context.Background(), hist, mdl)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !did {
		t.Fatal("expected compaction to run")
	}
	if len(mdl.requests) != 1 {
		t.Fatalf("expected 1 compression request, got %d", len(mdl.requests))
	}

	req := mdl.requests[0]
	if !strings.Contains(strings.ToLower(req.System), "prompt compression engine") {
		t.Fatalf("unexpected compression system prompt: %q", req.System)
	}
	for _, msg := range req.Messages {
		if strings.EqualFold(msg.Role, "tool") {
			t.Fatalf("unexpected tool-role message in compression input: %+v", msg)
		}
		if len(msg.ToolCalls) > 0 {
			t.Fatalf("unexpected tool calls/results in compression input: %+v", msg)
		}
		if strings.Contains(msg.Content, strings.Repeat("x", 50)) {
			t.Fatalf("unexpected tool output content in compression input")
		}
	}

	snapshot := hist.All()
	if len(snapshot) == 0 || snapshot[0].Role != "system" || !strings.Contains(snapshot[0].Content, "## Summary") {
		t.Fatalf("expected system summary message after compaction, got %+v", snapshot)
	}
}

func TestCompactor_NilModelErrors(t *testing.T) {
	hist := message.NewHistory()
	hist.Append(message.Message{Role: "user", Content: strings.Repeat("u", 1200)})
	hist.Append(message.Message{Role: "assistant", Content: strings.Repeat("a", 1200)})

	c := newCompactor(CompactConfig{Enabled: true, Threshold: 0.01, PreserveCount: 1}, 1)
	_, err := c.maybeCompact(context.Background(), hist, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "model is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

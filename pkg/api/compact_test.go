package api

import (
	"context"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/message"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type compactStubModel struct {
	resp string
	err  error
	last model.Request
}

func (s *compactStubModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	s.last = req
	if s.err != nil {
		return nil, s.err
	}
	return &model.Response{Message: model.Message{Content: s.resp}}, nil
}

func (s *compactStubModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	resp, err := s.Complete(ctx, req)
	if err != nil {
		return err
	}
	return cb(model.StreamResult{Final: true, Response: resp})
}

func TestCompactConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := CompactConfig{Enabled: true, PreserveCount: 0, Threshold: 2}
	got := cfg.withDefaults()
	if got.PreserveCount < 1 {
		t.Fatalf("expected preserve count default")
	}
	if got.Threshold != defaultCompactThreshold {
		t.Fatalf("expected default threshold")
	}
}

func TestCompactorMaybeCompact(t *testing.T) {
	t.Parallel()

	hist := message.NewHistory()
	hist.Append(message.Message{Role: "user", Content: "one"})
	hist.Append(message.Message{Role: "assistant", ToolCalls: []message.ToolCall{{ID: "t1", Name: "bash", Arguments: map[string]any{"cmd": "echo TOP_SECRET"}}}})
	hist.Append(message.Message{Role: "tool", ToolCalls: []message.ToolCall{{ID: "t1", Name: "bash", Result: "TOP_SECRET_OUTPUT"}}})
	hist.Append(message.Message{Role: "user", Content: "three"})

	comp := newCompactor(CompactConfig{Enabled: true, PreserveCount: 1, Threshold: 0.1}, 1)
	if comp == nil {
		t.Fatalf("expected compactor")
	}
	stub := &compactStubModel{resp: "summary"}
	ok, err := comp.maybeCompact(context.Background(), hist, stub)
	if err != nil || !ok {
		t.Fatalf("unexpected compact result ok=%v err=%v", ok, err)
	}
	msgs := hist.All()
	if len(msgs) != 2 {
		t.Fatalf("expected compacted history len=2, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "summary") {
		t.Fatalf("expected summary system message, got %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "three" {
		t.Fatalf("expected preserved tail, got %+v", msgs[1])
	}

	combined := ""
	for _, m := range stub.last.Messages {
		combined += m.Role + ":" + m.Content + "\n"
	}
	if strings.Contains(combined, "TOP_SECRET") || strings.Contains(combined, "TOP_SECRET_OUTPUT") {
		t.Fatalf("expected tool I/O stripped from compression input, got %q", combined)
	}
}

func TestCompactorPreservesToolTransactionSpans(t *testing.T) {
	t.Parallel()

	hist := message.NewHistory()
	hist.Append(message.Message{Role: "user", Content: "u0"})
	hist.Append(message.Message{Role: "assistant", ToolCalls: []message.ToolCall{{ID: "t1", Name: "echo", Arguments: map[string]any{"x": 1}}}})
	hist.Append(message.Message{Role: "tool", ToolCalls: []message.ToolCall{{ID: "t1", Name: "echo", Result: "ok"}}})
	hist.Append(message.Message{Role: "user", Content: "u1"})

	comp := newCompactor(CompactConfig{Enabled: true, PreserveCount: 2, Threshold: 0.1}, 1)
	ok, err := comp.maybeCompact(context.Background(), hist, &compactStubModel{resp: "summary"})
	if err != nil || !ok {
		t.Fatalf("maybeCompact ok=%v err=%v", ok, err)
	}
	msgs := hist.All()
	if len(msgs) < 3 {
		t.Fatalf("msgs=%+v", msgs)
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) == 0 {
		t.Fatalf("expected tool transaction preserved at start, got %+v", msgs[1])
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestClientCallDecodesResult(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"status": "ok"})
	st := &stubTransport{responses: []*Response{{ID: "1", Result: raw}}}
	client := NewClient(st)

	var out map[string]string
	if err := client.Call(context.Background(), "ping", nil, &out); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if out["status"] != "ok" {
		t.Fatalf("unexpected payload: %+v", out)
	}
}

func TestClientCallPropagatesError(t *testing.T) {
	st := &stubTransport{responses: []*Response{{ID: "1", Error: &Error{Code: 1, Message: "bad"}}}}
	client := NewClient(st)
	if err := client.Call(context.Background(), "ping", nil, nil); err == nil || err.Error() != "mcp error 1: bad" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientListTools(t *testing.T) {
	payload, _ := json.Marshal(ToolListResult{Tools: []ToolDescriptor{{Name: "echo", Description: "Echo"}}})
	st := &stubTransport{responses: []*Response{{ID: "1", Result: payload}}}
	client := NewClient(st)
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected list: %+v", tools)
	}
}

func TestClientInvokeTool(t *testing.T) {
	payload, _ := json.Marshal(ToolCallResult{Content: json.RawMessage(`"done"`)})
	st := &stubTransport{responses: []*Response{{ID: "1", Result: payload}}}
	client := NewClient(st)
	res, err := client.InvokeTool(context.Background(), "echo", map[string]interface{}{"text": "hi"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if string(res.Content) != "\"done\"" {
		t.Fatalf("unexpected content: %s", string(res.Content))
	}
}

func TestClientClose(t *testing.T) {
	st := &stubTransport{}
	client := NewClient(st)
	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !st.closed {
		t.Fatal("transport not closed")
	}
}

type stubTransport struct {
	mu        sync.Mutex
	responses []*Response
	calls     []*Request
	closed    bool
	err       error
}

func (s *stubTransport) Call(ctx context.Context, req *Request) (*Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, s.err
	}
	if len(s.responses) == 0 {
		return nil, errors.New("no response queued")
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp, nil
}

func (s *stubTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

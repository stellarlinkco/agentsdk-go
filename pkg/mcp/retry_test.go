package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

type flakyTransport struct {
	failures int
	calls    int
	err      error
	delay    time.Duration
}

func (f *flakyTransport) Call(ctx context.Context, req *Request) (*Response, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.calls <= f.failures {
		if f.err != nil {
			return nil, f.err
		}
		return nil, &net.DNSError{IsTimeout: true, Err: "temporary"}
	}
	return &Response{ID: req.ID, Result: json.RawMessage(`"ok"`)}, nil
}

func (f *flakyTransport) Close() error { return nil }

func TestRetryTransportRetries(t *testing.T) {
	inner := &flakyTransport{failures: 2}
	rt := NewRetryTransport(inner, RetryPolicy{MaxAttempts: 3})
	req := &Request{ID: "1", Method: "ping"}

	resp, err := rt.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if string(resp.Result) != "\"ok\"" {
		t.Fatalf("unexpected result: %s", string(resp.Result))
	}
	if inner.calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", inner.calls)
	}
}

func TestRetryTransportStopsOnContext(t *testing.T) {
	inner := &flakyTransport{failures: 5}
	rt := NewRetryTransport(inner, RetryPolicy{
		MaxAttempts: 4,
		Backoff:     func(int) time.Duration { return 0 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.Call(ctx, &Request{ID: "1"})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancel, got %v", err)
	}
	if inner.calls != 0 {
		t.Fatalf("expected zero attempts when context canceled early, got %d", inner.calls)
	}
}

func TestRetryTransportClosePassthrough(t *testing.T) {
	inner := &retryStubTransport{}
	rt := NewRetryTransport(inner, RetryPolicy{})
	if err := rt.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestRetryTransportCloseWithoutInner(t *testing.T) {
	rt := &RetryTransport{}
	if err := rt.Close(); err != nil {
		t.Fatalf("close nil: %v", err)
	}
}

func TestRetryTransportNonRetryable(t *testing.T) {
	fatal := errors.New("fatal")
	inner := &flakyTransport{failures: 1, err: fatal}
	rt := NewRetryTransport(inner, RetryPolicy{MaxAttempts: 3})
	if _, err := rt.Call(context.Background(), &Request{ID: "1"}); !errors.Is(err, fatal) {
		t.Fatalf("expected fatal error, got %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected single attempt, got %d", inner.calls)
	}
}

func TestRetryTransportContextCancelMidFlight(t *testing.T) {
	inner := &flakyTransport{failures: 5, delay: 5 * time.Millisecond}
	rt := NewRetryTransport(inner, RetryPolicy{
		MaxAttempts: 5,
		Backoff:     func(int) time.Duration { return 0 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(8 * time.Millisecond)
		cancel()
	}()
	if _, err := rt.Call(ctx, &Request{ID: "1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancel, got %v", err)
	}
	if inner.calls == 0 {
		t.Fatal("expected at least one attempt before cancel")
	}
}

type fakeNetErr struct {
	timeout   bool
	temporary bool
}

func (f fakeNetErr) Error() string   { return "fake" }
func (f fakeNetErr) Timeout() bool   { return f.timeout }
func (f fakeNetErr) Temporary() bool { return f.temporary }

func TestDefaultRetryable(t *testing.T) {
	if defaultRetryable(nil) {
		t.Fatal("nil should not be retryable")
	}
	if defaultRetryable(context.Canceled) {
		t.Fatal("canceled should not retry")
	}
	if !defaultRetryable(context.DeadlineExceeded) {
		t.Fatal("deadline should retry")
	}
	if !defaultRetryable(fakeNetErr{timeout: true}) {
		t.Fatal("timeout net error should retry")
	}
	if defaultRetryable(errors.New("other")) {
		t.Fatal("generic error should not retry")
	}
}

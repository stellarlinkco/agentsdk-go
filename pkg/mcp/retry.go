package mcp

import (
	"context"
	"errors"
	"net"
	"time"
)

// RetryPolicy controls retry behaviour for a transport wrapper.
type RetryPolicy struct {
	MaxAttempts int
	Backoff     func(attempt int) time.Duration
	Retryable   func(error) bool
	Sleep       func(time.Duration)
}

// RetryTransport wraps a Transport with retry semantics suitable for transient network hiccups.
type RetryTransport struct {
	inner  Transport
	policy RetryPolicy
}

// NewRetryTransport builds a retrying transport around inner.
func NewRetryTransport(inner Transport, policy RetryPolicy) *RetryTransport {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 3
	}
	if policy.Backoff == nil {
		policy.Backoff = func(attempt int) time.Duration {
			if attempt <= 1 {
				return 0
			}
			return time.Duration(1<<(attempt-2)) * 50 * time.Millisecond
		}
	}
	if policy.Retryable == nil {
		policy.Retryable = defaultRetryable
	}
	if policy.Sleep == nil {
		policy.Sleep = time.Sleep
	}
	return &RetryTransport{inner: inner, policy: policy}
}

// Call forwards requests to the inner transport with retry logic.
func (t *RetryTransport) Call(ctx context.Context, req *Request) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 1; attempt <= t.policy.MaxAttempts; attempt++ {
		resp, err := t.inner.Call(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !t.policy.Retryable(err) || attempt == t.policy.MaxAttempts {
			break
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			lastErr = ctxErr
			break
		}
		t.policy.Sleep(t.policy.Backoff(attempt + 1))
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if lastErr == nil {
		return nil, ErrTransportClosed
	}
	return nil, lastErr
}

// Close delegates to the inner transport when supported.
func (t *RetryTransport) Close() error {
	if closer, ok := t.inner.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func defaultRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return nerr.Timeout() || nerr.Temporary()
	}
	return false
}

package mcp

import (
	"context"
	"sync"
)

type retryStubTransport struct {
	responses []*Response
	errs      []error
	mu        sync.Mutex
}

func (r *retryStubTransport) Call(ctx context.Context, req *Request) (*Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		return nil, err
	}
	if len(r.responses) == 0 {
		return nil, ErrTransportClosed
	}
	resp := r.responses[0]
	r.responses = r.responses[1:]
	return resp, nil
}

func (r *retryStubTransport) Close() error { return nil }

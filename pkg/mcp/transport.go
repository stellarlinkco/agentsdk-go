package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrTransportClosed indicates the transport can no longer accept calls.
	ErrTransportClosed = errors.New("mcp transport closed")
)

// Transport hides the underlying IO mechanism (stdio/SSE).
type Transport interface {
	Call(ctx context.Context, req *Request) (*Response, error)
	Close() error
}

type callResult struct {
	resp *Response
	err  error
}

type pendingTracker struct {
	mu      sync.Mutex
	pending map[string]chan callResult
	closed  bool
	err     error
}

func newPendingTracker() *pendingTracker {
	return &pendingTracker{pending: make(map[string]chan callResult)}
}

func (p *pendingTracker) add(id string) (chan callResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		if p.err == nil {
			p.err = ErrTransportClosed
		}
		return nil, p.err
	}
	if _, exists := p.pending[id]; exists {
		return nil, fmt.Errorf("duplicate request id %s", id)
	}
	ch := make(chan callResult, 1)
	p.pending[id] = ch
	return ch, nil
}

func (p *pendingTracker) deliver(id string, res callResult) {
	p.mu.Lock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.mu.Unlock()
	if ok {
		ch <- res
		close(ch)
	}
}

func (p *pendingTracker) cancel(id string) {
	p.mu.Lock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.mu.Unlock()
	if ok {
		close(ch)
	}
}

func (p *pendingTracker) flush(err error) {
	p.mu.Lock()
	for id, ch := range p.pending {
		delete(p.pending, id)
		ch <- callResult{err: err}
		close(ch)
	}
	p.mu.Unlock()
}

func (p *pendingTracker) failAll(err error) {
	p.flush(err)
	p.mu.Lock()
	p.closed = true
	p.err = err
	p.mu.Unlock()
}

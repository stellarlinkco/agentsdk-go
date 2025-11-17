package main

import (
	"fmt"
	"sync"

	"github.com/cexll/agentsdk-go/pkg/workflow"
)

// PurchaseRequest is the payload that moves across the StateGraph via ExecutionContext.
type PurchaseRequest struct {
	ID       string
	Amount   float64
	Priority string
	Owner    string
}

// trace keeps execution metadata while remaining goroutine-safe for the parallel node.
type trace struct {
	sync.Mutex
	path       []string
	notes      []string
	resolution string
	completed  bool
}

func (t *trace) step(name string)   { t.Lock(); t.path = append(t.path, name); t.Unlock() }
func (t *trace) note(msg string)    { t.Lock(); t.notes = append(t.notes, msg); t.Unlock() }
func (t *trace) resolve(msg string) { t.Lock(); t.resolution = msg; t.Unlock() }
func (t *trace) completeOnce() bool {
	t.Lock()
	defer t.Unlock()
	if t.completed {
		return false
	}
	t.completed = true
	return true
}
func (t *trace) snapshot() ([]string, string, []string) {
	t.Lock()
	defer t.Unlock()
	return append([]string(nil), t.path...), t.resolution, append([]string(nil), t.notes...)
}

// fanInBarrier blocks callers until the configured number of arrivals has been reached.
type fanInBarrier struct {
	mu     sync.Mutex
	cond   *sync.Cond
	target int
	count  int
}

func newFanInBarrier(target int) *fanInBarrier {
	if target < 1 {
		target = 1
	}
	b := &fanInBarrier{target: target}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *fanInBarrier) arriveAndWait() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	if b.count < b.target {
		for b.count < b.target {
			b.cond.Wait()
		}
		return
	}
	b.cond.Broadcast()
}

func requestFromContext(ctx *workflow.ExecutionContext) (*PurchaseRequest, error) {
	if raw, ok := ctx.Get(ctxKeyRequest); !ok {
		return nil, fmt.Errorf("execution context missing %q", ctxKeyRequest)
	} else if req, ok := raw.(*PurchaseRequest); !ok || req == nil {
		return nil, fmt.Errorf("unexpected request payload %T", raw)
	} else {
		return req, nil
	}
}

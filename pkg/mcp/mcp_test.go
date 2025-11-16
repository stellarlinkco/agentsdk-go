package mcp

import (
	"context"
	"testing"
	"time"
)

func TestErrorString(t *testing.T) {
	err := &Error{Code: 42, Message: "boom"}
	if got := err.Error(); got != "mcp error 42: boom" {
		t.Fatalf("unexpected string: %s", got)
	}
	err.Data = []byte(`{"hint":"retry"}`)
	if got := err.Error(); got != "mcp error 42: boom ({\"hint\":\"retry\"})" {
		t.Fatalf("unexpected string with data: %s", got)
	}
}

func TestPendingTrackerLifecycle(t *testing.T) {
	p := newPendingTracker()
	ch, err := p.add("1")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() {
		<-ctx.Done()
		p.cancel("1")
	}()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("pending request not canceled")
	}
	if _, err := p.add("1"); err != nil {
		t.Fatalf("tracker should accept reused ids: %v", err)
	}
	p.flush(context.DeadlineExceeded)
	if _, err := p.add("2"); err != nil {
		t.Fatalf("tracker should stay open after flush: %v", err)
	}
	p.failAll(context.Canceled)
	if _, err := p.add("3"); err == nil {
		t.Fatal("expected closed tracker error")
	}
}

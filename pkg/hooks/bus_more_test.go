package hooks

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBus_Close_IsIdempotent(t *testing.T) {
	bus := NewBus()
	bus.Close()
	// Cover b.closed.Swap(true) early return.
	bus.Close()
}

func TestBus_Publish_ReturnsValidateError(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	if err := bus.Publish(Event{}); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestBus_Publish_ReturnsClosedWhenBaseContextDone(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	// Cancel base context without flipping closed flag to cover select <-Done path.
	// Force the send case to be disabled so select deterministically observes Done.
	bus.queue = nil
	bus.cancel()

	if err := bus.Publish(Event{Type: Stop}); err == nil {
		t.Fatalf("expected closed error")
	}
}

func TestBus_Subscribe_NoopsOnNilOrClosedBus(t *testing.T) {
	var nilBus *Bus
	unsub := nilBus.Subscribe(Stop, func(context.Context, Event) {})
	unsub()

	bus := NewBus()
	bus.Close()
	unsub = bus.Subscribe(Stop, func(context.Context, Event) {})
	unsub()
}

func TestBus_Unsubscribe_IsSafeToCallMultipleTimes(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var called atomic.Int32
	unsub := bus.Subscribe(Stop, func(context.Context, Event) {
		called.Add(1)
	})
	unsub()
	unsub() // should not panic

	_ = bus.Publish(Event{Type: Stop})
	time.Sleep(25 * time.Millisecond)
	if called.Load() != 0 {
		t.Fatalf("unexpected handler invocation after unsubscribe: %d", called.Load())
	}
}

func TestBus_SwallowsHandlerPanic(t *testing.T) {
	bus := NewBus(WithQueueDepth(4))
	defer bus.Close()

	var okCount atomic.Int32
	bus.Subscribe(Stop, func(context.Context, Event) {
		panic("boom")
	})
	bus.Subscribe(Stop, func(context.Context, Event) {
		okCount.Add(1)
	})

	if err := bus.Publish(Event{Type: Stop}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := bus.Publish(Event{Type: Stop}); err != nil {
		t.Fatalf("publish2: %v", err)
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) && okCount.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if okCount.Load() < 2 {
		t.Fatalf("expected non-panicking handler to keep receiving events, got %d", okCount.Load())
	}
}

func TestBus_DispatchLoop_StopsWhenQueueClosed(t *testing.T) {
	bus := NewBus(WithQueueDepth(1))
	defer bus.Close()

	close(bus.queue)

	done := make(chan struct{})
	go func() {
		bus.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("dispatch loop did not stop after queue close")
	}
}

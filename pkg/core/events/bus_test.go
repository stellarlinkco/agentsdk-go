package events

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusPreservesOrderPerSubscriber(t *testing.T) {
	t.Parallel()
	bus := NewBus(WithQueueDepth(8))
	defer bus.Close()

	var seen []string
	var mu sync.Mutex
	done := make(chan struct{})
	bus.Subscribe(PreToolUse, func(_ context.Context, evt Event) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, evt.ID)
		if len(seen) == 5 {
			close(done)
		}
	})

	var expected []string
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("custom-%d", i)
		expected = append(expected, id)
		if err := bus.Publish(Event{Type: PreToolUse, ID: id}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for events")
	}
	if len(seen) != len(expected) {
		t.Fatalf("unexpected length seen=%d expected=%d", len(seen), len(expected))
	}
	for i := range expected {
		if seen[i] != expected[i] {
			t.Fatalf("order violated, got %v expected %v", seen, expected)
		}
	}
}

func TestBusDedupeDropsRepeatedIDs(t *testing.T) {
	t.Parallel()
	bus := NewBus(WithDedupWindow(4))
	defer bus.Close()
	var count atomic.Int32
	bus.Subscribe(PostToolUse, func(_ context.Context, evt Event) {
		_ = evt
		count.Add(1)
	})
	dupID := "same-id"
	for i := 0; i < 3; i++ {
		if err := bus.Publish(Event{Type: PostToolUse, ID: dupID}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if count.Load() != 1 {
		t.Fatalf("expected deduped to 1, got %d", count.Load())
	}
}

func TestDeduperEvictsOldEntries(t *testing.T) {
	t.Parallel()
	d := newDeduper(2)
	if !d.Allow("a") || !d.Allow("b") {
		t.Fatalf("expected initial allows")
	}
	if d.Allow("a") {
		t.Fatalf("should reject duplicate within window")
	}
	if !d.Allow("c") {
		t.Fatalf("should allow after evicting oldest")
	}
	if !d.Allow("a") {
		t.Fatalf("a should be allowed after eviction")
	}
}

func TestBusConcurrentSubscribeSafety(t *testing.T) {
	t.Parallel()
	bus := NewBus(WithBufferSize(1))
	defer bus.Close()
	var consumed atomic.Int32
	wg := sync.WaitGroup{}
	for s := 0; s < 5; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unsub := bus.Subscribe(Notification, func(_ context.Context, evt Event) {
				if evt.Type == Notification {
					consumed.Add(1)
				}
			})
			defer unsub()
			for i := 0; i < 20; i++ {
				_ = bus.Publish(Event{Type: Notification})
			}
		}()
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	if consumed.Load() == 0 {
		t.Fatalf("expected some notifications consumed")
	}
}

func TestBusSubscriptionTimeout(t *testing.T) {
	t.Parallel()
	bus := NewBus()
	defer bus.Close()
	start := time.Now()
	done := make(chan struct{})
	bus.Subscribe(SessionStart, func(ctx context.Context, evt Event) {
		_ = evt
		select {
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
		}
		close(done)
	}, WithSubscriptionTimeout(50*time.Millisecond))

	if err := bus.Publish(Event{Type: SessionStart}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("handler never returned")
	}
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("timeout not respected, elapsed %v", elapsed)
	}
}

func TestBusCloseStopsDispatch(t *testing.T) {
	t.Parallel()
	bus := NewBus()
	var called atomic.Bool
	unsub := bus.Subscribe(Stop, func(_ context.Context, _ Event) {
		called.Store(true)
	})
	bus.Close()
	unsub()
	if err := bus.Publish(Event{Type: Stop}); err == nil {
		t.Fatalf("expected error after close")
	}
	time.Sleep(20 * time.Millisecond)
	if called.Load() {
		t.Fatalf("handler should not run after close")
	}
}

func TestEventValidateFailures(t *testing.T) {
	t.Parallel()
	if err := (Event{}).Validate(); err == nil {
		t.Fatalf("expected missing type error")
	}
}

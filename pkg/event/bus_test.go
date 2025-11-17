package event

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEventBusEmitDispatches(t *testing.T) {
	progress := make(chan Event, 1)
	control := make(chan Event, 1)
	monitor := make(chan Event, 1)
	bus := NewEventBus(progress, control, monitor)
	t.Cleanup(func() { _ = bus.Seal() })

	if err := bus.Emit(NewEvent(EventProgress, "s1", ProgressData{Stage: "plan"})); err != nil {
		t.Fatalf("emit progress: %v", err)
	}
	select {
	case got := <-progress:
		if got.Type != EventProgress {
			t.Fatalf("unexpected progress type: %s", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("progress event not delivered")
	}

	if err := bus.Emit(NewEvent(EventApprovalReq, "s1", ApprovalRequest{ID: "1"})); err != nil {
		t.Fatalf("emit control: %v", err)
	}
	select {
	case got := <-control:
		if got.Type != EventApprovalReq {
			t.Fatalf("unexpected control type: %s", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("control event not delivered")
	}

	if err := bus.Emit(NewEvent(EventError, "s1", ErrorData{Message: "boom"})); err != nil {
		t.Fatalf("emit monitor: %v", err)
	}
	select {
	case got := <-monitor:
		if got.Type != EventError {
			t.Fatalf("unexpected monitor type: %s", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("monitor event not delivered")
	}
}

func TestEventBusAutoSeal(t *testing.T) {
	progress := make(chan Event, 1)
	bus := NewEventBus(progress, make(chan Event, 1), make(chan Event, 1))
	completion := NewEvent(EventCompletion, "sess", CompletionData{Output: "done"})
	if err := bus.Emit(completion); err != nil {
		t.Fatalf("emit completion: %v", err)
	}
	if !bus.Sealed() {
		t.Fatal("bus should be sealed after completion")
	}
	if err := bus.Emit(NewEvent(EventProgress, "sess", nil)); !errors.Is(err, ErrBusSealed) {
		t.Fatalf("expected ErrBusSealed, got %v", err)
	}
	select {
	case got := <-progress:
		if got.Type != EventCompletion {
			t.Fatalf("expected completion event, got %s", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("completion event missing")
	}
}

func TestEventBusBufferedEmitDoesNotBlock(t *testing.T) {
	progress := make(chan Event, 1)
	control := make(chan Event)
	monitor := make(chan Event, 1)
	bus := NewEventBus(progress, control, monitor, WithBufferSize(1))
	defer bus.Seal()

	done := make(chan error, 1)
	go func() {
		done <- bus.Emit(NewEvent(EventApprovalReq, "s", ApprovalRequest{ID: "slow"}))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("emit returned error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("emit blocked despite buffer")
	}

	recv := make(chan Event, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		recv <- <-control
	}()

	select {
	case evt := <-recv:
		if evt.Type != EventApprovalReq {
			t.Fatalf("unexpected control event: %s", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("control event never drained")
	}
}

func TestEventBusLoggerAndAutoSealOption(t *testing.T) {
	logger := &stubBusLogger{}
	progress := make(chan Event, 1)
	monitor := make(chan Event, 1)
	bus := NewEventBus(progress, nil, monitor, WithLogger(logger), WithAutoSealTypes(EventProgress), WithBufferSize(0))
	defer bus.Seal()

	if err := bus.Emit(NewEvent(EventApprovalReq, "sess", nil)); err == nil {
		t.Fatal("expected error for unbound control channel")
	}
	if logger.Count() == 0 {
		t.Fatal("expected log entry for dropped event")
	}

	if err := bus.Emit(NewEvent(EventProgress, "sess", nil)); err != nil {
		t.Fatalf("emit progress: %v", err)
	}
	select {
	case <-progress:
	case <-time.After(time.Second):
		t.Fatal("progress event not observed")
	}
	if !bus.Sealed() {
		t.Fatal("bus should be sealed by auto seal option")
	}
}

func TestEventBusSafeSendRecovers(t *testing.T) {
	logger := &stubBusLogger{}
	progress := make(chan Event, 1)
	bus := NewEventBus(progress, make(chan Event, 1), make(chan Event, 1), WithLogger(logger))
	close(progress)

	if err := bus.Emit(NewEvent(EventProgress, "sess", nil)); err != nil {
		t.Fatalf("emit: %v", err)
	}
	waitForLog(t, logger, "recovered while sending")
	_ = bus.Seal()
}

type stubBusLogger struct {
	mu      sync.Mutex
	entries []string
}

func (l *stubBusLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, fmt.Sprintf(format, args...))
}

func (l *stubBusLogger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *stubBusLogger) contains(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, entry := range l.entries {
		if strings.Contains(entry, substr) {
			return true
		}
	}
	return false
}

func waitForLog(t *testing.T, logger *stubBusLogger, substr string) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if logger.contains(substr) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("log entry containing %q not found", substr)
}

func TestChannelBindingSafeSendNil(t *testing.T) {
	logger := &stubBusLogger{}
	binding := &channelBinding{name: ChannelProgress, log: logger}
	binding.safeSend(NewEvent(EventProgress, "s", nil))
	if !logger.contains("channel is nil") {
		t.Fatal("expected log for nil sink")
	}
}

func TestEventBusBindingErrors(t *testing.T) {
	bus := NewEventBus(make(chan Event, 1), make(chan Event, 1), make(chan Event, 1), WithAutoSealTypes())
	if _, err := bus.bindingForType(EventType("unknown-type")); err == nil {
		t.Fatal("expected binding error")
	}
	if bus.shouldAutoSeal(EventType("non-seal")) {
		t.Fatal("unexpected auto seal match")
	}
}

func TestEventBusNilGuards(t *testing.T) {
	var bus *EventBus
	if err := bus.Emit(Event{}); err != errNilBus {
		t.Fatalf("expected errNilBus, got %v", err)
	}
	if err := bus.Seal(); err != errNilBus {
		t.Fatalf("expected errNilBus on seal, got %v", err)
	}
	if !bus.Sealed() {
		t.Fatal("nil bus should report sealed")
	}
}

func TestEventBusWithStoreAssignsBookmarkAndPersists(t *testing.T) {
	store := &stubEventStore{}
	progress := make(chan Event, 1)
	bus := NewEventBus(progress, make(chan Event, 1), make(chan Event, 1), WithEventStore(store))
	t.Cleanup(func() { _ = bus.Seal() })

	if err := bus.Emit(NewEvent(EventProgress, "sess", nil)); err != nil {
		t.Fatalf("emit: %v", err)
	}
	select {
	case evt := <-progress:
		if evt.Bookmark == nil || evt.Bookmark.Seq == 0 {
			t.Fatalf("bookmark not assigned: %+v", evt.Bookmark)
		}
	case <-time.After(time.Second):
		t.Fatal("no event flushed")
	}
	appended := store.snapshot()
	if len(appended) != 1 || appended[0].Bookmark == nil {
		t.Fatalf("store missing bookmark: %+v", appended)
	}
}

func TestEventBusSubscribeSince(t *testing.T) {
	store := &stubEventStore{}
	store.prefill([]Event{
		{Type: EventProgress, Bookmark: &Bookmark{Seq: 1}},
		{Type: EventProgress, Bookmark: &Bookmark{Seq: 2}},
		{Type: EventProgress, Bookmark: &Bookmark{Seq: 3}},
	})
	bus := NewEventBus(make(chan Event, 1), make(chan Event, 1), make(chan Event, 1), WithEventStore(store))
	t.Cleanup(func() { _ = bus.Seal() })

	ch, err := bus.SubscribeSince(&Bookmark{Seq: 1})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	var history []Event
	for evt := range ch {
		history = append(history, evt)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 events, got %d", len(history))
	}
	if history[0].Bookmark.Seq != 2 || history[1].Bookmark.Seq != 3 {
		t.Fatalf("unexpected seqs: %+v", history)
	}
}

func TestEventBusSubscribeWithoutStore(t *testing.T) {
	bus := NewEventBus(make(chan Event, 1), make(chan Event, 1), make(chan Event, 1))
	if _, err := bus.SubscribeSince(nil); !errors.Is(err, errStoreNotConfigured) {
		t.Fatalf("expected errStoreNotConfigured, got %v", err)
	}
}

type stubEventStore struct {
	mu     sync.RWMutex
	events []Event
}

func (s *stubEventStore) Append(evt Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, cloneEvent(evt))
	return nil
}

func (s *stubEventStore) ReadSince(bookmark *Bookmark) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var history []Event
	for _, evt := range s.events {
		bm := evt.Bookmark
		if bm == nil {
			continue
		}
		if bookmark == nil || bm.Seq > bookmark.Seq {
			history = append(history, cloneEvent(evt))
		}
	}
	return history, nil
}

func (s *stubEventStore) ReadRange(start, end *Bookmark) ([]Event, error) {
	events, _ := s.ReadSince(start)
	if end == nil {
		return events, nil
	}
	var filtered []Event
	for _, evt := range events {
		if evt.Bookmark != nil && evt.Bookmark.Seq <= end.Seq {
			filtered = append(filtered, evt)
		}
	}
	return filtered, nil
}

func (s *stubEventStore) LastBookmark() (*Bookmark, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.events) == 0 {
		return nil, nil
	}
	last := s.events[len(s.events)-1]
	if last.Bookmark == nil {
		return nil, nil
	}
	return last.Bookmark.Clone(), nil
}

func (s *stubEventStore) snapshot() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copyEvents := make([]Event, len(s.events))
	for i, evt := range s.events {
		copyEvents[i] = cloneEvent(evt)
	}
	return copyEvents
}

func (s *stubEventStore) prefill(events []Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = make([]Event, len(events))
	for i, evt := range events {
		s.events[i] = cloneEvent(evt)
	}
}

func cloneEvent(evt Event) Event {
	if evt.Bookmark != nil {
		copy := *evt.Bookmark
		evt.Bookmark = &copy
	}
	return evt
}

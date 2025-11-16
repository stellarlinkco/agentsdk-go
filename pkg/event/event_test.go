package event

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEventValidationAndChannel(t *testing.T) {
	t.Parallel()
	evt := NewEvent(EventProgress, "sess", ProgressData{Stage: "run"})
	if err := evt.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if ch, ok := evt.Type.Channel(); !ok || ch != ChannelProgress {
		t.Fatalf("unexpected channel: %v %v", ch, ok)
	}
	invalid := Event{Type: EventType("unknown")}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected validation error for unknown type")
	}
}

func TestNormalizeEventClonesBookmark(t *testing.T) {
	bm, err := NewBookmark("bk", 10, map[string]string{"state": "x"})
	if err != nil {
		t.Fatalf("bookmark: %v", err)
	}
	evt := Event{Type: EventProgress, Bookmark: bm}
	normalized := normalizeEvent(evt)
	if normalized.ID == "" || normalized.Timestamp.IsZero() {
		t.Fatal("expected normalized id/timestamp")
	}
	if normalized.Bookmark == bm || normalized.Bookmark == nil {
		t.Fatal("bookmark should be cloned")
	}
}

func TestStreamSendAndStreamEvents(t *testing.T) {
	stream := NewStream()
	stream.SetHeartbeat(0)
	req := httptest.NewRequest(http.MethodGet, "/run/stream", nil)
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		stream.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	if err := stream.Send(NewEvent(EventProgress, "sess", "payload")); err != nil {
		cancel()
		<-done
		t.Fatalf("send: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done
	body := rec.Body.String()
	if !strings.Contains(body, "event: progress") {
		t.Fatalf("missing progress frame: %s", body)
	}

	stream.SetHeartbeat(5 * time.Millisecond)
	req = httptest.NewRequest(http.MethodGet, "/run/stream", nil)
	ctx, cancel = context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	done = make(chan struct{})
	go func() {
		stream.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	events := make(chan Event)
	go func() {
		events <- NewEvent(EventThinking, "sess", nil)
		time.Sleep(15 * time.Millisecond)
		close(events)
	}()
	if err := stream.StreamEvents(ctx, events); err != nil {
		cancel()
		<-done
		t.Fatalf("stream events: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done
	output := rec.Body.String()
	if !strings.Contains(output, "event: thinking") {
		t.Fatalf("missing streamed event: %s", output)
	}
	if !strings.Contains(output, "heartbeat") {
		t.Fatalf("missing heartbeat: %s", output)
	}
	if !strings.Contains(output, "event: complete") {
		t.Fatalf("missing completion frame: %s", output)
	}
}

func TestStreamBroadcastsToMultipleClients(t *testing.T) {
	stream := NewStream()
	stream.SetHeartbeat(0)
	var wg sync.WaitGroup
	spawn := func() (*httptest.ResponseRecorder, context.CancelFunc) {
		req := httptest.NewRequest(http.MethodGet, "/run/stream", nil)
		ctx, cancel := context.WithCancel(context.Background())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream.ServeHTTP(rec, req)
		}()
		return rec, cancel
	}
	recA, cancelA := spawn()
	recB, cancelB := spawn()
	time.Sleep(10 * time.Millisecond)
	events := make(chan Event, 2)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := stream.StreamEvents(ctx, events); err != nil && err != context.Canceled {
			t.Errorf("stream events: %v", err)
		}
	}()
	events <- NewEvent(EventProgress, "sess", "multi")
	close(events)
	time.Sleep(20 * time.Millisecond)
	cancel()
	cancelA()
	cancelB()
	wg.Wait()
	if !strings.Contains(recA.Body.String(), "event: progress") {
		t.Fatalf("client A missing event: %s", recA.Body.String())
	}
	if !strings.Contains(recB.Body.String(), "event: progress") {
		t.Fatalf("client B missing event: %s", recB.Body.String())
	}
}

func TestStreamContextCancel(t *testing.T) {
	stream := NewStream()
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event)
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	if err := stream.StreamEvents(ctx, events); err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestStreamErrorPaths(t *testing.T) {
	if err := (*Stream)(nil).Send(Event{}); err == nil {
		t.Fatal("send should fail on nil stream")
	}
	if err := (*Stream)(nil).StreamEvents(context.Background(), nil); err == nil {
		t.Fatal("stream events should fail on nil stream")
	}
}

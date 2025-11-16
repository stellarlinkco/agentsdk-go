package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	defaultHeartbeat = 15 * time.Second
	defaultClientBuf = 8
	heartbeatComment = ": heartbeat %d\n\n"
	completeFrame    = "event: complete\ndata: {}\n\n"
)

// Stream manages Server-Sent Events fan-out for multiple HTTP clients.
type Stream struct {
	heartbeat time.Duration
	clientBuf int
	clients   sync.Map // map[string]*subscriber
}

// NewStream constructs a broadcast-capable SSE stream.
func NewStream() *Stream {
	return &Stream{heartbeat: defaultHeartbeat, clientBuf: defaultClientBuf}
}

// NewStreamWriter attaches a plain writer as a virtual SSE client (useful for tests).
func NewStreamWriter(w io.Writer) *Stream {
	s := NewStream()
	s.attachWriter(w)
	return s
}

// SetHeartbeat sets the interval for per-client heartbeat comments (<=0 disables).
func (s *Stream) SetHeartbeat(d time.Duration) {
	if s == nil {
		return
	}
	if d <= 0 {
		s.heartbeat = 0
		return
	}
	s.heartbeat = d
}

// ServeHTTP registers the caller as an SSE client and streams events until context cancellation.
func (s *Stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "event: stream not configured", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "event: response does not support streaming", http.StatusInternalServerError)
		return
	}

	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")

	client := newSubscriber(s.clientBuf)
	s.clients.Store(client.id, client)
	defer func() {
		client.close()
		s.clients.Delete(client.id)
	}()

	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	var ticker *time.Ticker
	if s.heartbeat > 0 {
		ticker = time.NewTicker(s.heartbeat)
		defer ticker.Stop()
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-client.queue:
			if !ok {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		case <-tickChan(ticker):
			if _, err := fmt.Fprintf(w, heartbeatComment, time.Now().Unix()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// Send broadcasts a single event to all connected clients.
func (s *Stream) Send(evt Event) error {
	if s == nil {
		return errors.New("event: stream is nil")
	}
	frame, err := encodeEvent(evt)
	if err != nil {
		return err
	}
	s.broadcast(frame)
	return nil
}

// StreamEvents consumes an event channel and relays it using SSE format.
func (s *Stream) StreamEvents(ctx context.Context, events <-chan Event) error {
	if s == nil {
		return errors.New("event: stream is nil")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-events:
			if !ok {
				s.broadcast([]byte(completeFrame))
				return nil
			}
			if err := s.Send(evt); err != nil {
				return err
			}
		}
	}
}

func (s *Stream) broadcast(frame []byte) {
	if s == nil {
		return
	}
	s.clients.Range(func(key, value any) bool {
		client, ok := value.(*subscriber)
		if !ok {
			s.clients.Delete(key)
			return true
		}
		select {
		case client.queue <- frame:
		default:
			client.close()
			s.clients.Delete(key)
		}
		return true
	})
}

func encodeEvent(evt Event) ([]byte, error) {
	normalized := normalizeEvent(evt)
	payload := struct {
		ID        string    `json:"id"`
		Type      EventType `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		SessionID string    `json:"session_id,omitempty"`
		Data      any       `json:"data,omitempty"`
		Bookmark  *Bookmark `json:"bookmark,omitempty"`
	}{
		ID:        normalized.ID,
		Type:      normalized.Type,
		Timestamp: normalized.Timestamp,
		SessionID: normalized.SessionID,
		Data:      normalized.Data,
		Bookmark:  normalized.Bookmark,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("event: marshal SSE payload: %w", err)
	}
	frame := fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", payload.ID, payload.Type, body)
	return []byte(frame), nil
}

func tickChan(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func (s *Stream) attachWriter(w io.Writer) {
	client := newSubscriber(s.clientBuf)
	s.clients.Store(client.id, client)
	go func() {
		defer s.clients.Delete(client.id)
		for frame := range client.queue {
			if _, err := w.Write(frame); err != nil {
				return
			}
		}
	}()
}

type subscriber struct {
	id    string
	queue chan []byte
	once  sync.Once
}

func newSubscriber(buffer int) *subscriber {
	if buffer < 1 {
		buffer = 1
	}
	return &subscriber{
		id:    newEventID(),
		queue: make(chan []byte, buffer),
	}
}

func (s *subscriber) close() {
	s.once.Do(func() {
		close(s.queue)
	})
}

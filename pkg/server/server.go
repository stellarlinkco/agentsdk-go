package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/event"
)

// Server exposes a minimal HTTP API around an Agent, including SSE streaming endpoints.
type Server struct {
	agent  agent.Agent
	stream *event.Stream
	mux    *http.ServeMux
}

// New creates a Server with pre-wired routes.
func New(ag agent.Agent) *Server {
	srv := &Server{
		agent:  ag,
		stream: event.NewStream(),
		mux:    http.NewServeMux(),
	}
	srv.routes()
	return srv
}

func (s *Server) routes() {
	s.mux.HandleFunc("/run", s.handleRun)
	s.mux.Handle("/run/stream", http.HandlerFunc(s.handleStream))
}

// ServeHTTP implements http.Handler and delegates to the internal mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var payload struct {
		Input     string `json:"input"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	input := strings.TrimSpace(payload.Input)
	if input == "" {
		http.Error(w, "input is required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if payload.SessionID != "" {
		ctx = agent.WithRunContext(ctx, agent.RunContext{SessionID: payload.SessionID})
	}
	result, err := s.agent.Run(ctx, input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	input := strings.TrimSpace(query.Get("input"))
	sessionID := strings.TrimSpace(query.Get("session_id"))
	ctx := r.Context()
	if sessionID != "" {
		ctx = agent.WithRunContext(ctx, agent.RunContext{SessionID: sessionID})
	}
	if input != "" {
		go s.spawnStream(ctx, input)
	}
	s.stream.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) spawnStream(ctx context.Context, input string) {
	events, err := s.agent.RunStream(ctx, input)
	if err != nil {
		_ = s.stream.Send(event.NewEvent(event.EventError, sessionFromContext(ctx), event.ErrorData{
			Message:     err.Error(),
			Kind:        "run_stream",
			Recoverable: true,
		}))
		return
	}
	if err := s.stream.StreamEvents(ctx, events); err != nil && !errors.Is(err, context.Canceled) {
		_ = s.stream.Send(event.NewEvent(event.EventError, sessionFromContext(ctx), event.ErrorData{
			Message:     err.Error(),
			Kind:        "stream",
			Recoverable: true,
		}))
	}
}

func sessionFromContext(ctx context.Context) string {
	rc, ok := agent.GetRunContext(ctx)
	if !ok {
		return ""
	}
	return rc.SessionID
}

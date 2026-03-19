package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
)

func newTestHTTPServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	root, err := api.ResolveProjectRoot()
	if err != nil {
		t.Fatalf("ResolveProjectRoot: %v", err)
	}

	rt, err := api.New(context.Background(), api.Options{
		EntryPoint:  api.EntryPointPlatform,
		ProjectRoot: root,
		Model:       &demomodel.EchoModel{Prefix: "demo"},
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	srv := &httpServer{
		runtime:        rt,
		defaultTimeout: 2 * time.Second,
		staticDir:      staticDir,
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	ts := httptest.NewServer(mux)
	return ts, func() {
		ts.Close()
		rt.Close()
	}
}

func TestHTTPServer_Health(t *testing.T) {
	ts, cleanup := newTestHTTPServer(t)
	defer cleanup()

	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("payload=%v", payload)
	}

	res, err = client.Post(ts.URL+"/health", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST /health: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestHTTPServer_StaticAndRoot(t *testing.T) {
	ts, cleanup := newTestHTTPServer(t)
	defer cleanup()

	noRedirect := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := noRedirect.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/static/index.html" {
		t.Fatalf("Location=%q", loc)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	res, err = client.Get(ts.URL + "/static/index.html")
	if err != nil {
		t.Fatalf("GET /static/index.html: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if strings.TrimSpace(string(body)) != "ok" {
		t.Fatalf("body=%q", string(body))
	}

	res, err = client.Get(ts.URL + "/nope")
	if err != nil {
		t.Fatalf("GET /nope: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestHTTPServer_RunValidation(t *testing.T) {
	ts, cleanup := newTestHTTPServer(t)
	defer cleanup()

	client := &http.Client{Timeout: 2 * time.Second}

	res, err := client.Get(ts.URL + "/v1/run")
	if err != nil {
		t.Fatalf("GET /v1/run: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", res.StatusCode)
	}

	res, err = client.Post(ts.URL+"/v1/run", "application/json", bytes.NewBufferString(``))
	if err != nil {
		t.Fatalf("POST /v1/run empty: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", res.StatusCode)
	}

	res, err = client.Post(ts.URL+"/v1/run", "application/json", bytes.NewBufferString(`{"prompt":""}`))
	if err != nil {
		t.Fatalf("POST /v1/run missing prompt: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", res.StatusCode)
	}

	res, err = client.Post(ts.URL+"/v1/run", "application/json", bytes.NewBufferString(`{"prompt":"hi","timeout_ms":1}{"prompt":"x"}`))
	if err != nil {
		t.Fatalf("POST /v1/run multi: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestHTTPServer_RunOK(t *testing.T) {
	ts, cleanup := newTestHTTPServer(t)
	defer cleanup()

	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Post(ts.URL+"/v1/run", "application/json", bytes.NewBufferString(`{"prompt":"hello","session_id":"s1"}`))
	if err != nil {
		t.Fatalf("POST /v1/run: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var payload runResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.SessionID != "s1" {
		t.Fatalf("session_id=%q", payload.SessionID)
	}
	if strings.TrimSpace(payload.Output) == "" {
		t.Fatalf("empty output")
	}
}

func TestHTTPServer_Run_AutoSessionID(t *testing.T) {
	ts, cleanup := newTestHTTPServer(t)
	defer cleanup()

	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Post(ts.URL+"/v1/run", "application/json", bytes.NewBufferString(`{"prompt":"hello"}`))
	if err != nil {
		t.Fatalf("POST /v1/run: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var payload runResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(payload.SessionID, "session-") {
		t.Fatalf("session_id=%q", payload.SessionID)
	}
}

func TestHTTPServer_Stream(t *testing.T) {
	ts, cleanup := newTestHTTPServer(t)
	defer cleanup()

	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Post(ts.URL+"/v1/run/stream", "application/json", bytes.NewBufferString(`{"prompt":"hello","session_id":"s2"}`))
	if err != nil {
		t.Fatalf("POST /v1/run/stream: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Contains(body, []byte("data:")) {
		t.Fatalf("missing sse data: %q", string(body))
	}
}

func TestRunRequest_EnsureSessionID_Generates(t *testing.T) {
	var rr runRequest
	id := rr.ensureSessionID()
	if strings.TrimSpace(id) == "" {
		t.Fatalf("expected session id")
	}
	if id2 := rr.ensureSessionID(); id2 != id {
		t.Fatalf("expected stable session id, got %q then %q", id, id2)
	}
}

func TestHTTPServer_Decode_NilBody(t *testing.T) {
	s := &httpServer{}
	if err := s.decode(&http.Request{Body: nil}, &runRequest{}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHTTPServer_Decode_UnknownField(t *testing.T) {
	s := &httpServer{}
	req := httptest.NewRequest(http.MethodPost, "/v1/run", bytes.NewBufferString(`{"prompt":"hi","unknown":1}`))
	if err := s.decode(req, &runRequest{}); err == nil {
		t.Fatalf("expected error")
	}
}

type sseRecorder struct {
	header http.Header
	code   int
	buf    bytes.Buffer
}

func (r *sseRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *sseRecorder) WriteHeader(statusCode int) { r.code = statusCode }

func (r *sseRecorder) Write(p []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.buf.Write(p)
}

func (r *sseRecorder) Flush() {}

type noFlushWriter struct {
	header http.Header
	code   int
}

func (w *noFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *noFlushWriter) WriteHeader(statusCode int) { w.code = statusCode }
func (w *noFlushWriter) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return len(p), nil
}

func TestHTTPServer_Stream_MethodNotAllowed(t *testing.T) {
	s := &httpServer{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/run/stream", nil)
	s.handleStream(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestHTTPServer_Stream_PromptRequired(t *testing.T) {
	s := &httpServer{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/run/stream", bytes.NewBufferString(`{"prompt":""}`))
	s.handleStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestHTTPServer_Stream_DecodeError(t *testing.T) {
	s := &httpServer{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/run/stream", bytes.NewBufferString(``))
	s.handleStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHTTPServer_Stream_StreamingUnsupported(t *testing.T) {
	s := &httpServer{}
	w := &noFlushWriter{}
	req := httptest.NewRequest(http.MethodPost, "/v1/run/stream", bytes.NewBufferString(`{"prompt":"hi"}`))
	s.handleStream(w, req)
	if w.code != http.StatusInternalServerError {
		t.Fatalf("status=%d", w.code)
	}
}

func TestHTTPServer_Stream_RuntimeClosedIsBadGateway(t *testing.T) {
	root, err := api.ResolveProjectRoot()
	if err != nil {
		t.Fatalf("ResolveProjectRoot: %v", err)
	}
	rt, err := api.New(context.Background(), api.Options{
		EntryPoint:  api.EntryPointPlatform,
		ProjectRoot: root,
		Model:       &demomodel.EchoModel{Prefix: "demo"},
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	_ = rt.Close()

	s := &httpServer{runtime: rt, defaultTimeout: time.Second}
	w := &sseRecorder{}
	req := httptest.NewRequest(http.MethodPost, "/v1/run/stream", bytes.NewBufferString(`{"prompt":"hi","session_id":"s"}`))
	s.handleStream(w, req)
	if w.code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%q", w.code, w.buf.String())
	}
}

func TestHTTPServer_Run_RuntimeClosedIsBadGateway(t *testing.T) {
	root, err := api.ResolveProjectRoot()
	if err != nil {
		t.Fatalf("ResolveProjectRoot: %v", err)
	}
	rt, err := api.New(context.Background(), api.Options{
		EntryPoint:  api.EntryPointPlatform,
		ProjectRoot: root,
		Model:       &demomodel.EchoModel{Prefix: "demo"},
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	_ = rt.Close()

	s := &httpServer{runtime: rt, defaultTimeout: time.Second}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/run", bytes.NewBufferString(`{"prompt":"hi","session_id":"s"}`))
	s.handleRun(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHTTPServer_RequestContext_DefaultTimeoutFallback(t *testing.T) {
	s := &httpServer{defaultTimeout: 0}
	ctx, cancel := s.requestContext(context.Background(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatalf("expected deadline")
	}
}

func TestHTTPServer_RequestContext_ExplicitTimeoutMs(t *testing.T) {
	s := &httpServer{defaultTimeout: time.Hour}
	ctx, cancel := s.requestContext(context.Background(), 1)
	defer cancel()
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > time.Second {
		t.Fatalf("unexpected deadline")
	}
}

type fakeRuntime struct {
	run       func(context.Context, api.Request) (*api.Response, error)
	runStream func(context.Context, api.Request) (<-chan api.StreamEvent, error)
}

func (r fakeRuntime) Run(ctx context.Context, req api.Request) (*api.Response, error) {
	return r.run(ctx, req)
}

func (r fakeRuntime) RunStream(ctx context.Context, req api.Request) (<-chan api.StreamEvent, error) {
	return r.runStream(ctx, req)
}

func (r fakeRuntime) Close() error { return nil }

func TestHTTPServer_Run_ResultNilIsInternalError(t *testing.T) {
	s := &httpServer{
		runtime: fakeRuntime{
			run: func(context.Context, api.Request) (*api.Response, error) {
				return &api.Response{Result: nil}, nil
			},
		},
		defaultTimeout: time.Second,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/run", bytes.NewBufferString(`{"prompt":"hi","session_id":"s"}`))
	s.handleRun(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHTTPServer_Stream_PingAndCancel(t *testing.T) {
	old := streamPingPeriod
	t.Cleanup(func() { streamPingPeriod = old })
	streamPingPeriod = time.Millisecond

	never := make(chan api.StreamEvent)
	s := &httpServer{
		runtime: fakeRuntime{
			runStream: func(context.Context, api.Request) (<-chan api.StreamEvent, error) { return never, nil },
		},
		defaultTimeout: 20 * time.Millisecond,
	}
	w := &sseRecorder{}
	req := httptest.NewRequest(http.MethodPost, "/v1/run/stream", bytes.NewBufferString(`{"prompt":"hi","session_id":"s","timeout_ms":15}`))
	s.handleStream(w, req)
	if !bytes.Contains(w.buf.Bytes(), []byte(`{"type":"ping"}`)) {
		t.Fatalf("missing ping: %q", w.buf.String())
	}
}

func TestHTTPServer_Stream_ImmediatePingWithoutTicker(t *testing.T) {
	old := streamPingPeriod
	t.Cleanup(func() { streamPingPeriod = old })
	streamPingPeriod = time.Hour

	never := make(chan api.StreamEvent)
	s := &httpServer{
		runtime: fakeRuntime{
			runStream: func(context.Context, api.Request) (<-chan api.StreamEvent, error) { return never, nil },
		},
		defaultTimeout: 20 * time.Millisecond,
	}
	w := &sseRecorder{}
	req := httptest.NewRequest(http.MethodPost, "/v1/run/stream", bytes.NewBufferString(`{"prompt":"hi","session_id":"s","timeout_ms":15}`))
	s.handleStream(w, req)
	if !bytes.Contains(w.buf.Bytes(), []byte(`{"type":"ping"}`)) {
		t.Fatalf("missing immediate ping: %q", w.buf.String())
	}
}

func TestHTTPServer_Stream_MarshalErrorStops(t *testing.T) {
	events := make(chan api.StreamEvent, 1)
	events <- api.StreamEvent{Type: api.EventToolExecutionOutput, Output: make(chan int)}
	close(events)

	s := &httpServer{
		runtime: fakeRuntime{
			runStream: func(context.Context, api.Request) (<-chan api.StreamEvent, error) { return events, nil },
		},
		defaultTimeout: time.Second,
	}
	w := &sseRecorder{}
	req := httptest.NewRequest(http.MethodPost, "/v1/run/stream", bytes.NewBufferString(`{"prompt":"hi","session_id":"s"}`))
	s.handleStream(w, req)
}

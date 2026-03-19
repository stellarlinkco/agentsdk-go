package middleware

import (
	"net/http"
	"time"
)

// HTTPTraceEvent captures a single HTTP exchange at a coarse level.
type HTTPTraceEvent struct {
	Method     string `json:"method"`
	URL        string `json:"url"`
	Status     int    `json:"status"`
	DurationMS int64  `json:"duration_ms"`
}

// HTTPTraceWriter persists events. Implementations must be concurrency-safe.
type HTTPTraceWriter interface {
	WriteHTTPTrace(event *HTTPTraceEvent) error
}

// HTTPTraceMiddleware captures HTTP status + latency and forwards it to a writer.
type HTTPTraceMiddleware struct {
	writer HTTPTraceWriter
}

func NewHTTPTraceMiddleware(writer HTTPTraceWriter) *HTTPTraceMiddleware {
	return &HTTPTraceMiddleware{writer: writer}
}

func (m *HTTPTraceMiddleware) Handler(next http.Handler) http.Handler { return m.Wrap(next) }

func (m *HTTPTraceMiddleware) Wrap(next http.Handler) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	if m == nil || m.writer == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &httpTraceStatusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if err := m.writer.WriteHTTPTrace(&HTTPTraceEvent{
			Method:     r.Method,
			URL:        r.URL.String(),
			Status:     sw.statusOrOK(),
			DurationMS: time.Since(start).Milliseconds(),
		}); err != nil {
			// Best-effort tracing: do not break the response path.
			_ = err
		}
	})
}

type httpTraceStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *httpTraceStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *httpTraceStatusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *httpTraceStatusWriter) statusOrOK() int {
	if w == nil || w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

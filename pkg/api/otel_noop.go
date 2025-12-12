//go:build !otel

package api

// noopTracer provides a no-operation tracer when OTEL is not enabled.
// This is the default implementation used without the 'otel' build tag.
type noopTracer struct{}

// NewTracer creates a tracer. Without the otel build tag, returns a noop tracer.
func NewTracer(_ OTELConfig) (Tracer, error) {
	return &noopTracer{}, nil
}

func (t *noopTracer) StartAgentSpan(_, _ string, _ int) SpanContext {
	return &noopSpan{}
}

func (t *noopTracer) StartModelSpan(_ SpanContext, _ string) SpanContext {
	return &noopSpan{}
}

func (t *noopTracer) StartToolSpan(_ SpanContext, _ string) SpanContext {
	return &noopSpan{}
}

func (t *noopTracer) EndSpan(_ SpanContext, _ map[string]any, _ error) {}

func (t *noopTracer) Shutdown() error { return nil }

// noopSpan is a no-operation span context.
type noopSpan struct{}

func (s *noopSpan) TraceID() string   { return "" }
func (s *noopSpan) SpanID() string    { return "" }
func (s *noopSpan) IsRecording() bool { return false }

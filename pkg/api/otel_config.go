package api

// OTELConfig configures OpenTelemetry integration for the SDK.
// When Enabled is true, spans are created for agent runs, model calls, and tool executions.
// OTEL dependencies are optional and only loaded when using build tag 'otel'.
type OTELConfig struct {
	// Enabled activates OpenTelemetry tracing.
	Enabled bool `json:"enabled"`

	// ServiceName identifies this service in traces. Defaults to "agentsdk-go".
	ServiceName string `json:"service_name,omitempty"`

	// Endpoint is the OTLP endpoint URL (e.g., "http://localhost:4318").
	Endpoint string `json:"endpoint,omitempty"`

	// Headers are additional HTTP headers sent with OTLP requests.
	Headers map[string]string `json:"headers,omitempty"`

	// SampleRate controls the sampling ratio (0.0-1.0). Defaults to 1.0 (100%).
	SampleRate float64 `json:"sample_rate,omitempty"`

	// Insecure allows non-TLS connections to the endpoint.
	Insecure bool `json:"insecure,omitempty"`
}

// DefaultOTELConfig returns sensible defaults for OTEL configuration.
func DefaultOTELConfig() OTELConfig {
	return OTELConfig{
		ServiceName: "agentsdk-go",
		SampleRate:  1.0,
	}
}

// Tracer provides the interface for distributed tracing operations.
// Implementations are swapped based on build tags (otel vs noop).
type Tracer interface {
	// StartAgentSpan creates a span for an agent run.
	StartAgentSpan(sessionID, requestID string, iteration int) SpanContext

	// StartModelSpan creates a child span for model generation.
	StartModelSpan(parent SpanContext, modelName string) SpanContext

	// StartToolSpan creates a child span for tool execution.
	StartToolSpan(parent SpanContext, toolName string) SpanContext

	// EndSpan completes a span with optional attributes.
	EndSpan(span SpanContext, attrs map[string]any, err error)

	// Shutdown gracefully closes the tracer and flushes pending spans.
	Shutdown() error
}

// SpanContext carries span identification for propagation.
type SpanContext interface {
	// TraceID returns the trace identifier.
	TraceID() string

	// SpanID returns the span identifier.
	SpanID() string

	// IsRecording returns true if the span is being recorded.
	IsRecording() bool
}

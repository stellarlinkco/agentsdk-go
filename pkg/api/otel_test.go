package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOTELConfig_Defaults(t *testing.T) {
	cfg := DefaultOTELConfig()
	assert.Equal(t, "agentsdk-go", cfg.ServiceName)
	assert.Equal(t, 1.0, cfg.SampleRate)
	assert.False(t, cfg.Enabled)
}

func TestNewTracer_Noop(t *testing.T) {
	// Without the 'otel' build tag, NewTracer always returns noop
	tracer, err := NewTracer(OTELConfig{})
	require.NoError(t, err)
	require.NotNil(t, tracer)

	// All operations should be no-ops
	span := tracer.StartAgentSpan("session-1", "request-1", 0)
	require.NotNil(t, span)
	assert.Equal(t, "", span.TraceID())
	assert.Equal(t, "", span.SpanID())
	assert.False(t, span.IsRecording())

	modelSpan := tracer.StartModelSpan(span, "claude-3")
	assert.NotNil(t, modelSpan)
	assert.False(t, modelSpan.IsRecording())

	toolSpan := tracer.StartToolSpan(span, "bash")
	assert.NotNil(t, toolSpan)
	assert.False(t, toolSpan.IsRecording())

	// EndSpan should not panic
	tracer.EndSpan(span, map[string]any{"key": "value"}, nil)
	tracer.EndSpan(modelSpan, nil, assert.AnError)

	// Shutdown should be clean
	err = tracer.Shutdown()
	assert.NoError(t, err)
}

func TestNoopTracer_NilSafe(t *testing.T) {
	tracer := &noopTracer{}

	// Should handle nil parent spans
	span := tracer.StartModelSpan(nil, "model")
	assert.NotNil(t, span)

	// Should handle nil span in EndSpan
	tracer.EndSpan(nil, nil, nil)
}

func TestNoopSpan_Interface(t *testing.T) {
	span := &noopSpan{}

	// Verify interface implementation
	var _ SpanContext = span

	assert.Equal(t, "", span.TraceID())
	assert.Equal(t, "", span.SpanID())
	assert.False(t, span.IsRecording())
}

func TestNoopTracer_EndSpan_Coverage(t *testing.T) {
	tracer := &noopTracer{}
	span := &noopSpan{}

	// EndSpan should be a no-op but shouldn't panic
	tracer.EndSpan(span, map[string]any{
		"string": "value",
		"int":    42,
		"bool":   true,
	}, nil)

	// With error
	tracer.EndSpan(span, nil, assert.AnError)

	// With nil span
	tracer.EndSpan(nil, nil, nil)
}

func TestWithOTEL_Option(t *testing.T) {
	opts := &Options{}
	cfg := OTELConfig{
		Enabled:     true,
		ServiceName: "test-service",
		Endpoint:    "http://localhost:4318",
		SampleRate:  0.5,
	}

	WithOTEL(cfg)(opts)

	assert.True(t, opts.OTEL.Enabled)
	assert.Equal(t, "test-service", opts.OTEL.ServiceName)
	assert.Equal(t, "http://localhost:4318", opts.OTEL.Endpoint)
	assert.Equal(t, 0.5, opts.OTEL.SampleRate)
}

func TestOTELConfig_Headers(t *testing.T) {
	cfg := OTELConfig{
		Enabled: true,
		Headers: map[string]string{
			"Authorization": "Bearer token",
			"X-Custom":      "value",
		},
	}

	assert.Len(t, cfg.Headers, 2)
	assert.Equal(t, "Bearer token", cfg.Headers["Authorization"])
}

func TestOTELConfig_Insecure(t *testing.T) {
	cfg := OTELConfig{
		Enabled:  true,
		Insecure: true,
	}
	assert.True(t, cfg.Insecure)
}

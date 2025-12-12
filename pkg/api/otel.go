//go:build otel

package api

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// otelTracer wraps OpenTelemetry tracing for agent SDK operations.
type otelTracer struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
}

// NewTracer creates an OpenTelemetry tracer with the given configuration.
// Requires build tag 'otel' to include actual OTEL dependencies.
func NewTracer(cfg OTELConfig) (Tracer, error) {
	if !cfg.Enabled {
		return &noopTracerReal{}, nil
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "agentsdk-go"
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 1.0
	}

	// Configure OTLP exporter options
	opts := []otlptracehttp.Option{}
	if cfg.Endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint))
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	for k, v := range cfg.Headers {
		opts = append(opts, otlptracehttp.WithHeaders(map[string]string{k: v}))
	}

	exporter, err := otlptrace.New(context.Background(), otlptracehttp.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("otel: failed to create exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: failed to create resource: %w", err)
	}

	// Configure sampler based on sample rate
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(provider)
	tracer := provider.Tracer("agentsdk-go")

	return &otelTracer{
		provider: provider,
		tracer:   tracer,
	}, nil
}

func (t *otelTracer) StartAgentSpan(sessionID, requestID string, iteration int) SpanContext {
	ctx, span := t.tracer.Start(context.Background(), "agent.run",
		trace.WithAttributes(
			attribute.String("agent.session_id", sessionID),
			attribute.String("agent.request_id", requestID),
			attribute.Int("agent.iteration", iteration),
		),
	)
	return &otelSpan{ctx: ctx, span: span}
}

func (t *otelTracer) StartModelSpan(parent SpanContext, modelName string) SpanContext {
	parentCtx := context.Background()
	if ps, ok := parent.(*otelSpan); ok && ps != nil {
		parentCtx = ps.ctx
	}
	ctx, span := t.tracer.Start(parentCtx, "model.generate",
		trace.WithAttributes(
			attribute.String("model.name", modelName),
		),
	)
	return &otelSpan{ctx: ctx, span: span}
}

func (t *otelTracer) StartToolSpan(parent SpanContext, toolName string) SpanContext {
	parentCtx := context.Background()
	if ps, ok := parent.(*otelSpan); ok && ps != nil {
		parentCtx = ps.ctx
	}
	ctx, span := t.tracer.Start(parentCtx, "tool.execute",
		trace.WithAttributes(
			attribute.String("tool.name", toolName),
		),
	)
	return &otelSpan{ctx: ctx, span: span}
}

func (t *otelTracer) EndSpan(span SpanContext, attrs map[string]any, err error) {
	os, ok := span.(*otelSpan)
	if !ok || os == nil {
		return
	}

	// Set additional attributes
	for k, v := range attrs {
		switch val := v.(type) {
		case string:
			os.span.SetAttributes(attribute.String(k, val))
		case int:
			os.span.SetAttributes(attribute.Int(k, val))
		case int64:
			os.span.SetAttributes(attribute.Int64(k, val))
		case float64:
			os.span.SetAttributes(attribute.Float64(k, val))
		case bool:
			os.span.SetAttributes(attribute.Bool(k, val))
		}
	}

	if err != nil {
		os.span.RecordError(err)
		os.span.SetStatus(codes.Error, err.Error())
	} else {
		os.span.SetStatus(codes.Ok, "")
	}

	os.span.End()
}

func (t *otelTracer) Shutdown() error {
	if t.provider != nil {
		return t.provider.Shutdown(context.Background())
	}
	return nil
}

// otelSpan wraps an OpenTelemetry span.
type otelSpan struct {
	ctx  context.Context
	span trace.Span
}

func (s *otelSpan) TraceID() string {
	if s.span == nil {
		return ""
	}
	return s.span.SpanContext().TraceID().String()
}

func (s *otelSpan) SpanID() string {
	if s.span == nil {
		return ""
	}
	return s.span.SpanContext().SpanID().String()
}

func (s *otelSpan) IsRecording() bool {
	if s.span == nil {
		return false
	}
	return s.span.IsRecording()
}

// noopTracerReal is used when OTEL is compiled but disabled at runtime.
type noopTracerReal struct{}

func (t *noopTracerReal) StartAgentSpan(_, _ string, _ int) SpanContext {
	return &noopSpanReal{}
}

func (t *noopTracerReal) StartModelSpan(_ SpanContext, _ string) SpanContext {
	return &noopSpanReal{}
}

func (t *noopTracerReal) StartToolSpan(_ SpanContext, _ string) SpanContext {
	return &noopSpanReal{}
}

func (t *noopTracerReal) EndSpan(_ SpanContext, _ map[string]any, _ error) {}

func (t *noopTracerReal) Shutdown() error { return nil }

type noopSpanReal struct{}

func (s *noopSpanReal) TraceID() string   { return "" }
func (s *noopSpanReal) SpanID() string    { return "" }
func (s *noopSpanReal) IsRecording() bool { return false }

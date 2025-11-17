package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/cexll/agentsdk-go/pkg/telemetry"
)

func setupTelemetry(t *testing.T) (*telemetry.Manager, *sdkmetric.ManualReader, *tracetest.InMemoryExporter) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
	)
	mgr, err := telemetry.NewManager(telemetry.Config{
		ServiceName:    "telemetry-example",
		ServiceVersion: "0.1.0",
		Environment:    "test",
		MeterProvider:  mp,
		TracerProvider: tp,
		Filter: telemetry.FilterConfig{
			Mask:     "***REDACTED***",
			Patterns: []string{`customer-id\s*[=:]\s*\d+`},
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	telemetry.SetDefault(mgr)
	t.Cleanup(func() {
		telemetry.SetDefault(nil)
		_ = mgr.Shutdown(context.Background())
	})
	return mgr, reader, exporter
}

func collectMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				return metric
			}
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func TestTelemetryManagerInit(t *testing.T) {
	mgr, _, _ := setupTelemetry(t)
	if telemetry.Default() != mgr {
		t.Fatalf("default manager not set")
	}
	_, span := mgr.StartSpan(context.Background(), "examples.telemetry.init")
	if !span.SpanContext().TraceID().IsValid() {
		t.Fatalf("expected span context to be valid")
	}
	masked := mgr.MaskText("customer-id=4242 token sk-secret-001")
	if strings.Contains(masked, "4242") || strings.Contains(masked, "sk-secret") {
		t.Fatalf("expected sensitive values masked, got %q", masked)
	}
	attrs := mgr.SanitizeAttributes(attribute.String("request.input", "sk-secret-002"))
	if len(attrs) != 1 || strings.Contains(attrs[0].Value.AsString(), "sk-secret") {
		t.Fatalf("expected sanitized attribute, got %+v", attrs)
	}
	telemetry.EndSpan(span, nil)
}

func TestSensitiveDataFilter(t *testing.T) {
	setupTelemetry(t)
	masked := telemetry.MaskText("run sk-secret-003 for customer-id=9999")
	if strings.Contains(masked, "sk-secret") || strings.Contains(masked, "9999") {
		t.Fatalf("expected mask applied, got %q", masked)
	}
	attrs := telemetry.SanitizeAttributes(
		attribute.String("request.input", "sk-secret-004"),
		attribute.StringSlice("notes", []string{"customer-id: 7777"}),
	)
	if strings.Contains(attrs[0].Value.AsString(), "sk-secret") {
		t.Fatalf("string attribute not masked: %+v", attrs[0])
	}
	for _, v := range attrs[1].Value.AsStringSlice() {
		if strings.Contains(v, "7777") {
			t.Fatalf("string slice entry not masked: %q", v)
		}
	}
}

func TestRequestMetrics(t *testing.T) {
	mgr, reader, _ := setupTelemetry(t)
	mgr.RecordRequest(context.Background(), telemetry.RequestData{
		Kind:      "Run",
		AgentName: "telemetry-example-agent",
		SessionID: "telemetry-session-001",
		Input:     "Run `echo` with key sk-secret-005 and customer-id=5555",
		Duration:  42 * time.Millisecond,
		Error:     errors.New("timeout"),
	})
	metric := collectMetric(t, reader, "agent.requests.total")
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("unexpected request metric: %#v", metric.Data)
	}
	dp := sum.DataPoints[0]
	if dp.Value != 1 {
		t.Fatalf("expected single request increment, got %d", dp.Value)
	}
	if val, ok := dp.Attributes.Value(attribute.Key("agent.input")); !ok || strings.Contains(val.AsString(), "sk-secret") || strings.Contains(val.AsString(), "5555") {
		t.Fatalf("expected sanitized request input, got %+v", val)
	}
	if flag, ok := dp.Attributes.Value(attribute.Key("agent.request.error")); !ok || !flag.AsBool() {
		t.Fatalf("expected error attribute set, got %+v", flag)
	}
}

func TestToolMetrics(t *testing.T) {
	mgr, reader, _ := setupTelemetry(t)
	mgr.RecordToolCall(context.Background(), telemetry.ToolData{
		AgentName: "telemetry-example-agent",
		Name:      "bash",
		Error:     errors.New("missing binary"),
	})
	metric := collectMetric(t, reader, "tool.calls.total")
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("unexpected tool metric: %#v", metric.Data)
	}
	dp := sum.DataPoints[0]
	if val, ok := dp.Attributes.Value(attribute.Key("tool.name")); !ok || val.AsString() != "bash" {
		t.Fatalf("expected tool attribute, got %+v", val)
	}
	if flag, ok := dp.Attributes.Value(attribute.Key("tool.error")); !ok || !flag.AsBool() {
		t.Fatalf("expected tool error flag, got %+v", flag)
	}
	if agent, ok := dp.Attributes.Value(attribute.Key("agent.name")); !ok || agent.AsString() != "telemetry-example-agent" {
		t.Fatalf("expected agent attribute, got %+v", agent)
	}
}

func TestSpanOperations(t *testing.T) {
	_, _, exporter := setupTelemetry(t)
	ctx, span := telemetry.StartSpan(context.Background(), "examples.telemetry.request")
	telemetry.EndSpan(span, errors.New("upstream timeout"))
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected span exported, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Fatalf("expected error status, got %v", spans[0].Status)
	}

	ctx, span = telemetry.StartSpan(ctx, "examples.telemetry.cleanup")
	telemetry.EndSpan(span, nil)
	spans = exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected two spans, got %d", len(spans))
	}
	if spans[1].Status.Code != codes.Ok {
		t.Fatalf("expected ok status, got %v", spans[1].Status)
	}
}

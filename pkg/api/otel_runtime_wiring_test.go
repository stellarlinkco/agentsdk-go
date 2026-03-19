package api

import (
	"context"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

type testSpan struct{}

func (testSpan) TraceID() string   { return "trace" }
func (testSpan) SpanID() string    { return "span" }
func (testSpan) IsRecording() bool { return true }

type recordingTracer struct {
	agent int
	model int
	tool  int
	end   int
	shut  int
}

func (t *recordingTracer) StartAgentSpan(_, _ string, _ int) SpanContext {
	t.agent++
	return testSpan{}
}

func (t *recordingTracer) StartModelSpan(_ SpanContext, _ string) SpanContext {
	t.model++
	return testSpan{}
}

func (t *recordingTracer) StartToolSpan(_ SpanContext, _ string) SpanContext {
	t.tool++
	return testSpan{}
}

func (t *recordingTracer) EndSpan(_ SpanContext, _ map[string]any, _ error) { t.end++ }

func (t *recordingTracer) Shutdown() error {
	t.shut++
	return nil
}

func TestRuntimeInitializesAndUsesTracer(t *testing.T) {
	old := newTracer
	defer func() { newTracer = old }()

	var tracer *recordingTracer
	newTracer = func(_ OTELConfig) (Tracer, error) {
		tracer = &recordingTracer{}
		return tracer, nil
	}

	rt, err := New(context.Background(), Options{
		ProjectRoot: t.TempDir(),
		Model:       &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", Content: "ok"}}}},
		OTEL:        OTELConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	_, runErr := rt.Run(context.Background(), Request{Prompt: "hello"})
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if tracer == nil || tracer.agent == 0 || tracer.model == 0 || tracer.end == 0 || tracer.shut != 1 {
		t.Fatalf("unexpected tracer calls: %+v", tracer)
	}
}

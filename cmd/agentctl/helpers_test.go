package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/event"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	"github.com/cexll/agentsdk-go/pkg/tool"
	"github.com/cexll/agentsdk-go/pkg/workflow"
)

type fakeAgent struct {
	runFunc       func(context.Context, string) (*agent.RunResult, error)
	runStreamFunc func(context.Context, string) (<-chan event.Event, error)
	addToolFunc   func(tool.Tool) error
}

func (f *fakeAgent) Run(ctx context.Context, input string) (*agent.RunResult, error) {
	if f.runFunc == nil {
		return &agent.RunResult{}, nil
	}
	return f.runFunc(ctx, input)
}

func (f *fakeAgent) RunStream(ctx context.Context, input string) (<-chan event.Event, error) {
	if f.runStreamFunc == nil {
		ch := make(chan event.Event)
		close(ch)
		return ch, nil
	}
	return f.runStreamFunc(ctx, input)
}

func (f *fakeAgent) Resume(ctx context.Context, _ *event.Bookmark) (*agent.RunResult, error) {
	return f.Run(ctx, "")
}

func (f *fakeAgent) RunWorkflow(context.Context, *workflow.Graph, ...workflow.ExecutorOption) error {
	return nil
}

func (f *fakeAgent) AddTool(t tool.Tool) error {
	if f.addToolFunc == nil {
		return nil
	}
	return f.addToolFunc(t)
}

func (f *fakeAgent) Approve(string, bool) error { return nil }

func (f *fakeAgent) UseMiddleware(m middleware.Middleware) {}

func (f *fakeAgent) RemoveMiddleware(string) bool { return false }

func (f *fakeAgent) ListMiddlewares() []middleware.Middleware { return nil }

func (f *fakeAgent) WithHook(agent.Hook) agent.Agent { return f }

func (f *fakeAgent) Fork(...agent.ForkOption) (agent.Agent, error) { return f, nil }

func useAgentFactory(t *testing.T, stub agent.Agent) {
	t.Helper()
	original := agentFactory
	agentFactory = func(agent.Config, ...agent.Option) (agent.Agent, error) { return stub, nil }
	t.Cleanup(func() { agentFactory = original })
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForAddress(t *testing.T, buf *syncBuffer, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	const marker = "agentctl serve listening on http://"
	for time.Now().Before(deadline) {
		output := buf.String()
		idx := strings.LastIndex(output, marker)
		if idx >= 0 {
			start := idx + len(marker)
			end := strings.Index(output[start:], "\n")
			if end < 0 {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return strings.TrimSpace(output[start : start+end])
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server address not reported in time")
	return ""
}

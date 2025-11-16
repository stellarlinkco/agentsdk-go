package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/event"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func TestRunCommandPrintsMarkdown(t *testing.T) {
	stub := &fakeAgent{
		runFunc: func(ctx context.Context, input string) (*agent.RunResult, error) {
			return &agent.RunResult{
				Output:     "done",
				StopReason: "complete",
				Usage: agent.TokenUsage{
					InputTokens:  1,
					OutputTokens: 1,
					TotalTokens:  2,
				},
			}, nil
		},
	}
	useAgentFactory(t, stub)
	var out bytes.Buffer
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := runCommand(context.Background(), []string{"demo"}, cfgPath, ioStreams{out: &out, err: io.Discard}); err != nil {
		t.Fatalf("runCommand error: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "# agentctl run") {
		t.Fatalf("missing header: %s", output)
	}
	if !strings.Contains(output, "done") {
		t.Fatalf("missing output payload: %s", output)
	}
}

func TestRunCommandStreamMode(t *testing.T) {
	stub := &fakeAgent{
		runStreamFunc: func(ctx context.Context, input string) (<-chan event.Event, error) {
			ch := make(chan event.Event, 1)
			ch <- event.NewEvent(event.EventProgress, "demo", event.ProgressData{Stage: "start"})
			close(ch)
			return ch, nil
		},
	}
	useAgentFactory(t, stub)
	var out bytes.Buffer
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := runCommand(context.Background(), []string{"--stream", "hello"}, cfgPath, ioStreams{out: &out, err: io.Discard}); err != nil {
		t.Fatalf("runCommand stream error: %v", err)
	}
	if !strings.Contains(out.String(), "```json") {
		t.Fatalf("stream output missing json fence: %s", out.String())
	}
}

func TestRunCommandRegistersTools(t *testing.T) {
	var toolCount int
	stub := &fakeAgent{
		runFunc: func(ctx context.Context, input string) (*agent.RunResult, error) {
			return &agent.RunResult{}, nil
		},
		addToolFunc: func(tool tool.Tool) error {
			toolCount++
			return nil
		},
	}
	useAgentFactory(t, stub)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	err := runCommand(context.Background(), []string{"--tool", "bash", "task"}, cfgPath, ioStreams{out: io.Discard, err: io.Discard})
	if err != nil {
		t.Fatalf("runCommand tool error: %v", err)
	}
	if toolCount == 0 {
		t.Fatal("expected AddTool to be invoked")
	}
}

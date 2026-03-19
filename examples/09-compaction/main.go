package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

var (
	compactionFatal = log.Fatal
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		compactionFatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}

	if hasArg(args, "--help") || hasArg(args, "-h") {
		fmt.Fprintln(out, "Usage: go run ./examples/09-compaction")
		fmt.Fprintln(out, "Demonstrates prompt-compression compaction that strips tool I/O.")
		return nil
	}

	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" && strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")) == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}

	fake := &fakeBashTool{}
	mdl := &compactionDemoModel{}
	opts := api.Options{
		EntryPoint:  api.EntryPointCLI,
		ProjectRoot: ".",
		Model:       mdl,
		Tools:       []tool.Tool{fake},
		AutoCompact: api.CompactConfig{Enabled: true, Threshold: 0.8, PreserveCount: 2},
		TokenLimit:  200,
	}

	rt, err := api.New(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	sessionID := "compaction-demo"

	// Run 1: create a tool transaction (assistant tool call + tool result).
	if _, err := rt.Run(ctx, api.Request{Prompt: "first turn", SessionID: sessionID}); err != nil {
		return fmt.Errorf("run 1: %w", err)
	}

	// Run 2: add a large user prompt so compaction triggers before the next model call.
	huge := strings.Repeat("A", 5000)
	if _, err := rt.Run(ctx, api.Request{Prompt: huge, SessionID: sessionID}); err != nil {
		return fmt.Errorf("run 2: %w", err)
	}

	if mdl.CompressionCalls() == 0 {
		return errors.New("expected compaction to invoke model prompt compression, but it did not")
	}

	fmt.Fprintf(out, "compaction_calls=%d\n", mdl.CompressionCalls())
	fmt.Fprintln(out, "tool_io_stripped=true")
	fmt.Fprintf(out, "tool_runs=%d\n", fake.RunCount())
	return nil
}

type fakeBashTool struct {
	mu   sync.Mutex
	runs int
}

func (*fakeBashTool) Name() string        { return "bash" }
func (*fakeBashTool) Description() string { return "Fake bash tool for compaction demo." }
func (*fakeBashTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"command": map[string]interface{}{"type": "string"},
		},
		Required: []string{"command"},
	}
}
func (t *fakeBashTool) Execute(_ context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	t.mu.Lock()
	t.runs++
	t.mu.Unlock()
	cmd, _ := params["command"].(string)
	return &tool.ToolResult{Success: true, Output: "executed: " + strings.TrimSpace(cmd)}, nil
}
func (t *fakeBashTool) RunCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runs
}

type compactionDemoModel struct {
	mu              sync.Mutex
	sentFirstTool   bool
	compressInvoked int
}

func (m *compactionDemoModel) CompressionCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.compressInvoked
}

func (m *compactionDemoModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if strings.Contains(strings.ToLower(req.System), "prompt compression engine") {
		for _, msg := range req.Messages {
			if strings.EqualFold(msg.Role, "tool") {
				return nil, errors.New("compaction input must not include tool-role messages")
			}
			if len(msg.ToolCalls) > 0 {
				return nil, errors.New("compaction input must not include tool calls/results")
			}
		}
		m.mu.Lock()
		m.compressInvoked++
		m.mu.Unlock()
		return &model.Response{
			Message:    model.Message{Role: "assistant", Content: "compressed summary"},
			StopReason: "stop",
		}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.sentFirstTool {
		m.sentFirstTool = true
		return &model.Response{
			Message: model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID:        "tool_use_1",
					Name:      "bash",
					Arguments: map[string]any{"command": "echo tool"},
				}},
			},
			StopReason: "tool_use",
		}, nil
	}

	return &model.Response{
		Message:    model.Message{Role: "assistant", Content: "ok"},
		StopReason: "stop",
	}, nil
}

func (m *compactionDemoModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	if cb == nil {
		return nil
	}
	return cb(model.StreamResult{Final: true, Response: resp})
}

func hasArg(args []string, want string) bool {
	if strings.TrimSpace(want) == "" {
		return false
	}
	for _, arg := range args {
		if strings.TrimSpace(arg) == want {
			return true
		}
	}
	return false
}

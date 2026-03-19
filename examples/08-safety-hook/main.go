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
	"github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

var safetyHookFatal = log.Fatal

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		safetyHookFatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}

	if hasArg(args, "--help") || hasArg(args, "-h") {
		fmt.Fprintln(out, "Usage: go run ./examples/08-safety-hook")
		fmt.Fprintln(out, "Demonstrates the built-in PreToolUse safety hook and DisableSafetyHook.")
		return nil
	}

	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" && strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")) == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}

	command := "rm -rf /"

	blockedOut, blockedToolRuns, err := runOnce(ctx, false, command)
	if err != nil {
		return err
	}
	allowedOut, allowedToolRuns, err := runOnce(ctx, true, command)
	if err != nil {
		return err
	}

	if blockedToolRuns != 0 {
		return fmt.Errorf("expected tool not to execute with safety hook enabled, got %d runs", blockedToolRuns)
	}
	if !strings.Contains(blockedOut, hooks.ErrSafetyHookDenied.Error()) {
		return fmt.Errorf("expected safety hook denial in output: %q", blockedOut)
	}
	if allowedToolRuns != 1 {
		return fmt.Errorf("expected tool to execute once with safety hook disabled, got %d runs", allowedToolRuns)
	}
	if strings.Contains(allowedOut, hooks.ErrSafetyHookDenied.Error()) {
		return fmt.Errorf("unexpected safety hook denial when disabled: %q", allowedOut)
	}

	fmt.Fprintln(out, "Safety hook enabled:")
	fmt.Fprintln(out, blockedOut)
	fmt.Fprintln(out, "Safety hook disabled:")
	fmt.Fprintln(out, allowedOut)
	return nil
}

func runOnce(ctx context.Context, disableSafety bool, command string) (string, int, error) {
	fake := &fakeBashTool{}
	mdl := &toolCallThenReportModel{toolName: "bash", command: command}
	opts := api.Options{
		EntryPoint:        api.EntryPointCLI,
		ProjectRoot:       ".",
		Model:             mdl,
		Tools:             []tool.Tool{fake},
		DisableSafetyHook: disableSafety,
	}

	rt, err := api.New(ctx, opts)
	if err != nil {
		return "", 0, fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	resp, err := rt.Run(ctx, api.Request{
		Prompt:    "Trigger exactly one bash tool call, then report the tool result.",
		SessionID: fmt.Sprintf("safety-hook-%t", disableSafety),
	})
	if err != nil {
		return "", 0, fmt.Errorf("run: %w", err)
	}
	if resp == nil || resp.Result == nil {
		return "", 0, errors.New("run: missing result")
	}
	return strings.TrimSpace(resp.Result.Output), fake.RunCount(), nil
}

type fakeBashTool struct {
	mu   sync.Mutex
	runs int
}

func (*fakeBashTool) Name() string { return "bash" }
func (*fakeBashTool) Description() string {
	return "Fake bash tool for safety-hook demo. Does not execute system commands."
}
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

type toolCallThenReportModel struct {
	mu       sync.Mutex
	sentCall bool
	toolName string
	command  string
}

func (m *toolCallThenReportModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.sentCall {
		m.sentCall = true
		return &model.Response{
			Message: model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID:        "tool_use_1",
					Name:      m.toolName,
					Arguments: map[string]any{"command": m.command},
				}},
			},
			StopReason: "tool_use",
		}, nil
	}

	toolResult := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := req.Messages[i]
		if strings.EqualFold(msg.Role, "tool") && len(msg.ToolCalls) > 0 {
			toolResult = strings.TrimSpace(msg.ToolCalls[0].Result)
			break
		}
	}
	return &model.Response{
		Message:    model.Message{Role: "assistant", Content: "tool_result=" + toolResult},
		StopReason: "stop",
	}, nil
}

func (m *toolCallThenReportModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
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

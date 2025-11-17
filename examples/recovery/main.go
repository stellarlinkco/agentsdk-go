package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/session"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func main() {
	ctx := context.Background()
	sess, err := session.NewMemorySession("recovery-demo-session")
	if err != nil {
		log.Fatalf("create session: %v", err)
	}

	ag, err := agent.New(agent.Config{
		Name:            "recovery-demo",
		Description:     "Demonstrates tool timeout + watchdog recovery.",
		EnableRecovery:  true,
		ToolTimeout:     2 * time.Second,
		WatchdogTimeout: 10 * time.Second,
		DefaultContext: agent.RunContext{
			SessionID:     sess.ID(),
			MaxIterations: 2,
		},
	}, agent.WithSession(sess), agent.WithModel(&demoModel{}))
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	if err := ag.AddTool(slowTool{delay: 5 * time.Second}); err != nil {
		log.Fatalf("register tool: %v", err)
	}

	result, err := ag.Run(ctx, "演示崩溃自愈机制")
	if err != nil {
		log.Printf("agent run error: %v", err)
	}
	if result == nil {
		return
	}

	fmt.Printf("StopReason = %s\n", result.StopReason)
	for _, call := range result.ToolCalls {
		fmt.Printf("tool=%s duration=%s error=%q\n", call.Name, call.Duration, call.Error)
	}
	fmt.Printf("Recorded events: %d\n", len(result.Events))
}

type demoModel struct {
	step int
}

func (m *demoModel) Generate(ctx context.Context, _ []model.Message) (model.Message, error) {
	m.step++
	if m.step == 1 {
		return model.Message{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				{
					ID:        "slow-tool-1",
					Name:      "slow_tool",
					Arguments: map[string]any{},
				},
			},
		}, nil
	}
	return model.Message{
		Role:    "assistant",
		Content: "工具超时已记录，任务结束。",
	}, nil
}

func (m *demoModel) GenerateWithTools(ctx context.Context, messages []model.Message, tools []map[string]any) (model.Message, error) {
	return m.Generate(ctx, messages)
}

func (m *demoModel) GenerateStream(context.Context, []model.Message, model.StreamCallback) error {
	return fmt.Errorf("demo model does not stream")
}

type slowTool struct {
	delay time.Duration
}

func (slowTool) Name() string        { return "slow_tool" }
func (slowTool) Description() string { return "Sleeps for a while to trigger timeout." }
func (slowTool) Schema() *tool.JSONSchema {
	return nil
}

func (t slowTool) Execute(ctx context.Context, _ map[string]interface{}) (*tool.ToolResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(t.delay):
		return &tool.ToolResult{Success: true, Output: "slow work complete"}, nil
	}
}

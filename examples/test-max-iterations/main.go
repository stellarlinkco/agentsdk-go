package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/session"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

// endlessLoopTool 模拟一个总是返回"需要继续"的工具
type endlessLoopTool struct{}

func (t *endlessLoopTool) Name() string {
	return "endless_loop"
}

func (t *endlessLoopTool) Description() string {
	return "A tool that simulates endless loop scenario"
}

func (t *endlessLoopTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type:       "object",
		Properties: map[string]any{},
		Required:   []string{},
	}
}

func (t *endlessLoopTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	return &tool.ToolResult{
		Success: true,
		Output:  "Tool executed, please continue calling me again",
	}, nil
}

// mockEndlessModel 模拟一个总是返回工具调用的模型
type mockEndlessModel struct {
	callCount int
}

func (m *mockEndlessModel) Generate(ctx context.Context, messages []modelpkg.Message) (modelpkg.Message, error) {
	m.callCount++
	fmt.Printf("[Model Call %d] Returning tool call request\n", m.callCount)

	return modelpkg.Message{
		Role:    "assistant",
		Content: fmt.Sprintf("Iteration %d: I need to call the tool again", m.callCount),
		ToolCalls: []modelpkg.ToolCall{
			{
				ID:        fmt.Sprintf("call_%d", m.callCount),
				Name:      "endless_loop",
				Arguments: map[string]any{},
			},
		},
	}, nil
}

func (m *mockEndlessModel) GenerateWithTools(ctx context.Context, messages []modelpkg.Message, tools []map[string]any) (modelpkg.Message, error) {
	return m.Generate(ctx, messages)
}

func (m *mockEndlessModel) GenerateStream(ctx context.Context, messages []modelpkg.Message, cb modelpkg.StreamCallback) error {
	msg, err := m.Generate(ctx, messages)
	if err != nil {
		return err
	}
	return cb(modelpkg.StreamResult{Message: msg, Final: true})
}

func main() {
	fmt.Println("=== MaxIterations 防护测试 ===")
	fmt.Println()

	// 创建 mock 模型
	mockModel := &mockEndlessModel{}

	// 创建 session
	sess, err := session.NewMemorySession("test-max-iterations")
	if err != nil {
		log.Fatalf("创建 session 失败: %v", err)
	}
	defer sess.Close()

	// 创建 Agent
	ag, err := agent.New(
		agent.Config{
			Name:        "test-max-iterations-agent",
			Description: "测试 MaxIterations 防护",
			DefaultContext: agent.RunContext{
				SessionID:     sess.ID(),
				MaxIterations: 3, // 仅允许 3 次迭代
			},
		},
		agent.WithModel(mockModel),
		agent.WithSession(sess),
	)
	if err != nil {
		log.Fatalf("创建 agent 失败: %v", err)
	}

	// 注册无限循环工具
	if err := ag.AddTool(&endlessLoopTool{}); err != nil {
		log.Fatalf("注册工具失败: %v", err)
	}

	fmt.Println("配置:")
	fmt.Println("- MaxIterations: 3")
	fmt.Println("- 模拟场景: 模型每次都返回工具调用")
	fmt.Println("- 预期行为: 达到 3 次迭代后自动停止")
	fmt.Println()

	// 运行测试
	fmt.Println("开始运行...")
	fmt.Println()
	result, err := ag.Run(context.Background(), "请开始无限循环测试")
	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}

	// 输出结果
	fmt.Println("\n=== 测试结果 ===")
	fmt.Printf("StopReason: %s\n", result.StopReason)
	fmt.Printf("实际迭代次数: %d\n", mockModel.callCount)
	fmt.Printf("工具调用次数: %d\n", len(result.ToolCalls))
	fmt.Printf("最终输出: %s\n", result.Output)

	// 验证
	fmt.Println("\n=== 验证 ===")
	if result.StopReason == "max_iterations" {
		fmt.Println("✅ PASS: 正确触发 MaxIterations 停止")
	} else {
		fmt.Printf("❌ FAIL: 预期 StopReason='max_iterations'，实际='%s'\n", result.StopReason)
	}

	if mockModel.callCount == 3 {
		fmt.Println("✅ PASS: 迭代次数符合预期（3 次）")
	} else {
		fmt.Printf("❌ FAIL: 预期迭代 3 次，实际 %d 次\n", mockModel.callCount)
	}

	if len(result.ToolCalls) == 3 {
		fmt.Println("✅ PASS: 工具调用次数符合预期（3 次）")
	} else {
		fmt.Printf("⚠️  WARN: 预期工具调用 3 次，实际 %d 次\n", len(result.ToolCalls))
	}
}

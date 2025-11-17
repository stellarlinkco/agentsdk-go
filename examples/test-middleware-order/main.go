package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/session"
)

// loggingMiddleware 记录执行顺序的中间件
type loggingMiddleware struct {
	*middleware.BaseMiddleware
	label string
	order *[]string
}

func newLoggingMiddleware(label string, priority int, order *[]string) *loggingMiddleware {
	return &loggingMiddleware{
		BaseMiddleware: middleware.NewBaseMiddleware(label, priority),
		label:          label,
		order:          order,
	}
}

func (m *loggingMiddleware) ExecuteModelCall(ctx context.Context, req *middleware.ModelRequest, next middleware.ModelCallFunc) (*middleware.ModelResponse, error) {
	*m.order = append(*m.order, fmt.Sprintf("%s-pre", m.label))
	fmt.Printf("[%s] 模型调用前处理 (priority=%d)\n", m.label, m.Priority())

	resp, err := next(ctx, req)

	*m.order = append(*m.order, fmt.Sprintf("%s-post", m.label))
	fmt.Printf("[%s] 模型调用后处理\n", m.label)

	return resp, err
}

func (m *loggingMiddleware) ExecuteToolCall(ctx context.Context, req *middleware.ToolCallRequest, next middleware.ToolCallFunc) (*middleware.ToolCallResponse, error) {
	*m.order = append(*m.order, fmt.Sprintf("%s-tool-pre", m.label))
	fmt.Printf("[%s] 工具调用前处理 (priority=%d)\n", m.label, m.Priority())

	resp, err := next(ctx, req)

	*m.order = append(*m.order, fmt.Sprintf("%s-tool-post", m.label))
	fmt.Printf("[%s] 工具调用后处理\n", m.label)

	return resp, err
}

// mockSimpleModel 简单的 mock 模型
type mockSimpleModel struct{}

func (m *mockSimpleModel) Generate(ctx context.Context, messages []modelpkg.Message) (modelpkg.Message, error) {
	return modelpkg.Message{
		Role:    "assistant",
		Content: "Hello from mock model",
	}, nil
}

func (m *mockSimpleModel) GenerateWithTools(ctx context.Context, messages []modelpkg.Message, tools []map[string]any) (modelpkg.Message, error) {
	return m.Generate(ctx, messages)
}

func (m *mockSimpleModel) GenerateStream(ctx context.Context, messages []modelpkg.Message, cb modelpkg.StreamCallback) error {
	msg, err := m.Generate(ctx, messages)
	if err != nil {
		return err
	}
	return cb(modelpkg.StreamResult{Message: msg, Final: true})
}

func main() {
	fmt.Println("=== 中间件优先级与执行顺序测试 ===")
	fmt.Println()

	// 创建 session
	sess, err := session.NewMemorySession("test-middleware-order")
	if err != nil {
		log.Fatalf("创建 session 失败: %v", err)
	}
	defer sess.Close()

	// 创建 Agent
	ag, err := agent.New(
		agent.Config{
			Name:        "test-middleware-order-agent",
			Description: "测试中间件优先级和执行顺序",
			DefaultContext: agent.RunContext{
				SessionID:     sess.ID(),
				MaxIterations: 1,
			},
		},
		agent.WithModel(&mockSimpleModel{}),
		agent.WithSession(sess),
	)
	if err != nil {
		log.Fatalf("创建 agent 失败: %v", err)
	}

	// 记录执行顺序
	var order []string

	// 注册三个不同优先级的中间件（乱序注册）
	fmt.Println("注册中间件（乱序）:")
	fmt.Println("- Low (priority=10)")
	fmt.Println("- High (priority=90)")
	fmt.Println("- Medium (priority=50)")
	fmt.Println()

	ag.UseMiddleware(newLoggingMiddleware("Low", 10, &order))
	ag.UseMiddleware(newLoggingMiddleware("High", 90, &order))
	ag.UseMiddleware(newLoggingMiddleware("Medium", 50, &order))

	// 列出中间件
	fmt.Println("注册后的中间件列表（按执行顺序）:")
	for i, mw := range ag.ListMiddlewares() {
		fmt.Printf("%d. %s (priority=%d)\n", i+1, mw.Name(), mw.Priority())
	}
	fmt.Println()

	// 运行测试
	fmt.Println("开始运行...")
	fmt.Println()
	fmt.Println("--- 模型调用流程 ---")
	result, err := ag.Run(context.Background(), "测试消息")
	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}

	fmt.Printf("\n结果: %s\n", result.Output)

	// 验证执行顺序
	fmt.Println("\n=== 验证执行顺序 ===")
	fmt.Println("实际执行顺序:")
	for i, step := range order {
		fmt.Printf("%d. %s\n", i+1, step)
	}

	expectedOrder := []string{
		"High-pre",    // 高优先级先执行（外层）
		"Medium-pre",  // 中优先级（中层）
		"Low-pre",     // 低优先级（内层）
		"Low-post",    // 低优先级后处理（内层）
		"Medium-post", // 中优先级后处理（中层）
		"High-post",   // 高优先级后处理（外层）
	}

	fmt.Println("\n预期执行顺序:")
	for i, step := range expectedOrder {
		fmt.Printf("%d. %s\n", i+1, step)
	}

	// 比较
	fmt.Println("\n=== 测试结果 ===")
	if len(order) == len(expectedOrder) {
		allMatch := true
		for i := range order {
			if order[i] != expectedOrder[i] {
				allMatch = false
				fmt.Printf("❌ FAIL: 位置 %d 不匹配：预期 '%s'，实际 '%s'\n", i+1, expectedOrder[i], order[i])
			}
		}
		if allMatch {
			fmt.Println("✅ PASS: 执行顺序完全符合预期！")
			fmt.Println("✅ PASS: 洋葱模型正确实现（高优先级在外层）")
		}
	} else {
		fmt.Printf("❌ FAIL: 执行步骤数量不匹配：预期 %d，实际 %d\n", len(expectedOrder), len(order))
	}
}

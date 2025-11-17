package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/approval"
	"github.com/cexll/agentsdk-go/pkg/workflow"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== agentsdk-go v0.3.1 功能演示 ===")
	fmt.Println()

	// 演示 1: 审批白名单
	demoApprovalWhitelist()

	// 演示 2: 工作流图
	demoWorkflowGraph(ctx)

	// 演示 3: Agent Fork
	demoAgentFork(ctx)

	// 演示 4: 审批记录日志
	demoApprovalRecordLog()

	fmt.Println()
	fmt.Println("=== 所有演示完成 ✅ ===")
}

// 演示 1: 审批白名单
func demoApprovalWhitelist() {
	fmt.Println("--- 演示 1: 审批白名单 ---")

	wl := approval.NewWhitelist()

	// 添加到白名单
	params := map[string]interface{}{"command": "echo test"}
	wl.Add("session-1", "bash_execute", params, time.Now())
	fmt.Println("✅ 工具已加入白名单")

	// 检查是否在白名单
	if wl.Allowed("session-1", "bash_execute", params) {
		fmt.Println("✅ 工具在白名单中,无需重复审批")
	}

	// 导出快照
	snapshot := wl.Snapshot()
	fmt.Printf("✅ 白名单快照: %d 个条目\n\n", len(snapshot))
}

// 演示 2: 工作流图
func demoWorkflowGraph(ctx context.Context) {
	fmt.Println("--- 演示 2: 工作流图 ---")

	// 创建工作流图
	graph := workflow.NewGraph()
	fmt.Println("✅ 工作流图已创建")

	// 添加节点
	startNode := workflow.NewAction("start", func(ec *workflow.ExecutionContext) error {
		fmt.Println("  -> 执行: start 节点")
		return nil
	})

	endNode := workflow.NewAction("end", func(ec *workflow.ExecutionContext) error {
		fmt.Println("  -> 执行: end 节点")
		return nil
	})

	if err := graph.AddNode(startNode); err != nil {
		log.Fatalf("add start node: %v", err)
	}
	if err := graph.AddNode(endNode); err != nil {
		log.Fatalf("add end node: %v", err)
	}
	if err := graph.SetStart("start"); err != nil {
		log.Fatalf("set start node: %v", err)
	}
	fmt.Println("✅ 已添加 2 个节点")

	// 添加边
	if err := graph.AddTransition("start", "end", workflow.Always()); err != nil {
		log.Fatalf("add transition: %v", err)
	}
	fmt.Println("✅ 已添加边: start -> end")

	// 执行工作流
	executor := workflow.NewExecutor(graph)
	if err := executor.Run(ctx); err != nil {
		log.Fatalf("execute workflow: %v", err)
	}

	fmt.Println("✅ 工作流执行完成")
	fmt.Println()
}

// 演示 3: Agent Fork
func demoAgentFork(ctx context.Context) {
	fmt.Println("--- 演示 3: Agent Fork (子代理) ---")

	// 创建主 Agent
	mainAgent, err := agent.New(agent.Config{
		Name:        "main-agent",
		Description: "Main agent",
		DefaultContext: agent.RunContext{
			SessionID: "main-session",
		},
	})
	if err != nil {
		log.Fatalf("create main agent: %v", err)
	}
	fmt.Println("✅ 主 Agent 已创建")

	// Fork 子 Agent
	_, err = mainAgent.Fork()
	if err != nil {
		log.Fatalf("fork agent: %v", err)
	}
	fmt.Println("✅ 子 Agent 已 Fork 成功")
	fmt.Println()
}

// 演示 4: 审批记录日志
func demoApprovalRecordLog() {
	fmt.Println("--- 演示 4: 审批记录日志 ---")

	// 创建记录日志
	recordLog, err := approval.NewRecordLog("/tmp/approval-demo")
	if err != nil {
		log.Fatalf("create record log: %v", err)
	}
	defer recordLog.Close()

	fmt.Println("✅ 审批记录日志已创建")

	// 配置 GC
	recordLog.ConfigureGC(
		approval.WithRetentionDays(7),
		approval.WithRetentionCount(1000),
	)
	fmt.Println("✅ GC 已配置 (保留 7 天, 1000 条)")

	// 获取状态
	status := recordLog.GCStatus()
	fmt.Printf("✅ 当前状态: 最近运行后 %d 条记录, %d bytes\n", status.Last.AfterCount, status.Last.AfterBytes)
}

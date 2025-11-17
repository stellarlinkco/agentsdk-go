package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/approval"
	"github.com/cexll/agentsdk-go/pkg/workflow"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== agentsdk-go v0.3.1 功能测试 ===")
	fmt.Println()

	// 测试 1: 审批系统
	testApprovalSystem()

	// 测试 2: StateGraph 工作流
	testStateGraphWorkflow(ctx)

	// 测试 3: Team 多代理协作
	testTeamCollaboration(ctx)

	// 测试 4: 审批队列 GC
	testApprovalGC()

	fmt.Println()
	fmt.Println("=== 所有 v0.3.1 功能测试完成 ✅ ===")
}

// 测试 1: 审批系统
func testApprovalSystem() {
	fmt.Println("--- 测试 1: 审批系统 ---")

	wl := approval.NewWhitelist()
	queue := approval.NewQueue(nil, wl)
	params := map[string]interface{}{"command": "echo test"}

	rec, autoApproved, err := queue.Request("test-session", "bash_execute", params)
	if err != nil {
		log.Fatalf("request approval: %v", err)
	}
	if autoApproved {
		fmt.Println("✅ 工具已在白名单中,自动放行")
		fmt.Println()
		return
	}
	fmt.Println("✅ 审批请求已提交")

	if _, err := queue.Approve(rec.ID, "测试命令执行"); err != nil {
		log.Fatalf("approve: %v", err)
	}
	fmt.Println("✅ 审批请求已批准")

	if wl.Allowed("test-session", "bash_execute", params) {
		fmt.Println("✅ 工具已加入会话白名单")
	}
	fmt.Println()
}

// 测试 2: StateGraph 工作流
func testStateGraphWorkflow(ctx context.Context) {
	fmt.Println("--- 测试 2: StateGraph 工作流 ---")

	// 创建状态图
	graph := workflow.NewGraph()
	fmt.Println("✅ 状态图已创建")

	// 添加节点
	startNode := workflow.NewAction("start", func(execCtx *workflow.ExecutionContext) error {
		fmt.Println("  -> 执行节点: start")
		execCtx.Set("step", "start")
		return nil
	})

	processNode := workflow.NewAction("process", func(execCtx *workflow.ExecutionContext) error {
		fmt.Println("  -> 执行节点: process")
		execCtx.Set("step", "process")
		return nil
	})

	if err := graph.AddNode(startNode); err != nil {
		log.Fatalf("add start node: %v", err)
	}
	if err := graph.AddNode(processNode); err != nil {
		log.Fatalf("add process node: %v", err)
	}
	if err := graph.SetStart("start"); err != nil {
		log.Fatalf("set start node: %v", err)
	}

	// 添加边
	if err := graph.AddTransition("start", "process", workflow.Always()); err != nil {
		log.Fatalf("add transition: %v", err)
	}
	fmt.Println("✅ 已添加转换: start -> process")

	// 执行工作流
	executor := workflow.NewExecutor(graph)
	if err := executor.Run(ctx); err != nil {
		log.Fatalf("execute workflow: %v", err)
	}

	fmt.Println("✅ 工作流执行完成")
	fmt.Println()
}

// 测试 3: Team 多代理协作
func testTeamCollaboration(ctx context.Context) {
	fmt.Println("--- 测试 3: Team 多代理协作 ---")

	// 创建 Leader Agent
	leader, err := agent.New(agent.Config{
		Name:        "leader",
		Description: "Team leader",
		DefaultContext: agent.RunContext{
			SessionID: "team-session",
		},
	})
	if err != nil {
		log.Fatalf("create leader: %v", err)
	}
	fmt.Println("✅ Leader Agent 已创建")

	// 创建 Worker Agents
	worker1, err := agent.New(agent.Config{
		Name:        "worker-1",
		Description: "Worker 1",
		DefaultContext: agent.RunContext{
			SessionID: "worker-1-session",
		},
	})
	if err != nil {
		log.Fatalf("create worker1: %v", err)
	}

	worker2, err := agent.New(agent.Config{
		Name:        "worker-2",
		Description: "Worker 2",
		DefaultContext: agent.RunContext{
			SessionID: "worker-2-session",
		},
	})
	if err != nil {
		log.Fatalf("create worker2: %v", err)
	}
	fmt.Println("✅ 2 个 Worker Agent 已创建")

	// 创建 Team
	team, err := agent.NewTeamAgent(agent.TeamConfig{
		Name:        "demo-team",
		Strategy:    agent.StrategyRoundRobin,
		DefaultMode: agent.CollaborationSequential,
		Members: []agent.TeamMemberConfig{
			{Name: "leader", Role: agent.TeamRoleLeader, Agent: leader},
			{Name: "worker-1", Role: agent.TeamRoleWorker, Agent: worker1},
			{Name: "worker-2", Role: agent.TeamRoleWorker, Agent: worker2},
		},
	})
	if err != nil {
		log.Fatalf("create team: %v", err)
	}
	fmt.Println("✅ Team 已创建 (Sequential + RoundRobin)")

	// 测试 Team Run
	result, err := team.Run(ctx, "team task")
	if err != nil {
		log.Fatalf("team run: %v", err)
	}
	fmt.Printf("✅ Team 执行完成: %s\n\n", result.StopReason)
}

// 测试 4: 审批队列 GC
func testApprovalGC() {
	fmt.Println("--- 测试 4: 审批队列 GC ---")

	// 创建临时审批记录日志
	recordLog, err := approval.NewRecordLog("/tmp/approval-gc-test")
	if err != nil {
		log.Fatalf("create record log: %v", err)
	}
	defer recordLog.Close()

	// 配置 GC
	recordLog.ConfigureGC(
		approval.WithRetentionDays(7),
		approval.WithRetentionCount(100),
	)
	fmt.Println("✅ GC 配置完成 (保留 7 天, 100 条记录)")

	fmt.Println("✅ 手动 GC 可通过 recordLog.RunGC() 触发")

	// 获取 GC 状态
	status := recordLog.GCStatus()
	fmt.Printf("✅ GC 状态: 运行次数=%d, 删除记录数=%d\n", status.Runs, status.TotalDropped)
	fmt.Println()
}

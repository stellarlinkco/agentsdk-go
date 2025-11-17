package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/memory"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	"github.com/cexll/agentsdk-go/pkg/model"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

func main() {
	ctx := context.Background()
	workDir := filepath.Join(os.TempDir(), "agentsdk-memory-example")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		log.Fatalf("ensure workdir: %v", err)
	}

	agentMemStore := memory.NewFileAgentMemoryStore(workDir)
	workingMemStore := memory.NewFileWorkingMemoryStore(workDir)

	const persona = "# Agent 配置\n\n我是一个记忆演示 Agent，负责展示工作记忆注入效果。"
	if err := agentMemStore.Write(ctx, persona); err != nil {
		log.Fatalf("write agent memory: %v", err)
	}

	updateTool := toolbuiltin.NewUpdateWorkingMemoryTool(workingMemStore)
	if _, err := updateTool.Execute(ctx, map[string]any{
		"thread_id": "memory-demo-thread",
		"data": map[string]any{
			"progress": "初始化阶段",
			"tasks":    []string{"创建三层记忆", "验证工具链"},
		},
		"ttl_seconds": float64((30 * time.Minute) / time.Second),
	}); err != nil {
		log.Fatalf("seed working memory: %v", err)
	}

	ag, err := agent.New(agent.Config{
		Name: "memory-demo-agent",
		DefaultContext: agent.RunContext{
			SessionID:     "memory-demo-thread",
			WorkDir:       workDir,
			MaxIterations: 1,
		},
	}, agent.WithModel(&printModel{}))
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	ag.UseMiddleware(middleware.NewAgentMemoryMiddleware(agentMemStore))
	ag.UseMiddleware(middleware.NewWorkingMemoryMiddleware(workingMemStore))

	if err := ag.AddTool(updateTool); err != nil {
		log.Fatalf("register working memory tool: %v", err)
	}

	result, err := ag.Run(ctx, "请汇总当前任务进度")
	if err != nil {
		log.Fatalf("agent run: %v", err)
	}

	fmt.Println("---- 模型输出 ----")
	fmt.Println(result.Output)
}

// printModel prints every prompt it receives to illustrate memory injection.
type printModel struct{}

func (printModel) Generate(_ context.Context, messages []model.Message) (model.Message, error) {
	fmt.Println("==== LLM 输入开始 ====")
	for idx, msg := range messages {
		fmt.Printf("[%d] %s\n%s\n\n", idx, msg.Role, msg.Content)
	}
	fmt.Println("==== LLM 输入结束 ====")
	return model.Message{Role: "assistant", Content: "三层记忆注入演示完成"}, nil
}

func (m printModel) GenerateStream(ctx context.Context, messages []model.Message, cb model.StreamCallback) error {
	msg, err := m.Generate(ctx, messages)
	if err != nil {
		return err
	}
	if cb != nil {
		return cb(model.StreamResult{Message: msg, Final: true})
	}
	return nil
}

// Ensure printModel satisfies ModelWithTools when tool schemas are provided.
func (m printModel) GenerateWithTools(ctx context.Context, messages []model.Message, _ []map[string]any) (model.Message, error) {
	return m.Generate(ctx, messages)
}

var _ interface {
	model.Model
	model.ModelWithTools
} = (*printModel)(nil)

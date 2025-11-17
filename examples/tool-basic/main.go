package main // tool + non-streaming example: Bash/File tools + agent.Run

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

const (
	defaultModel    = "claude-3-5-sonnet-20241022"
	toolBasicPrompt = "请执行 'ls -la' 并读取 go.mod 文件的前 5 行"
)

func main() {
	ctx := context.Background()
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}
	if model, err := newAnthropicModel(ctx, apiKey); err != nil {
		log.Fatalf("create anthropic model: %v", err)
	} else {
		log.Printf("Anthropic model ready: %T (%s)", model, defaultModel)
	}

	ag, err := agent.New(agent.Config{
		Name:        "tool-basic-agent",
		Description: "Bash+File tools via non-streaming agent.Run.",
		DefaultContext: agent.RunContext{
			SessionID: "tool-basic-session", WorkDir: ".", MaxIterations: 4,
		},
	})
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	if err := ag.AddTool(toolbuiltin.NewBashTool()); err != nil {
		log.Fatalf("add bash tool: %v", err)
	}
	if err := ag.AddTool(toolbuiltin.NewFileTool()); err != nil {
		log.Fatalf("add file tool: %v", err)
	}

	log.Println("---- Tool + Non-Streaming Run ----")
	result, err := ag.Run(ctx, toolBasicPrompt)
	if err != nil {
		log.Fatalf("agent run: %v", err)
	}

	fmt.Println("---- Final Answer ----")
	fmt.Println(result.Output)
	fmt.Println("---- Token Usage ----")
	fmt.Printf("input=%d output=%d total=%d cache=%d\n",
		result.Usage.InputTokens,
		result.Usage.OutputTokens,
		result.Usage.TotalTokens,
		result.Usage.CacheTokens,
	)
}

// Anthropic wiring now reuses the official SDK wrapper to minimize boilerplate.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

package main // tool + streaming example: observe tool progress via RunStream

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

const (
	defaultModel     = "claude-3-5-sonnet-20241022"
	toolStreamPrompt = "执行 echo test 并读取 go.mod 文件"
)

func main() {
	ctx := context.Background()
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}

	// Reuse Anthropic wiring to keep parity with other examples.
	if model, err := newAnthropicModel(ctx, apiKey); err != nil {
		log.Fatalf("create anthropic model: %v", err)
	} else {
		log.Printf("Anthropic model ready: %T (%s)", model, defaultModel)
	}

	ag, err := agent.New(agent.Config{
		Name:        "tool-stream-agent",
		Description: "Bash+File tools with streaming visibility.",
		DefaultContext: agent.RunContext{
			SessionID:     "tool-stream-session",
			WorkDir:       ".",
			MaxIterations: 4,
		},
	})
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	// Register builtin tools so the agent can execute commands and read files.
	if err := ag.AddTool(toolbuiltin.NewBashTool()); err != nil {
		log.Fatalf("add bash tool: %v", err)
	}
	if err := ag.AddTool(toolbuiltin.NewFileTool()); err != nil {
		log.Fatalf("add file tool: %v", err)
	}

	log.Println("---- Tool + Streaming Run ----")
	events, err := ag.RunStream(ctx, toolStreamPrompt)
	if err != nil {
		log.Fatalf("run stream: %v", err)
	}

	// Inspect each streaming event to observe tool execution progress.
	for evt := range events {
		log.Printf("type=%s data=%v", evt.Type, evt.Data)
	}
}

// newAnthropicModel now wraps the official SDK instead of reimplementing providers.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

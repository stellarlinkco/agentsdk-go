package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
)

const (
	defaultModel    = "claude-3-5-sonnet-20241022"
	streamingPrompt = "请以 3 句话解释 agentsdk-go 的 streaming 事件。"
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
		Name:           "simple-stream-agent",
		Description:    "Demonstrates agent.RunStream without tools.",
		DefaultContext: agent.RunContext{SessionID: "simple-stream-session", WorkDir: ".", MaxIterations: 1},
	})
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	log.Println("---- RunStream streaming example ----")
	events, err := ag.RunStream(ctx, streamingPrompt)
	if err != nil {
		log.Fatalf("run stream: %v", err)
	}

	// Iterate over the events channel to print streaming progress.
	for evt := range events {
		log.Printf("type=%s payload=%v", evt.Type, evt.Data)
	}
}

// Reuses the helper from examples/basic to keep Anthropic wiring identical.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

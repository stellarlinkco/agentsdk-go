package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	"github.com/cexll/agentsdk-go/pkg/session"
)

const (
	defaultModel = "claude-3-5-sonnet-20241022"
	demoPrompt   = "请围绕 agentsdk-go 的核心特性与架构进行长篇讨论，覆盖上下文管理、工具调用、安全审计与扩展能力。"
)

func main() {
	ctx := context.Background()
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}

	model, err := newAnthropicModel(ctx, apiKey)
	if err != nil {
		log.Fatalf("create anthropic model: %v", err)
	}

	sess, err := session.NewMemorySession("middleware-demo-session")
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	defer sess.Close()

	ag, err := agent.New(
		agent.Config{
			Name:        "middleware-demo-agent",
			Description: "Demonstrates onion middleware with automatic summarization.",
			DefaultContext: agent.RunContext{
				SessionID:     sess.ID(),
				MaxIterations: 2,
			},
		},
		agent.WithModel(model),
		agent.WithSession(sess),
	)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	summaryMW := middleware.NewSummarizationMiddleware(120, 4)
	ag.UseMiddleware(summaryMW)

	fmt.Println("---- Registered Middlewares ----")
	for _, mw := range ag.ListMiddlewares() {
		fmt.Printf("- %s (priority=%d)\n", mw.Name(), mw.Priority())
	}

	fmt.Println("---- Running long-form prompt (will trigger summarization due to low threshold) ----")
	result, err := ag.Run(ctx, demoPrompt)
	if err != nil {
		log.Fatalf("agent run: %v", err)
	}

	fmt.Println("---- Agent Output ----")
	fmt.Println(result.Output)
}

func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	return anthropic.NewSDKModel(apiKey, defaultModel, 2048), nil
}

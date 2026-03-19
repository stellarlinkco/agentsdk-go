// Package main demonstrates reasoning_content passthrough for thinking models.
// Requires DEEPSEEK_API_KEY.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

var (
	reasoningFatal        = log.Fatal
	reasoningOnlineModel  = createOnlineModel
	reasoningNewOpenAI    = model.NewOpenAI
	reasoningNewAnthropic = model.NewAnthropic
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		reasoningFatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	provider := parseProvider(args)

	apiKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("DEEPSEEK_API_KEY is required")
	}
	mdl, err := reasoningOnlineModel(apiKey, provider)
	if err != nil {
		return err
	}

	// Demo 1: Non-streaming.
	resp, err := mdl.Complete(ctx, model.Request{
		Messages: []model.Message{{Role: "user", Content: "What is 15 * 37? Think step by step."}},
	})
	if err != nil {
		return fmt.Errorf("Complete: %w", err)
	}
	printResponse(resp)

	// Demo 2: Streaming.
	var streamResp *model.Response
	err = mdl.CompleteStream(ctx, model.Request{
		Messages: []model.Message{{Role: "user", Content: "What is 23 + 89? Think step by step."}},
	}, func(sr model.StreamResult) error {
		if sr.Final && sr.Response != nil {
			streamResp = sr.Response
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("CompleteStream: %w", err)
	}
	if streamResp != nil {
		_ = streamResp.Message.ReasoningContent
	}

	return nil
}

func parseProvider(args []string) string {
	provider := "openai"
	for _, arg := range args {
		if arg == "--provider" || arg == "-p" {
			continue
		}
		if arg == "anthropic" || arg == "--provider=anthropic" || arg == "-p=anthropic" {
			provider = "anthropic"
		}
	}
	for i, arg := range args {
		if (arg == "--provider" || arg == "-p") && i+1 < len(args) {
			provider = args[i+1]
		}
	}
	return provider
}

func printResponse(resp *model.Response) {
	if resp == nil {
		return
	}
	_ = resp.Message.Content
	_ = resp.Message.ReasoningContent
}

func createOnlineModel(apiKey, provider string) (model.Model, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("online model: api key required")
	}
	switch provider {
	case "anthropic":
		mdl, err := reasoningNewAnthropic(model.AnthropicConfig{
			APIKey:    apiKey,
			BaseURL:   "https://api.deepseek.com/anthropic",
			Model:     "deepseek-reasoner",
			MaxTokens: 4096,
		})
		if err != nil {
			return nil, fmt.Errorf("create anthropic model: %w", err)
		}
		return mdl, nil
	default:
		mdl, err := reasoningNewOpenAI(model.OpenAIConfig{
			APIKey:    apiKey,
			BaseURL:   "https://api.deepseek.com",
			Model:     "deepseek-reasoner",
			MaxTokens: 4096,
		})
		if err != nil {
			return nil, fmt.Errorf("create openai model: %w", err)
		}
		return mdl, nil
	}
}

// Package main demonstrates reasoning_content passthrough for thinking models.
// Offline-safe by default; pass --online to call DeepSeek via OpenAI/Anthropic APIs.
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
	online := false
	for _, arg := range args {
		if strings.TrimSpace(arg) == "--online" {
			online = true
		}
	}

	provider := parseProvider(args)

	var mdl model.Model
	if online {
		apiKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
		if apiKey == "" {
			return fmt.Errorf("--online requires DEEPSEEK_API_KEY")
		}
		var err error
		mdl, err = reasoningOnlineModel(apiKey, provider)
		if err != nil {
			return err
		}
	} else {
		mdl = offlineReasoningModel{}
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

type offlineReasoningModel struct{}

func (offlineReasoningModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.TrimSpace(req.Messages[i].Role) == "user" {
			last = strings.TrimSpace(req.Messages[i].TextContent())
			break
		}
	}
	return &model.Response{
		Message: model.Message{
			Role:             "assistant",
			Content:          "offline: " + last,
			ReasoningContent: "offline reasoning",
		},
		StopReason: "stop",
		Usage:      model.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	}, nil
}

func (m offlineReasoningModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	if cb == nil {
		return nil
	}
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	_ = cb(model.StreamResult{Delta: "offline", Final: false})
	return cb(model.StreamResult{Final: true, Response: resp})
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

// Package main demonstrates multi-model support with subagent-level model binding.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

var (
	multimodelFatal                 = log.Fatal
	multimodelAnthropicModelFactory = func(ctx context.Context, apiKey, modelName string) (modelpkg.Model, error) {
		return (&modelpkg.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: modelName,
		}).Model(ctx)
	}
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		multimodelFatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	opts, err := buildOptions(ctx, args)
	if err != nil {
		return err
	}

	rt, err := api.New(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	resp, err := rt.Run(ctx, api.Request{
		Prompt:    "List the files in the current directory.",
		SessionID: "multimodel-demo",
	})
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	if resp != nil && resp.Result != nil {
		fmt.Println(resp.Result.Output)
	}

	resp2, err := rt.Run(ctx, api.Request{
		Prompt:    "What is 2+2?",
		SessionID: "multimodel-demo-override",
		Model:     api.ModelTierLow,
	})
	if err != nil {
		return fmt.Errorf("run override: %w", err)
	}
	if resp2 != nil && resp2.Result != nil {
		fmt.Println(resp2.Result.Output)
	}
	return nil
}

func buildOptions(ctx context.Context, args []string) (api.Options, error) {
	_ = args
	opts := api.Options{
		ProjectRoot: ".",
		SubagentModelMapping: map[string]api.ModelTier{
			"plan":            api.ModelTierHigh,
			"explore":         api.ModelTierMid,
			"general-purpose": api.ModelTierMid,
		},
		MaxIterations: 5,
		Timeout:       10 * time.Second,
	}

	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return api.Options{}, fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}

	haiku, err := multimodelAnthropicModelFactory(ctx, apiKey, "claude-3-5-haiku-20241022")
	if err != nil {
		return api.Options{}, fmt.Errorf("create haiku model: %w", err)
	}
	sonnet, err := multimodelAnthropicModelFactory(ctx, apiKey, "claude-sonnet-4-20250514")
	if err != nil {
		return api.Options{}, fmt.Errorf("create sonnet model: %w", err)
	}
	opus, err := multimodelAnthropicModelFactory(ctx, apiKey, "claude-sonnet-4-20250514")
	if err != nil {
		return api.Options{}, fmt.Errorf("create opus model: %w", err)
	}

	opts.Model = sonnet
	opts.ModelPool = map[api.ModelTier]modelpkg.Model{
		api.ModelTierLow:  haiku,
		api.ModelTierMid:  sonnet,
		api.ModelTierHigh: opus,
	}

	return opts, nil
}

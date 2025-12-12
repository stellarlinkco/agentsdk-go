// Package main demonstrates multi-model support with tool-level model binding.
// This example shows how to configure different models for different tools
// to optimize costs (e.g., use cheaper models for simple tasks).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cexll/agentsdk-go/pkg/api"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_AUTH_TOKEN")
	}
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create model providers for different tiers.
	// In production, you would typically use:
	// - low:  claude-3-5-haiku (fast, cheap)
	// - mid:  claude-sonnet-4 (balanced)
	// - high: claude-opus-4 (powerful, expensive)
	haikuProvider := &modelpkg.AnthropicProvider{
		APIKey:    apiKey,
		ModelName: "claude-3-5-haiku-20241022",
	}
	sonnetProvider := &modelpkg.AnthropicProvider{
		APIKey:    apiKey,
		ModelName: "claude-sonnet-4-20250514",
	}
	// Using sonnet as a placeholder for high tier in this example.
	opusProvider := &modelpkg.AnthropicProvider{
		APIKey:    apiKey,
		ModelName: "claude-sonnet-4-20250514",
	}

	haiku, err := haikuProvider.Model(ctx)
	if err != nil {
		log.Fatalf("failed to create haiku model: %v", err)
	}
	sonnet, err := sonnetProvider.Model(ctx)
	if err != nil {
		log.Fatalf("failed to create sonnet model: %v", err)
	}
	opus, err := opusProvider.Model(ctx)
	if err != nil {
		log.Fatalf("failed to create opus model: %v", err)
	}

	// Configure runtime with multi-model support.
	rt, err := api.New(ctx, api.Options{
		ProjectRoot: ".",
		Model:       sonnet, // Default model.

		// Model pool for cost optimization.
		ModelPool: map[string]modelpkg.Model{
			string(api.ModelTierLow):  haiku,
			string(api.ModelTierMid):  sonnet,
			string(api.ModelTierHigh): opus,
		},

		// Tool-to-model mapping. Tool names must match the registered tool Name().
		ToolModelMapping: map[string]string{
			"Grep": string(api.ModelTierLow),
			"Glob": string(api.ModelTierLow),
			"Read": string(api.ModelTierLow),

			"Bash":  string(api.ModelTierMid),
			"Write": string(api.ModelTierMid),
			"Edit":  string(api.ModelTierMid),

			"Task": string(api.ModelTierHigh),
		},

		MaxIterations: 10,
		Timeout:       5 * time.Minute,
	})
	if err != nil {
		log.Fatalf("failed to create runtime: %v", err)
	}
	defer rt.Close()

	fmt.Println("Multi-model runtime configured successfully!")
	fmt.Println("\nModel Pool:")
	fmt.Println("  - low:  claude-3-5-haiku (fast, cheap)")
	fmt.Println("  - mid:  claude-sonnet-4 (balanced)")
	fmt.Println("  - high: claude-sonnet-4 (powerful placeholder)")
	fmt.Println("\nTool Mappings:")
	fmt.Println("  - Grep, Glob, Read -> low (Haiku)")
	fmt.Println("  - Bash, Write, Edit -> mid (Sonnet)")
	fmt.Println("  - Task -> high (Opus/placeholder)")
	fmt.Println("\nTools not in mapping use the default model (Sonnet).")

	resp, err := rt.Run(ctx, api.Request{
		Prompt:    "List the files in the current directory.",
		SessionID: "multimodel-demo",
	})
	if err != nil {
		log.Fatalf("failed to run: %v", err)
	}

	fmt.Println("\n--- Response ---")
	if resp.Result != nil {
		fmt.Println(resp.Result.Output)
	}
}

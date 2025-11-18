package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cexll/agentsdk-go/pkg/api"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

// basic example showing how the unified API powers CLI-style runs.
func main() {
	provider := &modelpkg.AnthropicProvider{ModelName: "claude-3-5-sonnet-20241022"}
	rt, err := api.New(context.Background(), api.Options{
		EntryPoint:   api.EntryPointCLI,
		ProjectRoot:  ".",
		ModelFactory: provider,
	})
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}
	defer rt.Close()

	req := api.Request{
		Prompt: "用一句中文介绍 agentsdk-go 项目。",
		Mode: api.ModeContext{
			EntryPoint: api.EntryPointCLI,
			CLI:        &api.CLIContext{User: os.Getenv("USER")},
		},
	}
	resp, err := rt.Run(context.Background(), req)
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	if resp.Result != nil {
		fmt.Println(resp.Result.Output)
	}
}

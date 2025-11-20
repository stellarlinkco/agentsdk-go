package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

// 交互式 REPL 示例：使用固定会话 ID 保持历史，可通过 --settings-path 读取 .claude/settings.json。
func main() {
	sessionID := flag.String("session-id", defaultSessionID(), "session identifier to keep chat history")
	settingsPath := flag.String("settings-path", "", "path to .claude/settings.json for sandbox/tools config")
	flag.Parse()

	provider := &modelpkg.AnthropicProvider{ModelName: "claude-sonnet-4-5-20250929"}

	// 创建 debug middleware
	traceMW := middleware.NewTraceMiddleware(".trace")

	rt, err := api.New(context.Background(), api.Options{
		EntryPoint:   api.EntryPointCLI,
		ModelFactory: provider,
		Middleware:   []middleware.Middleware{traceMW},
		SettingsPath: *settingsPath,
	})
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}
	defer rt.Close()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("You> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "exit" {
			break
		}
		if line == "" {
			continue
		}

		req := api.Request{
			Prompt:    line,
			SessionID: *sessionID,
			Mode: api.ModeContext{
				EntryPoint: api.EntryPointCLI,
				CLI:        &api.CLIContext{User: os.Getenv("USER")},
			},
		}
		resp, err := rt.Run(context.Background(), req)
		if err != nil {
			log.Fatalf("run: %v", err)
		}
		if resp.Result == nil {
			continue
		}

		fmt.Printf("\nAssistant> %s\n\n", resp.Result.Output)
		for _, call := range resp.Result.ToolCalls {
			params, err := json.MarshalIndent(call.Arguments, "", "  ")
			if err != nil {
				log.Printf("marshal tool params: %v", err)
				continue
			}
			fmt.Printf("Tool %s params: %s\n", call.Name, string(params))
		}
	}
}

func defaultSessionID() string {
	if env := strings.TrimSpace(os.Getenv("SESSION_ID")); env != "" {
		return env
	}
	return "demo-session"
}

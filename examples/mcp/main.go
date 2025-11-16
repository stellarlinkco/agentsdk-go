package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

func main() {
	registry := tool.NewRegistry()

	// Register a remote MCP server. Replace the URL or command with your deployment.
	if err := registry.RegisterMCPServer("http://localhost:8080"); err != nil {
		log.Fatalf("register MCP server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := registry.Execute(ctx, "echo", map[string]interface{}{"text": "hello"})
	if err != nil {
		log.Fatalf("execute remote tool: %v", err)
	}

	fmt.Println("MCP tool output:", result.Output)
}

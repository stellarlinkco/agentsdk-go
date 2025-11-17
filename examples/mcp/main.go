package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

func main() {
	registry := tool.NewRegistry()

	const stdioSpec = "stdio://uvx mcp-server-time"
	if err := registry.RegisterMCPServer(stdioSpec); err != nil {
		log.Fatalf("register MCP server %q: %v", stdioSpec, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]interface{}{
		"timezone": "UTC",
	}

	result, err := registry.Execute(ctx, "get_current_time", params)
	if err != nil {
		log.Fatalf("execute time tool: %v", err)
	}

	if raw, ok := result.Data.(json.RawMessage); ok {
		var out bytes.Buffer
		if err := json.Indent(&out, raw, "", "  "); err == nil {
			fmt.Println("Current time response:")
			fmt.Println(out.String())
			return
		}
	}

	fmt.Println("Current time (raw):", result.Output)
}

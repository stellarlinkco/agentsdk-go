// DEPRECATED: This combined stream + HTTP example is retained only for backward compatibility.
// Prefer the focused examples instead:
//   - simple-stream: streaming only
//   - tool-stream: tools with streaming
//   - http-simple: HTTP server without streaming
//   - http-stream: HTTP server with SSE
//   - http-full: production-grade HTTP server
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	"github.com/cexll/agentsdk-go/pkg/server"
	"github.com/cexll/agentsdk-go/pkg/tool"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

const defaultModel = "claude-3-5-sonnet-20241022"

func main() {
	log.Println("DEPRECATED: use simple-stream, tool-stream, http-simple, http-stream, or http-full instead of examples/stream.")
	ctx := context.Background()
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}

	claudeModel, err := newAnthropicModel(ctx, apiKey)
	if err != nil {
		log.Fatalf("create anthropic model: %v", err)
	}
	log.Printf("Anthropic model ready: %T (%s)", claudeModel, defaultModel)

	ag, err := agent.New(agent.Config{
		Name: "stream-demo",
		DefaultContext: agent.RunContext{
			SessionID:     "demo-session",
			MaxIterations: 10,
		},
	})
	if err != nil {
		log.Fatalf("new agent: %v", err)
	}

	tools := []tool.Tool{
		toolbuiltin.NewBashTool(),
		toolbuiltin.NewFileTool(),
		toolbuiltin.NewGlobTool(),
		toolbuiltin.NewGrepTool(),
	}
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		if err := ag.AddTool(t); err != nil {
			log.Fatalf("add %s tool: %v", t.Name(), err)
		}
		toolNames = append(toolNames, t.Name())
	}

	log.Println("--- RunStream sample ---")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := ag.RunStream(ctx, "hello streaming world")
	if err != nil {
		log.Fatalf("run stream: %v", err)
	}
	for evt := range events {
		log.Printf("event=%s data=%v", evt.Type, evt.Data)
	}

	addr := ":8080"
	log.Println("--- Starting HTTP/SSE server ---")
	log.Printf("Registered tools (%d): %s", len(toolNames), strings.Join(toolNames, ", "))
	srv := server.New(ag)
	mux := http.NewServeMux()
	mux.Handle("/health", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	mux.Handle("/", srv)

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	log.Printf("HTTP server listening on %s", addr)
	log.Println("POST /run        -> curl -X POST http://localhost:8080/run -d '{\"input\":\"demo\"}'")
	log.Println("GET  /run/stream -> curl -N http://localhost:8080/run/stream?input=hello")
	log.Println("GET  /health     -> curl http://localhost:8080/health")

	select {}
}

// newAnthropicModel wires the demo to the official Anthropic Go SDK wrapper.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

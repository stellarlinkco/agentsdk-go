// Example HTTP server showcasing both synchronous POST /run and SSE streaming GET /run/stream.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	"github.com/cexll/agentsdk-go/pkg/server"
	"github.com/cexll/agentsdk-go/pkg/tool"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

const defaultModel = "claude-3-5-sonnet-20241022"

func main() {
	ctx := context.Background()

	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}

	// Reuse the Anthropic helper so misconfiguration is caught before serving requests.
	if _, err := newAnthropicModel(ctx, apiKey); err != nil {
		log.Fatalf("create anthropic model: %v", err)
	}

	ag, err := agent.New(agent.Config{
		Name: "http-stream-demo",
		DefaultContext: agent.RunContext{
			SessionID:     "http-stream-session",
			MaxIterations: 6,
		},
	})
	if err != nil {
		log.Fatalf("new agent: %v", err)
	}

	// Register Bash + File + Glob so the agent can execute commands and inspect files.
	tools := []tool.Tool{
		toolbuiltin.NewBashTool(),
		toolbuiltin.NewFileTool(),
		toolbuiltin.NewGlobTool(),
	}
	for _, t := range tools {
		if err := ag.AddTool(t); err != nil {
			log.Fatalf("add %s tool: %v", t.Name(), err)
		}
	}

	// server.New already wires POST /run and GET /run/stream, we just add /health.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", server.New(ag))

	httpSrv := &http.Server{Addr: ":8080", Handler: mux}

	log.Printf("HTTP server listening on %s", httpSrv.Addr)
	log.Println("This demo shows HTTP sync responses and SSE streaming responses side-by-side.")
	log.Println(`POST /run        -> curl -X POST http://localhost:8080/run -d '{"input":"demo"}'`)
	log.Println(`GET  /run/stream -> curl -N http://localhost:8080/run/stream?input=hello`)

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}

// newAnthropicModel now uses the official SDK wrapper to avoid manual provider wiring.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

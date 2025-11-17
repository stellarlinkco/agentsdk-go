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
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

const defaultModel = "claude-3-5-sonnet-20241022"

func main() {
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
		Name: "http-simple-agent",
		DefaultContext: agent.RunContext{
			SessionID: "http-simple-session", WorkDir: ".", MaxIterations: 4,
		},
	})
	if err != nil {
		log.Fatalf("new agent: %v", err)
	}

	if err := ag.AddTool(toolbuiltin.NewBashTool()); err != nil {
		log.Fatalf("add bash tool: %v", err)
	}
	if err := ag.AddTool(toolbuiltin.NewFileTool()); err != nil {
		log.Fatalf("add file tool: %v", err)
	}

	// HTTP server example: demonstrates non-streaming POST /run with an optional GET /health probe.
	srv := server.New(ag)
	mux := http.NewServeMux()
	mux.Handle("/run", srv)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":8080"
	httpSrv := &http.Server{Addr: addr, Handler: mux}

	log.Printf("HTTP non-streaming demo listening on %s", addr)
	log.Printf("POST /run -> curl -s -X POST http://localhost%[1]s/run -H 'Content-Type: application/json' -d '{\"input\":\"hello\"}'", addr)

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}

// newAnthropicModel now reuses the official Anthropic Go SDK wrapper.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

package main

// Production-grade HTTP API example wiring sync and streaming endpoints with every builtin tool registered.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	"github.com/cexll/agentsdk-go/pkg/server"
	"github.com/cexll/agentsdk-go/pkg/tool"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

const (
	defaultModel = "claude-3-5-sonnet-20241022"
	defaultAddr  = ":8080"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		Name:           "http-full-example",
		Description:    "Production HTTP API wiring sync + SSE endpoints with builtin tools",
		DefaultContext: agent.RunContext{SessionID: "http-full-session", WorkDir: ".", MaxIterations: 12},
	})
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	tools := []tool.Tool{toolbuiltin.NewBashTool(), toolbuiltin.NewFileTool(), toolbuiltin.NewGlobTool(), toolbuiltin.NewGrepTool()}
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		if err := ag.AddTool(t); err != nil {
			log.Fatalf("register tool %s: %v", t.Name(), err)
		}
		toolNames = append(toolNames, t.Name())
	}

	addr := strings.TrimSpace(os.Getenv("HTTP_SERVER_ADDR"))
	if addr == "" {
		addr = defaultAddr
	}

	srv := server.New(ag)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", srv)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	printUsage(addr, toolNames)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	} else {
		log.Println("HTTP server stopped cleanly")
	}
}

func printUsage(addr string, tools []string) {
	log.Printf("====================================================\nHTTP Full Example ready (sync + SSE + health)\nListening on http://localhost%s\nRegistered builtin tools: %s\nPOST /run        -> curl -s -X POST http://localhost:8080/run -H 'Content-Type: application/json' -d '{\"input\":\"ls\"}'\nGET  /run/stream -> curl -N http://localhost:8080/run/stream?input=plan\nGET  /health     -> curl -s http://localhost:8080/health\n====================================================", addr, strings.Join(tools, ", "))
}

// newAnthropicModel now reuses the official SDK wrapper to minimize configuration drift.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

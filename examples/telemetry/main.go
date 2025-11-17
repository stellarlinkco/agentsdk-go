package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	"github.com/cexll/agentsdk-go/pkg/telemetry"
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

	mgr, err := telemetry.NewManager(telemetry.Config{ // Manager initialization wires tracer, meter, and masking filter.
		ServiceName:    "telemetry-example",
		ServiceVersion: "0.1.0",
		Environment:    "demo",
		Filter: telemetry.FilterConfig{
			Mask: "***REDACTED***",
			Patterns: []string{
				`customer-id\s*[=:]\s*\d+`,
			},
		},
	})
	if err != nil {
		log.Fatalf("create telemetry manager: %v", err)
	}
	defer func() {
		if shutdownErr := mgr.Shutdown(ctx); shutdownErr != nil {
			log.Printf("telemetry shutdown: %v", shutdownErr)
		}
	}()
	telemetry.SetDefault(mgr) // Expose manager to helper functions inside the SDK.

	sensitivePrompt := "Run `echo hello` with key sk-demo-secret-001 and customer-id 4242"
	log.Printf("prompt raw=%s masked=%s", sensitivePrompt, mgr.MaskText(sensitivePrompt)) // Filter masks inline secrets.

	started := time.Now() // StartSpan/EndSpan wrap request handling so traces capture errors and timing.
	ctx, span := mgr.StartSpan(ctx, "examples.telemetry.request")
	var runErr error
	defer telemetry.EndSpan(span, runErr)

	log.Println("simulating model request with telemetry annotations...")
	time.Sleep(250 * time.Millisecond)
	runErr = fmt.Errorf("simulated upstream timeout")

	reqData := telemetry.RequestData{ // RecordRequest publishes latency + error metrics (input auto-masked by Filter).
		Kind:      "Run",
		AgentName: "telemetry-example-agent",
		SessionID: "telemetry-session-001",
		Input:     sensitivePrompt,
		Duration:  time.Since(started),
		Error:     runErr,
	}
	mgr.RecordRequest(ctx, reqData)
	log.Printf("request metrics recorded: duration=%s err=%v", reqData.Duration.Round(time.Millisecond), runErr)

	toolErr := fmt.Errorf("bash tool missing") // RecordToolCall increments counters for each tool invocation.
	toolData := telemetry.ToolData{
		AgentName: "telemetry-example-agent",
		Name:      "bash",
		Error:     toolErr,
	}
	mgr.RecordToolCall(ctx, toolData)
	log.Printf("tool metrics recorded: tool=%s err=%v", toolData.Name, toolErr)

	attrs := mgr.SanitizeAttributes( // SanitizeAttributes removes secrets before attaching OTEL attributes to spans.
		attribute.String("request.input", sensitivePrompt),
		attribute.String("custom.note", "attributes sanitized before span attachment"),
	)
	log.Printf("sanitized span attributes: %v", attrs)

	log.Println("telemetry demo finished; inspect OTEL exporter for spans/metrics")
}

func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}

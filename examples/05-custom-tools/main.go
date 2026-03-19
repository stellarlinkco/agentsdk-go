package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

var (
	customToolsFatal = log.Fatal
)

type customToolsRuntime interface {
	Run(context.Context, api.Request) (*api.Response, error)
	Close() error
}

var customToolsNewRuntime = func(ctx context.Context, opts api.Options) (customToolsRuntime, error) {
	return api.New(ctx, opts)
}

// EchoTool is a simple custom tool used for demonstration.
type EchoTool struct{}

func (t *EchoTool) Name() string        { return "echo" }
func (t *EchoTool) Description() string { return "return the provided text" }
func (t *EchoTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]any{
			"text": map[string]any{"type": "string", "description": "text to return"},
		},
		Required: []string{"text"},
	}
}
func (t *EchoTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
	_ = ctx
	return &tool.ToolResult{Output: fmt.Sprint(params["text"])}, nil
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		customToolsFatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	opts, err := buildOptions(args)
	if err != nil {
		return err
	}

	rt, err := customToolsNewRuntime(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	resp, err := rt.Run(ctx, api.Request{
		Prompt:    "Use the echo tool to repeat 'hello from custom tool'",
		SessionID: "custom-tools-demo",
	})
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	if resp != nil && resp.Result != nil && strings.TrimSpace(resp.Result.Output) != "" {
		fmt.Fprintln(out, resp.Result.Output)
		return nil
	}
	fmt.Fprintln(out, "(no output)")
	return nil
}

func buildOptions(args []string) (api.Options, error) {
	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return api.Options{}, fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}

	opts := api.Options{
		ProjectRoot:         ".",
		EnabledBuiltinTools: []string{"bash", "read"},
		CustomTools:         []tool.Tool{&EchoTool{}},
		ModelFactory: &model.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: "claude-sonnet-4-5-20250929",
		},
	}
	return opts, nil
}

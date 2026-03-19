package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

var (
	embedFatal = log.Fatal
)

type embedRuntime interface {
	Run(context.Context, api.Request) (*api.Response, error)
	Close() error
}

var embedNewRuntime = func(ctx context.Context, opts api.Options) (embedRuntime, error) {
	return api.New(ctx, opts)
}

//go:embed .agents
var agentsFS embed.FS

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		embedFatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	opts, err := buildOptions(args)
	if err != nil {
		return err
	}
	opts.EmbedFS = agentsFS

	runtime, err := embedNewRuntime(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer runtime.Close()

	resp, err := runtime.Run(ctx, api.Request{
		Prompt:    "列出当前目录",
		SessionID: "embed-demo",
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
	_ = args
	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return api.Options{}, fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}
	return api.Options{
		ProjectRoot: ".",
		ModelFactory: &model.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: "claude-sonnet-4-5-20250929",
		},
	}, nil
}

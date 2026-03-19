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
	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

var (
	basicFatal = log.Fatal
)

type basicRuntime interface {
	Run(context.Context, api.Request) (*api.Response, error)
	Close() error
}

var basicNewRuntime = func(ctx context.Context, opts api.Options) (basicRuntime, error) {
	return api.New(ctx, opts)
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, ".trace"); err != nil {
		basicFatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer, traceDir string) error {
	opts, err := buildOptions(args, out, traceDir)
	if err != nil {
		return err
	}

	traceMW := middleware.NewTraceMiddleware(traceDir)
	defer traceMW.Close()
	opts.Middleware = []middleware.Middleware{traceMW}

	rt, err := basicNewRuntime(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	resp, err := rt.Run(ctx, api.Request{Prompt: "你好"})
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

func buildOptions(args []string, _ io.Writer, _ string) (api.Options, error) {
	_ = args
	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return api.Options{}, fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}
	return api.Options{
		ProjectRoot: ".",
		ModelFactory: &modelpkg.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: "claude-sonnet-4-5-20250929",
		},
	}, nil
}

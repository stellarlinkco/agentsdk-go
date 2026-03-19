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
	basicFatal                       = log.Fatal
	basicNewRuntime                  = api.New
	basicOfflineModel modelpkg.Model = &demomodel.EchoModel{Prefix: "offline"}
)

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
	opts := api.Options{ProjectRoot: "."}
	if hasArg(args, "--online") {
		apiKey := demomodel.AnthropicAPIKey()
		if strings.TrimSpace(apiKey) == "" {
			return api.Options{}, fmt.Errorf("--online requires ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN)")
		}
		opts.ModelFactory = &modelpkg.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: "claude-sonnet-4-5-20250929",
		}
		return opts, nil
	}
	opts.Model = basicOfflineModel
	return opts, nil
}

func hasArg(args []string, want string) bool {
	if strings.TrimSpace(want) == "" {
		return false
	}
	for _, arg := range args {
		if strings.TrimSpace(arg) == want {
			return true
		}
	}
	return false
}

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
	embedFatal                    = log.Fatal
	embedNewRuntime               = api.New
	embedOfflineModel model.Model = &demomodel.EchoModel{Prefix: "offline"}
)

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
	opts := api.Options{ProjectRoot: "."}
	if hasArg(args, "--online") {
		apiKey := demomodel.AnthropicAPIKey()
		if strings.TrimSpace(apiKey) == "" {
			return api.Options{}, fmt.Errorf("--online requires ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN)")
		}
		opts.ModelFactory = &model.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: "claude-sonnet-4-5-20250929",
		}
		return opts, nil
	}
	opts.Model = embedOfflineModel
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

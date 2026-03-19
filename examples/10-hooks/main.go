package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

var (
	hooksFatal = log.Fatal
)

type hooksRuntime interface {
	Run(context.Context, api.Request) (*api.Response, error)
	Close() error
}

var hooksNewRuntime = func(ctx context.Context, opts api.Options) (hooksRuntime, error) {
	return api.New(ctx, opts)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		hooksFatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	_ = args

	_, currentFile, _, _ := runtime.Caller(0)
	exampleDir := filepath.Dir(currentFile)
	scriptsDir := filepath.Join(exampleDir, "scripts")

	typedHooks := []hooks.ShellHook{
		{Event: hooks.PreToolUse, Command: filepath.Join(scriptsDir, "pre_tool.sh")},
		{Event: hooks.PostToolUse, Command: filepath.Join(scriptsDir, "post_tool.sh"), Async: true},
	}

	opts := api.Options{
		ProjectRoot: exampleDir,
		TypedHooks:  typedHooks,
	}
	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}
	opts.ModelFactory = &modelpkg.AnthropicProvider{
		APIKey:    apiKey,
		BaseURL:   demomodel.AnthropicBaseURL(),
		ModelName: "claude-sonnet-4-5-20250514",
	}

	rt, err := hooksNewRuntime(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	resp, err := rt.Run(ctx, api.Request{
		Prompt:    "请用 pwd 命令显示当前目录",
		SessionID: "hooks-demo",
	})
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	if resp != nil && resp.Result != nil {
		_ = resp.Result.Output
	}
	return nil
}

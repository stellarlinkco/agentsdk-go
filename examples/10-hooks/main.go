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
	hooksFatal                  = log.Fatal
	offlineModel modelpkg.Model = &demomodel.EchoModel{Prefix: "offline"}
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		hooksFatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	online := false
	for _, arg := range args {
		if strings.TrimSpace(arg) == "--online" {
			online = true
		}
	}

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
	if online {
		apiKey := demomodel.AnthropicAPIKey()
		if strings.TrimSpace(apiKey) == "" {
			return fmt.Errorf("--online requires ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN)")
		}
		opts.ModelFactory = &modelpkg.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: "claude-sonnet-4-5-20250514",
		}
	} else {
		opts.Model = offlineModel
	}

	rt, err := api.New(ctx, opts)
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

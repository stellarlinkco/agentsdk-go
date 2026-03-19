package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

const defaultModel = "claude-sonnet-4-5-20250929"

var (
	cliFatal    = log.Fatal
	filepathAbs = filepath.Abs
)

type runConfig struct {
	sessionID   string
	projectRoot string
	enableMCP   bool
	interactive bool
	prompt      string
}

type runtimeRunner interface {
	Run(context.Context, api.Request) (*api.Response, error)
	Close() error
}

var cliNewRuntime = func(ctx context.Context, opts api.Options) (runtimeRunner, error) {
	return api.New(ctx, opts)
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		cliFatal(err)
	}
}

func run(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	cfg, opts, err := buildConfigAndOptions(args, out)
	if err != nil {
		return err
	}

	rt, err := cliNewRuntime(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	if !cfg.interactive {
		resp, err := rt.Run(ctx, api.Request{
			Prompt:    cfg.prompt,
			SessionID: cfg.sessionID,
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

	fmt.Fprintln(out, "Type 'exit' to quit.")
	if cfg.enableMCP {
		fmt.Fprintln(out, "MCP auto-load enabled; SDK will read .agents/settings.json. Use --enable-mcp=false to disable.")
	}
	fmt.Fprintln(out)

	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "You> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" {
			break
		}
		resp, err := rt.Run(ctx, api.Request{
			Prompt:    input,
			SessionID: cfg.sessionID,
		})
		if err != nil {
			fmt.Fprintf(out, "\nError: %v\n\n", err)
			continue
		}
		if resp != nil && resp.Result != nil && strings.TrimSpace(resp.Result.Output) != "" {
			fmt.Fprintf(out, "\nAssistant> %s\n\n", resp.Result.Output)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	return nil
}

func buildConfigAndOptions(args []string, out io.Writer) (runConfig, api.Options, error) {
	fs := flag.NewFlagSet("02-cli", flag.ContinueOnError)
	fs.SetOutput(out)

	var cfg runConfig
	fs.StringVar(&cfg.sessionID, "session-id", envOrDefault("SESSION_ID", "demo-session"), "session identifier to keep chat history")
	fs.StringVar(&cfg.projectRoot, "project-root", ".", "project root directory (default: current directory)")
	fs.BoolVar(&cfg.enableMCP, "enable-mcp", false, "enable MCP servers from .agents/settings.json (auto-loaded)")
	fs.BoolVar(&cfg.interactive, "interactive", false, "run in interactive REPL mode (default: single prompt and exit)")
	fs.StringVar(&cfg.prompt, "prompt", "你好", "single prompt used when not interactive")
	if err := fs.Parse(args); err != nil {
		return runConfig{}, api.Options{}, err
	}

	absRoot, err := filepathAbs(cfg.projectRoot)
	if err != nil {
		return runConfig{}, api.Options{}, fmt.Errorf("resolve project root: %w", err)
	}

	opts := api.Options{
		EntryPoint:  api.EntryPointCLI,
		ProjectRoot: absRoot,
	}
	if !cfg.enableMCP {
		opts.MCPServers = []string{}
	}

	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return runConfig{}, api.Options{}, fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}
	opts.ModelFactory = &modelpkg.AnthropicProvider{
		APIKey:    apiKey,
		BaseURL:   demomodel.AnthropicBaseURL(),
		ModelName: defaultModel,
	}

	return cfg, opts, nil
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

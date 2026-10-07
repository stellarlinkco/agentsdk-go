package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

const (
	parallelEndpoint  = "https+http://search.parallel.ai/mcp"
	parallelUserAgent = "agentsdk-go/parallel-search-example"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, parallelEndpoint); err != nil {
		log.Print(err)
	}
}

// run uses the SDK registry directly; no model inference or saved settings are loaded.
func run(ctx context.Context, args []string, out io.Writer, endpoint string) error {
	flags := flag.NewFlagSet("parallel-search", flag.ContinueOnError)
	flags.SetOutput(out)
	query := flags.String("query", "Go context cancellation documentation", "search query (ignored when -url is set)")
	pageURL := flags.String("url", "", "fetch this HTTP(S) URL instead of searching")
	sessionID := flags.String("session-id", "", "reuse an ID for related search and fetch calls")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *sessionID == "" {
		*sessionID = uuid.NewString()
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	registry := tool.NewRegistry()
	defer registry.Close()
	if err := registry.RegisterMCPServerWithOptions(ctx, endpoint, "parallel", tool.MCPServerOptions{
		Headers:      map[string]string{"User-Agent": parallelUserAgent},
		Timeout:      20 * time.Second,
		ToolTimeout:  60 * time.Second,
		EnabledTools: []string{"web_search", "web_fetch"},
	}); err != nil {
		return fmt.Errorf("register Parallel MCP: %w", err)
	}

	name := "parallel__web_search"
	params := map[string]any{
		"objective":      "Find reliable sources for: " + *query,
		"search_queries": []any{*query},
		"session_id":     *sessionID,
	}
	if strings.TrimSpace(*pageURL) != "" {
		name = "parallel__web_fetch"
		params = map[string]any{"urls": []any{*pageURL}, "session_id": *sessionID}
	}
	result, err := registry.Execute(ctx, name, params)
	if err != nil {
		return fmt.Errorf("execute %s: %w", name, err)
	}
	if result == nil || strings.TrimSpace(result.Output) == "" {
		return fmt.Errorf("%s returned no output", name)
	}
	_, err = fmt.Fprintf(out, "Session: %s\n%s\n", *sessionID, result.Output)
	return err
}

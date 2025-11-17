package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/tool"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

var agentFactory = agent.New

func runCommand(ctx context.Context, argv []string, cfgPath string, streams ioStreams) error {
	set := flag.NewFlagSet("run", flag.ContinueOnError)
	set.SetOutput(streams.err)
	var (
		modelFlag   = set.String("model", "", "Override the model declared in config.json.")
		sessionFlag = set.String("session", "", "Reuse an existing session ID to resume context.")
		streamFlag  = set.Bool("stream", false, "Stream progress events instead of waiting for completion.")
		configFlag  = set.String("config", cfgPath, "Path to CLI config file.")
	)
	var toolFlags multiValue
	var mcpFlags multiValue
	set.Var(&toolFlags, "tool", fmt.Sprintf("Register built-in tools (%s,all). Repeatable.", strings.Join(allBuiltinToolNames(), ",")))
	set.Var(&mcpFlags, "mcp", "Load tools exposed by an MCP server (URL or stdio command). Repeatable.")
	set.Usage = func() {
		fmt.Fprintln(streams.err, "Usage: agentctl run [flags] \"task description\"")
		fmt.Fprintln(streams.err, "\nFlags:")
		set.PrintDefaults()
		fmt.Fprintln(streams.err, "\nExamples:")
		fmt.Fprintln(streams.err, "  agentctl run \"fix TODOs in repo\"")
		fmt.Fprintln(streams.err, "  agentctl run --session dev --tool bash \"run go test\"")
		fmt.Fprintln(streams.err, "  agentctl run --stream --mcp http://localhost:8082 \"plan release\"")
	}
	if err := set.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfgPath = *configFlag
	cfg, err := loadCLIConfig(cfgPath)
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(set.Args(), " "))
	if input == "" {
		return errors.New("run requires a task description")
	}
	model := pickString(*modelFlag, cfg.DefaultModel)
	sessionID := pickString(*sessionFlag, "")
	mcpServers := uniqueStrings(mcpFlags.slice(), cfg.MCPServers)
	ag, err := agentFactory(agent.Config{
		Name:        "agentctl",
		Description: "agentsdk-go CLI runner",
		DefaultContext: agent.RunContext{
			MaxIterations: 1,
		},
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	if err := attachTools(ag, toolFlags.slice(), mcpServers); err != nil {
		return err
	}
	runCtx := ctx
	if sessionID != "" {
		runCtx = agent.WithRunContext(runCtx, agent.RunContext{SessionID: sessionID})
	}
	if *streamFlag {
		return streamRun(runCtx, ag, input, model, sessionID, streams.out)
	}
	result, err := ag.Run(runCtx, input)
	if err != nil {
		return fmt.Errorf("agent run: %w", err)
	}
	writeMarkdownResult(streams.out, result, resultMeta{Model: model, Session: sessionID})
	return nil
}

func streamRun(ctx context.Context, ag agent.Agent, input, model, session string, out io.Writer) error {
	events, err := ag.RunStream(ctx, input)
	if err != nil {
		return fmt.Errorf("agent run stream: %w", err)
	}
	if out == nil {
		return nil
	}
	fmt.Fprintln(out, "# agentctl run (stream)")
	fmt.Fprintf(out, "- Model: `%s`\n", labelOrNA(model))
	if session != "" {
		fmt.Fprintf(out, "- Session: `%s`\n", session)
	}
	fmt.Fprintln(out, "\n```json")
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	for evt := range events {
		if err := encoder.Encode(evt); err != nil {
			return fmt.Errorf("stream encode: %w", err)
		}
	}
	fmt.Fprintln(out, "```")
	return nil
}

type resultMeta struct {
	Model   string
	Session string
}

func writeMarkdownResult(out io.Writer, res *agent.RunResult, meta resultMeta) {
	if out == nil || res == nil {
		return
	}
	fmt.Fprintln(out, "# agentctl run")
	fmt.Fprintf(out, "- Model: `%s`\n", labelOrNA(meta.Model))
	if meta.Session != "" {
		fmt.Fprintf(out, "- Session: `%s`\n", meta.Session)
	}
	fmt.Fprintf(out, "- Stop Reason: `%s`\n", safeString(res.StopReason))
	fmt.Fprintln(out, "\n## Output")
	fmt.Fprintf(out, "```\n%s\n```\n", res.Output)
	fmt.Fprintln(out, "\n## Usage")
	fmt.Fprintf(out, "- Input tokens: %d\n", res.Usage.InputTokens)
	fmt.Fprintf(out, "- Output tokens: %d\n", res.Usage.OutputTokens)
	fmt.Fprintf(out, "- Total tokens: %d\n", res.Usage.TotalTokens)
	fmt.Fprintf(out, "- Cache tokens: %d\n", res.Usage.CacheTokens)
	if len(res.ToolCalls) == 0 {
		return
	}
	fmt.Fprintln(out, "\n## Tool Calls")
	for _, call := range res.ToolCalls {
		status := "ok"
		if call.Failed() {
			status = "error"
		}
		detail := safeString(call.Error)
		if detail == "" && call.Duration > 0 {
			detail = fmt.Sprintf("%dms", call.Duration.Milliseconds())
		}
		fmt.Fprintf(out, "- `%s` (%s): %s\n", call.Name, status, detail)
	}
}

func attachTools(ag agent.Agent, builtinNames, mcpServers []string) error {
	if ag == nil {
		return errors.New("agent is nil")
	}
	names := expandToolNames(builtinNames)
	if len(names) == 0 && len(mcpServers) == 0 {
		return nil
	}
	registry := tool.NewRegistry()
	for _, name := range names {
		creator, ok := builtinToolConstructors[name]
		if !ok {
			return fmt.Errorf("unknown tool %s", name)
		}
		if err := registry.Register(creator()); err != nil {
			return fmt.Errorf("register tool %s: %w", name, err)
		}
	}
	for _, server := range mcpServers {
		if err := registry.RegisterMCPServer(server); err != nil {
			return fmt.Errorf("register MCP server %s: %w", server, err)
		}
	}
	for _, t := range registry.List() {
		if err := ag.AddTool(t); err != nil {
			return fmt.Errorf("attach tool %s: %w", t.Name(), err)
		}
	}
	return nil
}

func pickString(primary, fallback string) string {
	primary = strings.TrimSpace(primary)
	if primary != "" {
		return primary
	}
	return strings.TrimSpace(fallback)
}

func labelOrNA(value string) string {
	if strings.TrimSpace(value) == "" {
		return "n/a"
	}
	return value
}

func safeString(v string) string {
	return strings.TrimSpace(v)
}

func expandToolNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	set := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed == "" {
			continue
		}
		if trimmed == "all" {
			for _, builtinName := range allBuiltinToolNames() {
				if _, ok := seen[builtinName]; ok {
					continue
				}
				seen[builtinName] = struct{}{}
				set = append(set, builtinName)
			}
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		set = append(set, trimmed)
	}
	return set
}

var builtinToolConstructors = map[string]func() tool.Tool{
	"bash": func() tool.Tool { return toolbuiltin.NewBashTool() },
	"file": func() tool.Tool { return toolbuiltin.NewFileTool() },
	"glob": func() tool.Tool { return toolbuiltin.NewGlobTool() },
	"grep": func() tool.Tool { return toolbuiltin.NewGrepTool() },
}

func allBuiltinToolNames() []string {
	names := make([]string, 0, len(builtinToolConstructors))
	for name := range builtinToolConstructors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type multiValue []string

func (m *multiValue) String() string {
	return strings.Join(m.slice(), ",")
}

func (m *multiValue) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("value cannot be empty")
	}
	*m = append(*m, trimmed)
	return nil
}

func (m *multiValue) slice() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), (*m)...)
}

func uniqueStrings(groups ...[]string) []string {
	set := []string{}
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, value := range group {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			set = append(set, trimmed)
		}
	}
	return set
}

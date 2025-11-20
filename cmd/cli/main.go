package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/api"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(argv []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentsdk-cli", flag.ContinueOnError)
	flags.SetOutput(stderr)

	entry := flags.String("entry", "cli", "Entry point type (cli/ci/platform)")
	project := flags.String("project", ".", "Project root")
	claudeDir := flags.String("claude", "", "Optional path to .claude directory")
	modelName := flags.String("model", "claude-3-5-sonnet-20241022", "Anthropic model name")
	systemPrompt := flags.String("system-prompt", "", "System prompt override")
	sessionID := flags.String("session", "", "Session identifier override")
	promptFile := flags.String("prompt-file", "", "Read prompt from file (defaults to stdin/args)")
	promptLiteral := flags.String("prompt", "", "Prompt literal (overrides stdin)")
	stream := flags.Bool("stream", false, "Stream events instead of waiting for completion")

	var mcpServers multiValue
	flags.Var(&mcpServers, "mcp", "Register an MCP server (repeatable)")

	var tagFlags multiValue
	flags.Var(&tagFlags, "tag", "Attach tag key=value pairs (repeatable)")

	if err := flags.Parse(argv); err != nil {
		return err
	}
	prompt, err := resolvePrompt(*promptLiteral, *promptFile, flags.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("prompt is empty")
	}

	provider := &modelpkg.AnthropicProvider{
		ModelName: *modelName,
		System:    *systemPrompt,
	}
	settingsPath := ""
	if strings.TrimSpace(*claudeDir) != "" {
		settingsPath = filepath.Join(*claudeDir, "settings.json")
	}
	options := api.Options{
		EntryPoint:   api.EntryPoint(strings.ToLower(strings.TrimSpace(*entry))),
		ProjectRoot:  *project,
		SettingsPath: settingsPath,
		ModelFactory: provider,
		MCPServers:   mcpServers,
	}
	runtime, err := api.New(context.Background(), options)
	if err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	defer runtime.Close()

	req := api.Request{
		Prompt:    prompt,
		SessionID: strings.TrimSpace(*sessionID),
		Mode: api.ModeContext{
			EntryPoint: options.EntryPoint,
			CLI: &api.CLIContext{
				User:      os.Getenv("USER"),
				Workspace: *project,
				Args:      argv,
			},
		},
		Tags: parseTags(tagFlags),
	}
	if *stream {
		return streamRun(runtime, req, stdout)
	}
	resp, err := runtime.Run(context.Background(), req)
	if err != nil {
		return err
	}
	printResponse(resp, stdout)
	return nil
}

func resolvePrompt(literal, file string, tail []string) (string, error) {
	if strings.TrimSpace(literal) != "" {
		return literal, nil
	}
	if strings.TrimSpace(file) != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		return string(data), nil
	}
	if len(tail) > 0 {
		return strings.Join(tail, " "), nil
	}
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("no prompt provided")
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func streamRun(rt *api.Runtime, req api.Request, out io.Writer) error {
	ch, err := rt.RunStream(context.Background(), req)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	for evt := range ch {
		if err := encoder.Encode(evt); err != nil {
			return err
		}
	}
	return nil
}

func printResponse(resp *api.Response, out io.Writer) {
	if resp == nil || out == nil {
		return
	}
	fmt.Fprintf(out, "# agentsdk run (%s)\n", resp.Mode.EntryPoint)
	if resp.Result != nil {
		fmt.Fprintf(out, "stop_reason: %s\n", resp.Result.StopReason)
		fmt.Fprintf(out, "output:\n%s\n", resp.Result.Output)
	}
}

type multiValue []string

func (m *multiValue) String() string {
	return strings.Join(*m, ",")
}

func (m *multiValue) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func parseTags(values multiValue) map[string]string {
	if len(values) == 0 {
		return nil
	}
	tags := map[string]string{}
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		val := "true"
		if len(parts) == 2 {
			val = strings.TrimSpace(parts[1])
		}
		tags[key] = val
	}
	return tags
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/config"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func main() {
	cfg := parseConfig()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, settings, cleanup, err := resolveProjectRoot()
	if err != nil {
		log.Fatalf("init project root: %v", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	if settings == nil {
		def := config.GetDefaultSettings()
		settings = &def
	}
	mergedSettings := config.MergeSettings(settings, &config.Settings{
		Env: map[string]string{
			"REQUEST_OWNER": cfg.owner,
		},
	})

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	monitorMW := newMonitoringMiddleware(cfg.slowThreshold, logger)
	middlewares := []middleware.Middleware{
		newLoggingMiddleware(logger),
		newRateLimitMiddleware(cfg.rps, cfg.burst, cfg.concurrent),
		newSecurityMiddleware(nil, logger),
		newSettingsMiddleware(root, cfg.prompt, cfg.owner, mergedSettings, logger),
		monitorMW,
	}

	rt, err := api.New(ctx, api.Options{
		EntryPoint:        api.EntryPointCLI,
		ProjectRoot:       root,
		SettingsOverrides: mergedSettings,
		Model:             newDemoModel(root, cfg.owner, mergedSettings),
		Tools:             []tool.Tool{newObserveLogsTool(cfg.toolLatency, logger, mergedSettings)},
		Middleware:        middlewares,
		MiddlewareTimeout: cfg.middlewareTimeout,
		MaxIterations:     cfg.maxIterations,
		Timeout:           cfg.runTimeout,
	})
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}
	defer rt.Close()

	req := api.Request{
		Prompt: cfg.prompt,
		Mode:   api.ModeContext{EntryPoint: api.EntryPointCLI},
		Metadata: map[string]any{
			"project_root": root,
			"owner":        cfg.owner,
		},
	}

	logger.Info("running middleware demo", "prompt", cfg.prompt)
	resp, err := rt.Run(ctx, req)
	if err != nil {
		log.Fatalf("run agent: %v", err)
	}

	fmt.Println("\n===== Final Output =====")
	if resp == nil || resp.Result == nil {
		fmt.Println("(no result)")
	} else {
		fmt.Println(resp.Result.Output)
		fmt.Println("\nTool Calls:")
		if len(resp.Result.ToolCalls) == 0 {
			fmt.Println("- (no tools used)")
		} else {
			for _, call := range resp.Result.ToolCalls {
				fmt.Printf("- %s -> %v\n", call.Name, call.Arguments)
			}
		}
	}
	total, slow, maxLatency, lastLatency := monitorMW.Snapshot()
	logger.Info("metrics snapshot", "runs", total, "slow_runs", slow, "max_latency", maxLatency, "last_latency", lastLatency)
}

type runConfig struct {
	prompt            string
	owner             string
	rps               int
	burst             int
	concurrent        int
	slowThreshold     time.Duration
	toolLatency       time.Duration
	runTimeout        time.Duration
	middlewareTimeout time.Duration
	maxIterations     int
}

func parseConfig() runConfig {
	var cfg runConfig
	flag.StringVar(&cfg.prompt, "prompt", "分析 HTTP 日志并生成安全报告", "user prompt for the demo")
	flag.StringVar(&cfg.owner, "owner", "middleware-demo", "logical owner for logging")
	flag.IntVar(&cfg.rps, "rps", 5, "token bucket refill rate per second")
	flag.IntVar(&cfg.burst, "burst", 10, "token bucket burst size")
	flag.IntVar(&cfg.concurrent, "concurrent", 2, "maximum concurrent agent runs")
	flag.DurationVar(&cfg.slowThreshold, "slow-threshold", 250*time.Millisecond, "slow request threshold")
	flag.DurationVar(&cfg.toolLatency, "tool-latency", 150*time.Millisecond, "simulated tool latency")
	flag.DurationVar(&cfg.runTimeout, "timeout", 5*time.Second, "agent timeout")
	flag.DurationVar(&cfg.middlewareTimeout, "middleware-timeout", 2*time.Second, "per-hook timeout")
	flag.IntVar(&cfg.maxIterations, "max-iterations", 3, "max agent iterations")
	flag.Parse()
	return cfg
}

func resolveProjectRoot() (string, *config.Settings, func(), error) {
	if root := strings.TrimSpace(os.Getenv("AGENTSDK_PROJECT_ROOT")); root != "" {
		settings, err := (&config.SettingsLoader{ProjectRoot: root}).Load()
		if err != nil {
			return "", nil, nil, err
		}
		return root, settings, nil, nil
	}
	tmp, err := os.MkdirTemp("", "agentsdk-middleware-*")
	if err != nil {
		return "", nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if _, err := scaffoldSettings(tmp); err != nil {
		cleanup()
		return "", nil, nil, err
	}
	settings, err := (&config.SettingsLoader{ProjectRoot: tmp}).Load()
	if err != nil {
		cleanup()
		return "", nil, nil, err
	}
	return tmp, settings, cleanup, nil
}

func scaffoldSettings(root string) (*config.Settings, error) {
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return nil, err
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return nil, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	sandboxEnabled := false
	settings := &config.Settings{
		Permissions: &config.PermissionsConfig{
			Allow: []string{
				"Bash(ls:*)",
				"Bash(pwd:*)",
				"Read(**/*.go)",
			},
		},
		Env: map[string]string{
			"EXAMPLE_VAR": "value",
		},
		Sandbox: &config.SandboxConfig{
			Enabled: &sandboxEnabled,
		},
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	return settings, os.WriteFile(settingsPath, data, 0o644)
}

type demoModel struct {
	projectRoot string
	owner       string
	settings    *config.Settings
	counter     int64
}

func newDemoModel(root, owner string, settings *config.Settings) model.Model {
	if settings == nil {
		def := config.GetDefaultSettings()
		settings = &def
	}
	return &demoModel{
		projectRoot: root,
		owner:       owner,
		settings:    settings,
	}
}

func (m *demoModel) Complete(_ context.Context, req model.Request) (*model.Response, error) {
	if m == nil {
		return nil, errors.New("demo model is nil")
	}

	prompt := lastUserPrompt(req.Messages)
	if prompt == "" {
		return nil, errors.New("demo model: prompt is empty")
	}

	if summary := lastToolResult(req.Messages); summary != "" {
		return &model.Response{
			Message: model.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("安全报告：%s", summary),
			},
			StopReason: "done",
		}, nil
	}

	m.counter++
	envSuffix := ""
	if val := strings.TrimSpace(m.settings.Env["EXAMPLE_VAR"]); val != "" {
		envSuffix = fmt.Sprintf(" (EXAMPLE_VAR=%s)", val)
	}

	return &model.Response{
		Message: model.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("收到指令：%s，准备分析项目 %s。%s", prompt, m.projectRoot, envSuffix),
			ToolCalls: []model.ToolCall{{
				ID:        fmt.Sprintf("tool-%d", m.counter),
				Name:      "observe_logs",
				Arguments: map[string]any{"query": prompt, "project_root": m.projectRoot, "owner": m.owner},
			}},
		},
		StopReason: "tool_call",
	}, nil
}

func (m *demoModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	if cb == nil {
		return nil
	}
	return cb(model.StreamResult{Response: resp, Final: true})
}

func lastUserPrompt(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if strings.EqualFold(msg.Role, "user") {
			return strings.TrimSpace(msg.Content)
		}
	}
	return ""
}

func lastToolResult(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if strings.EqualFold(msg.Role, "tool") && strings.TrimSpace(msg.Content) != "" {
			return strings.TrimSpace(msg.Content)
		}
	}
	return ""
}

type observeLogsTool struct {
	latency  time.Duration
	logger   *slog.Logger
	settings *config.Settings
}

func newObserveLogsTool(latency time.Duration, logger *slog.Logger, settings *config.Settings) tool.Tool {
	if settings == nil {
		def := config.GetDefaultSettings()
		settings = &def
	}
	return &observeLogsTool{
		latency:  latency,
		logger:   logger,
		settings: settings,
	}
}

func (t *observeLogsTool) Name() string { return "observe_logs" }
func (t *observeLogsTool) Description() string {
	return "读取最近的 HTTP 访问日志并返回安全摘要"
}
func (t *observeLogsTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "日志过滤条件或诊断提示",
			},
			"project_root": map[string]any{
				"type":        "string",
				"description": "项目根目录，用于解析相对路径",
			},
			"owner": map[string]any{
				"type":        "string",
				"description": "逻辑 owner，便于链路追踪",
			},
		},
		Required: []string{"query", "project_root"},
	}
}

func (t *observeLogsTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	select {
	case <-time.After(t.latency):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	query := readString(params, "query")
	root := readString(params, "project_root")
	owner := readString(params, "owner")

	output := fmt.Sprintf("已检查 %s 的最近 100 行日志，未发现高危操作；查询: %s", root, query)
	if env := strings.TrimSpace(t.settings.Env["EXAMPLE_VAR"]); env != "" {
		output += fmt.Sprintf("；EXAMPLE_VAR=%s", env)
	}
	if owner != "" {
		output += fmt.Sprintf("；owner=%s", owner)
	}

	t.logger.Info("tool finished", "tool", t.Name(), "latency", t.latency)
	return &tool.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]any{
			"latency_ms":   t.latency.Milliseconds(),
			"project_root": root,
		},
	}, nil
}

func newSettingsMiddleware(projectRoot, prompt, owner string, settings *config.Settings, logger *slog.Logger) middleware.Middleware {
	if settings == nil {
		def := config.GetDefaultSettings()
		settings = &def
	}
	env := maps.Clone(settings.Env)
	var allowRules []string
	if settings.Permissions != nil {
		allowRules = append(allowRules, settings.Permissions.Allow...)
	}
	return middleware.Funcs{
		Identifier: "settings",
		OnBeforeAgent: func(_ context.Context, st *middleware.State) error {
			if st.Values == nil {
				st.Values = map[string]any{}
			}
			st.Values[promptKey] = prompt
			st.Values["project_root"] = projectRoot
			st.Values["settings.env"] = maps.Clone(env)
			if len(allowRules) > 0 {
				st.Values["settings.permissions.allow"] = append([]string(nil), allowRules...)
			}

			if ctx, ok := st.Agent.(*agent.Context); ok && ctx != nil {
				if ctx.Values == nil {
					ctx.Values = map[string]any{}
				}
				ctx.Values[promptKey] = prompt
				ctx.Values["project_root"] = projectRoot
				ctx.Values["request_owner"] = owner
			}

			logger.Info("settings applied", "env_keys", len(env), "allow_rules", len(allowRules))
			return nil
		},
	}
}

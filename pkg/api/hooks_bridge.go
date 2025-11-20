package api

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/config"
	coreevents "github.com/cexll/agentsdk-go/pkg/core/events"
	corehooks "github.com/cexll/agentsdk-go/pkg/core/hooks"
)

func newHookExecutor(opts Options, recorder HookRecorder, settings *config.Settings) *corehooks.Executor {
	exec := corehooks.NewExecutor(corehooks.WithMiddleware(opts.HookMiddleware...), corehooks.WithTimeout(opts.HookTimeout))
	if len(opts.TypedHooks) > 0 {
		exec.Register(opts.TypedHooks...)
	}
	if !hooksDisabled(settings) {
		if hook := buildSettingsHook(settings); hook != nil {
			exec.Register(hook)
		}
	}
	_ = recorder
	return exec
}

func hooksDisabled(settings *config.Settings) bool {
	return settings != nil && settings.DisableAllHooks != nil && *settings.DisableAllHooks
}

type settingsHook struct {
	cfg *config.HooksConfig
	env map[string]string
}

func buildSettingsHook(settings *config.Settings) *settingsHook {
	if settings == nil || settings.Hooks == nil {
		return nil
	}
	if len(settings.Hooks.PreToolUse) == 0 && len(settings.Hooks.PostToolUse) == 0 {
		return nil
	}
	env := map[string]string{}
	for k, v := range settings.Env {
		env[k] = v
	}
	return &settingsHook{cfg: settings.Hooks, env: env}
}

func (h *settingsHook) PreToolUse(ctx context.Context, payload coreevents.ToolUsePayload) error {
	return h.run(ctx, h.cfg.PreToolUse, payload.Name)
}

func (h *settingsHook) PostToolUse(ctx context.Context, payload coreevents.ToolResultPayload) error {
	return h.run(ctx, h.cfg.PostToolUse, payload.Name)
}

func (h *settingsHook) run(ctx context.Context, hooks map[string]string, toolName string) error {
	if h == nil || len(hooks) == 0 {
		return nil
	}
	cmd := strings.TrimSpace(hooks[toolName])
	if cmd == "" {
		return nil
	}
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd) // shell parity with Claude Code
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	c.Env = append(os.Environ(), formatEnv(h.env)...)
	return c.Run()
}

func formatEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

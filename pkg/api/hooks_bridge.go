package api

import (
	"log"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	hooks "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
)

func newHookExecutor(opts Options, settings *config.Settings) *hooks.Executor {
	execOpts := []hooks.ExecutorOption{
		hooks.WithMiddleware(opts.HookMiddleware...),
		hooks.WithTimeout(opts.HookTimeout),
	}
	if opts.ProjectRoot != "" {
		execOpts = append(execOpts, hooks.WithWorkDir(opts.ProjectRoot))
	}
	exec := hooks.NewExecutor(execOpts...)
	if len(opts.TypedHooks) > 0 {
		exec.Register(opts.TypedHooks...)
	}
	if !hooksDisabled(settings) {
		settingsHooks := buildSettingsHooks(settings, opts.ProjectRoot)
		if len(settingsHooks) > 0 {
			exec.Register(settingsHooks...)
		}
	}
	return exec
}

func hooksDisabled(settings *config.Settings) bool {
	return settings != nil && settings.DisableAllHooks != nil && *settings.DisableAllHooks
}

// buildSettingsHooks converts settings.Hooks config to ShellHook structs.
func buildSettingsHooks(settings *config.Settings, projectRoot string) []hooks.ShellHook {
	if settings == nil || settings.Hooks == nil {
		return nil
	}

	var out []hooks.ShellHook
	env := map[string]string{}
	for k, v := range settings.Env {
		env[k] = v
	}
	if projectRoot != "" {
		env["CLAUDE_PROJECT_DIR"] = projectRoot
	}

	addEntries := func(event hooks.EventType, entries []config.HookMatcherEntry, prefix string) {
		for _, entry := range entries {
			normalizedMatcher := normalizeToolSelectorPattern(entry.Matcher)
			sel, err := hooks.NewSelector(normalizedMatcher, "")
			if err != nil {
				continue
			}
			for _, hookDef := range entry.Hooks {
				switch hookDef.Type {
				case "command", "":
					if hookDef.Command == "" {
						continue
					}
					timeout := time.Duration(0)
					if hookDef.Timeout > 0 {
						timeout = time.Duration(hookDef.Timeout) * time.Second
					}
					out = append(out, hooks.ShellHook{
						Event:         event,
						Command:       hookDef.Command,
						Selector:      sel,
						Timeout:       timeout,
						Env:           env,
						Name:          "settings:" + prefix + ":" + normalizedMatcher,
						Async:         hookDef.Async,
						Once:          hookDef.Once,
						StatusMessage: hookDef.StatusMessage,
					})
				case "prompt", "agent":
					log.Printf("hooks: skipping %s hook type %q (not yet supported)", prefix, hookDef.Type)
				}
			}
		}
	}

	addEntries(hooks.PreToolUse, settings.Hooks.PreToolUse, "pre")
	addEntries(hooks.PostToolUse, settings.Hooks.PostToolUse, "post")
	addEntries(hooks.SessionStart, settings.Hooks.SessionStart, "session_start")
	addEntries(hooks.SessionEnd, settings.Hooks.SessionEnd, "session_end")
	addEntries(hooks.SubagentStart, settings.Hooks.SubagentStart, "subagent_start")
	addEntries(hooks.SubagentStop, settings.Hooks.SubagentStop, "subagent_stop")
	addEntries(hooks.Stop, settings.Hooks.Stop, "stop")

	return out
}

// normalizeToolSelectorPattern maps wildcard "*" to the selector wildcard (empty pattern).
func normalizeToolSelectorPattern(pattern string) string {
	if strings.TrimSpace(pattern) == "*" {
		return ""
	}
	return pattern
}

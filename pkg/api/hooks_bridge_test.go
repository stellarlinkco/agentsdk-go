package api

import (
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	hookspkg "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
)

func TestBuildSettingsHooksNil(t *testing.T) {
	if hooks := buildSettingsHooks(nil, ""); len(hooks) != 0 {
		t.Fatalf("expected no hooks, got %d", len(hooks))
	}
	if hooks := buildSettingsHooks(&config.Settings{Hooks: &config.HooksConfig{}}, ""); len(hooks) != 0 {
		t.Fatalf("expected no hooks for empty config, got %d", len(hooks))
	}
}

func TestBuildSettingsHooksCreatesCorrectTypes(t *testing.T) {
	settings := &config.Settings{
		Env: map[string]string{"KEY": "value"},
		Hooks: &config.HooksConfig{
			PreToolUse:  []config.HookMatcherEntry{{Matcher: "echo", Hooks: []config.HookDefinition{{Type: "command", Command: "echo pre"}}}},
			PostToolUse: []config.HookMatcherEntry{{Matcher: "grep", Hooks: []config.HookDefinition{{Type: "command", Command: "echo post"}}}},
		},
	}
	settingsHooks := buildSettingsHooks(settings, "/tmp/test")
	if len(settingsHooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(settingsHooks))
	}

	// Verify PreToolUse hook
	var foundPre, foundPost bool
	for _, h := range settingsHooks {
		if h.Event == hookspkg.PreToolUse {
			foundPre = true
			if h.Command != "echo pre" {
				t.Fatalf("expected pre command 'echo pre', got %q", h.Command)
			}
			if h.Env["KEY"] != "value" {
				t.Fatalf("expected env KEY=value, got %v", h.Env)
			}
		}
		if h.Event == hookspkg.PostToolUse {
			foundPost = true
			if h.Command != "echo post" {
				t.Fatalf("expected post command 'echo post', got %q", h.Command)
			}
		}
	}
	if !foundPre {
		t.Fatal("PreToolUse hook not found")
	}
	if !foundPost {
		t.Fatal("PostToolUse hook not found")
	}
}

func TestBuildSettingsHooksSkipsEmpty(t *testing.T) {
	settings := &config.Settings{
		Hooks: &config.HooksConfig{
			PreToolUse: []config.HookMatcherEntry{
				{Matcher: "echo", Hooks: []config.HookDefinition{{Type: "command", Command: ""}}},
				{Matcher: "valid", Hooks: []config.HookDefinition{{Type: "command", Command: "echo ok"}}},
			},
		},
	}
	settingsHooks := buildSettingsHooks(settings, "/tmp/test")
	if len(settingsHooks) != 1 {
		t.Fatalf("expected 1 hook (empty skipped), got %d", len(settingsHooks))
	}
}

func TestHooksDisabledFlag(t *testing.T) {
	disabled := true
	if !hooksDisabled(&config.Settings{DisableAllHooks: &disabled}) {
		t.Fatal("expected hooks disabled")
	}
}

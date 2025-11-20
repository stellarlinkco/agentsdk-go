package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/config"
	coreevents "github.com/cexll/agentsdk-go/pkg/core/events"
)

func TestBuildSettingsHookNil(t *testing.T) {
	if hook := buildSettingsHook(nil); hook != nil {
		t.Fatalf("expected nil hook, got %+v", hook)
	}
	if hook := buildSettingsHook(&config.Settings{Hooks: &config.HooksConfig{}}); hook != nil {
		t.Fatalf("expected nil hook for empty config, got %+v", hook)
	}
}

func TestSettingsHookRunAppliesEnv(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "env.txt")
	settings := &config.Settings{
		Env: map[string]string{"HOOKVAR": "expected"},
		Hooks: &config.HooksConfig{
			PreToolUse: map[string]string{"echo": fmt.Sprintf("printf '%%s' \"$HOOKVAR\" > %s", outFile)},
		},
	}
	hook := buildSettingsHook(settings)
	if hook == nil {
		t.Fatal("hook not built")
	}
	if err := hook.PreToolUse(context.Background(), coreevents.ToolUsePayload{Name: "echo"}); err != nil {
		t.Fatalf("pre tool use failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "expected" {
		t.Fatalf("env not propagated, got %q", string(data))
	}
}

func TestSettingsHookRunReturnsCommandError(t *testing.T) {
	settings := &config.Settings{
		Hooks: &config.HooksConfig{
			PreToolUse: map[string]string{"fail": "exit 7"},
		},
	}
	hook := buildSettingsHook(settings)
	if err := hook.PreToolUse(context.Background(), coreevents.ToolUsePayload{Name: "fail"}); err == nil {
		t.Fatal("expected command failure to propagate")
	}
}

func TestHooksDisabledFlag(t *testing.T) {
	disabled := true
	if !hooksDisabled(&config.Settings{DisableAllHooks: &disabled}) {
		t.Fatal("expected hooks disabled")
	}
}

func TestFormatEnvProducesPairs(t *testing.T) {
	env := formatEnv(map[string]string{"K": "V"})
	if len(env) != 1 || env[0] != "K=V" {
		t.Fatalf("unexpected env formatting: %+v", env)
	}
}

func TestSettingsHookPostToolUse(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "post.txt")
	settings := &config.Settings{
		Hooks: &config.HooksConfig{
			PostToolUse: map[string]string{"echo": fmt.Sprintf("echo done > %s", outFile)},
		},
	}
	hook := buildSettingsHook(settings)
	if hook == nil {
		t.Fatal("hook not built")
	}
	if err := hook.PostToolUse(context.Background(), coreevents.ToolResultPayload{Name: "echo"}); err != nil {
		t.Fatalf("post tool use error: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) == "" {
		t.Fatal("expected post hook to run")
	}
}

//go:build integration
// +build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSettingsLoaderProjectLocalPrecedence(t *testing.T) {
	projectRoot := t.TempDir()

	writeFile(t, filepath.Join(projectRoot, ".agents", "settings.json"), `{
        "model": "claude-3-opus-latest",
        "env": {
            "PROJECT_ONLY": "1",
            "SHARED": "project"
        },
        "permissions": {
            "allow": ["Bash(ls:*)"],
            "defaultMode": "plan"
        }
    }`)

	writeFile(t, filepath.Join(projectRoot, ".agents", "settings.local.json"), `{
        "model": "claude-3-haiku-latest",
        "env": {
            "SHARED": "local",
            "LOCAL_ONLY": "1"
        },
        "permissions": {
            "ask": ["Bash(cat:*)"],
            "defaultMode": "bypassPermissions"
        }
    }`)

	loader := config.SettingsLoader{ProjectRoot: projectRoot}
	settings, err := loader.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	if settings.Model != "claude-3-haiku-latest" {
		t.Fatalf("expected local settings to override model, got %q", settings.Model)
	}

	expectedEnv := map[string]string{
		"PROJECT_ONLY": "1",
		"SHARED":       "local",
		"LOCAL_ONLY":   "1",
	}
	if len(settings.Env) != len(expectedEnv) {
		t.Fatalf("unexpected env size: got %d, want %d", len(settings.Env), len(expectedEnv))
	}
	for k, v := range expectedEnv {
		if settings.Env[k] != v {
			t.Fatalf("env[%s]=%q, want %q", k, settings.Env[k], v)
		}
	}

	if settings.Permissions == nil {
		t.Fatalf("permissions should be populated")
	}
	if settings.Permissions.DefaultMode != "bypassPermissions" {
		t.Fatalf("expected local defaultMode override, got %q", settings.Permissions.DefaultMode)
	}
	if len(settings.Permissions.Allow) != 1 || settings.Permissions.Allow[0] != "Bash(ls:*)" {
		t.Fatalf("project allow rules not preserved: %+v", settings.Permissions.Allow)
	}
	if len(settings.Permissions.Ask) != 1 || settings.Permissions.Ask[0] != "Bash(cat:*)" {
		t.Fatalf("local ask rules not applied: %+v", settings.Permissions.Ask)
	}
}

func TestSkillsLoadProjectOnly(t *testing.T) {
	projectRoot := t.TempDir()
	fakeHome := t.TempDir()

	writeFile(t, filepath.Join(projectRoot, ".agents", "skills", "project-skill", "SKILL.md"), `---
name: project-skill
description: project scoped skill
allowed-tools: bash
---
# skill body
`)

	writeFile(t, filepath.Join(fakeHome, ".agents", "skills", "user-skill", "SKILL.md"), `---
name: user-skill
description: should be ignored
allowed-tools: bash
---
# user skill
`)

	opts := skills.LoaderOptions{
		ProjectRoot: projectRoot,
		UserHome:    fakeHome,
		EnableUser:  true,
	}
	regs, errs := skills.LoadFromFS(opts)
	if len(errs) != 0 {
		t.Fatalf("unexpected skill loader errors: %v", errs)
	}

	if len(regs) != 1 {
		t.Fatalf("expected only project skills to load, got %d", len(regs))
	}
	if regs[0].Definition.Name != "project-skill" {
		t.Fatalf("loaded skill %q, want project-skill", regs[0].Definition.Name)
	}
}

func TestSubagentLoadProjectOnly(t *testing.T) {
	projectRoot := t.TempDir()
	fakeHome := t.TempDir()

	writeFile(t, filepath.Join(projectRoot, ".agents", "agents", "project-agent.md"), `---
name: project-agent
description: project scoped agent
tools: bash
model: sonnet
permissionMode: default
skills: helper
---
# body
`)

	writeFile(t, filepath.Join(fakeHome, ".agents", "agents", "user-agent.md"), `---
name: user-agent
description: should be ignored
tools: bash
model: sonnet
permissionMode: default
skills: helper
---
# user agent
`)

	opts := subagents.LoaderOptions{
		ProjectRoot: projectRoot,
		UserHome:    fakeHome,
		EnableUser:  true,
	}
	regs, errs := subagents.LoadFromFS(opts)
	if len(errs) != 0 {
		t.Fatalf("unexpected subagent loader errors: %v", errs)
	}

	if len(regs) != 1 {
		t.Fatalf("expected only project agents to load, got %d", len(regs))
	}
	if regs[0].Definition.Name != "project-agent" {
		t.Fatalf("loaded agent %q, want project-agent", regs[0].Definition.Name)
	}
}

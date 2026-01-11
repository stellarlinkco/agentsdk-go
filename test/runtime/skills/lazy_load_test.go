package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
)

func TestLazyLoadViaRegistry(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "skills", "ext")

	writeSkill(t, filepath.Join(dir, "SKILL.md"), "ext", "body from registry")

	regs, errs := skills.LoadFromFS(skills.LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 {
		t.Fatalf("unexpected load errs: %v", errs)
	}

	registry := skills.NewRegistry()
	for _, reg := range regs {
		if err := registry.Register(reg.Definition, reg.Handler); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	updatedBody := "body loaded lazily"
	writeSkill(t, filepath.Join(dir, "SKILL.md"), "ext", updatedBody)

	res, err := registry.Execute(context.Background(), "ext", skills.ActivationContext{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	output, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type: %T", res.Output)
	}
	if output["body"] != updatedBody {
		t.Fatalf("expected lazy body %q, got %#v", updatedBody, output["body"])
	}

	// Hot-reload: file changes should trigger reload on next execute
	// Sleep to ensure mtime changes on filesystems with 1s granularity
	time.Sleep(1100 * time.Millisecond)
	hotReloadBody := "body after first execute"
	writeSkill(t, filepath.Join(dir, "SKILL.md"), "ext", hotReloadBody)
	resReloaded, err := registry.Execute(context.Background(), "ext", skills.ActivationContext{})
	if err != nil {
		t.Fatalf("execute reloaded: %v", err)
	}
	reloadedOutput, ok := resReloaded.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected reloaded output type: %T", resReloaded.Output)
	}
	if reloadedOutput["body"] != hotReloadBody {
		t.Fatalf("expected hot-reloaded body %q, got %#v", hotReloadBody, reloadedOutput["body"])
	}
}

func TestLazyLoadErrorPropagates(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "skills", "err")
	writeSkill(t, filepath.Join(dir, "SKILL.md"), "err", "body")

	regs, errs := skills.LoadFromFS(skills.LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 {
		t.Fatalf("unexpected load errs: %v", errs)
	}

	if err := os.Remove(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("remove skill: %v", err)
	}

	registry := skills.NewRegistry()
	if err := registry.Register(regs[0].Definition, regs[0].Handler); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := registry.Execute(context.Background(), "err", skills.ActivationContext{}); err == nil {
		t.Fatalf("expected execute error")
	}
}

// writeSkill duplicates the helper from pkg/runtime/skills for external tests.
func writeSkill(t *testing.T, path, name, body string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: desc\n---\n" + body
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

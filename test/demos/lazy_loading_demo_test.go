//go:build demo
// +build demo

package demos

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
)

func TestSkillLazyLoadingDemo(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.TraceSessionIDContextKey, "lazy-loading-demo")

	root := t.TempDir()
	skillBodies := map[string]string{
		"alpha": "alpha body content",
		"beta":  "beta body content",
		"gamma": "gamma body content",
	}
	for name, body := range skillBodies {
		writeDemoSkill(t, root, name, body)
	}

	registrations, errs := skills.LoadFromFS(skills.LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 {
		t.Fatalf("load skills: %v", errs)
	}
	if len(registrations) != len(skillBodies) {
		t.Fatalf("expected %d skills, got %d", len(skillBodies), len(registrations))
	}

	registry := skills.NewRegistry()
	for _, reg := range registrations {
		if err := registry.Register(reg.Definition, reg.Handler); err != nil {
			t.Fatalf("register %s: %v", reg.Definition.Name, err)
		}
	}

	names := []string{"alpha", "beta", "gamma"}
	traceDir := t.TempDir()
	tracer := middleware.NewTraceMiddleware(traceDir, middleware.WithSkillTracing(true))
	state := &middleware.State{
		Iteration: 1,
		Values: map[string]any{
			"skills.registry":      registry,
			"request.force_skills": names,
		},
	}

	if err := tracer.BeforeAgent(ctx, state); err != nil {
		t.Fatalf("BeforeAgent: %v", err)
	}
	logBodies(t, "before", registry, names)
	for _, name := range names {
		if size, loaded := bodyLength(t, registry, name); loaded || size != 0 {
			t.Fatalf("skill %s should be unloaded before execute, got size=%d loaded=%t", name, size, loaded)
		}
	}

	target := "beta"
	if _, err := registry.Execute(ctx, target, skills.ActivationContext{}); err != nil {
		t.Fatalf("execute %s: %v", target, err)
	}
	t.Logf("executed %s to trigger lazy load", target)

	if err := tracer.AfterAgent(ctx, state); err != nil {
		t.Fatalf("AfterAgent: %v", err)
	}
	logBodies(t, "after", registry, names)

	for _, name := range names {
		size, loaded := bodyLength(t, registry, name)
		if name == target {
			if !loaded || size != len(skillBodies[name]) {
				t.Fatalf("skill %s expected loaded with size %d, got size=%d loaded=%t", name, len(skillBodies[name]), size, loaded)
			}
			continue
		}
		if loaded || size != 0 {
			t.Fatalf("skill %s should remain lazy-loaded, got size=%d loaded=%t", name, size, loaded)
		}
	}
	t.Logf("trace output directory: %s", traceDir)
}

func writeDemoSkill(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, ".agents", "skills", name, "SKILL.md")
	content := strings.Join([]string{
		"---",
		"name: " + name,
		"description: demo " + name,
		"---",
		body,
	}, "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func bodyLength(t *testing.T, registry *skills.Registry, name string) (int, bool) {
	t.Helper()
	skill, ok := registry.Get(name)
	if !ok {
		t.Fatalf("missing skill %s", name)
	}
	sizer, ok := skill.Handler().(interface {
		BodyLength() (int, bool)
	})
	if !ok {
		t.Fatalf("handler for %s lacks BodyLength", name)
	}
	return sizer.BodyLength()
}

func logBodies(t *testing.T, stage string, registry *skills.Registry, names []string) {
	t.Helper()
	for _, name := range names {
		size, loaded := bodyLength(t, registry, name)
		t.Logf("[%s] skill=%s loaded=%t body_len=%d", stage, name, loaded, size)
	}
}

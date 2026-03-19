package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
)

func TestSkillsAndMatchers(t *testing.T) {
	regs := buildSkills()
	if len(regs) < 3 {
		t.Fatalf("expected skills")
	}

	ac := skills.ActivationContext{
		Prompt:   "incident log error",
		Channels: []string{"cli"},
		Tags:     map[string]string{"env": "prod", "severity": "high"},
		Metadata: map[string]any{"request_id": "r1"},
		Traits:   []string{"fast"},
	}

	for _, reg := range regs {
		def := reg.Definition
		if def.Name == "" || reg.Handler == nil {
			t.Fatalf("bad skill registration: %+v", def)
		}
		for _, m := range def.Matchers {
			_ = m.Match(ac)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	for _, reg := range regs {
		_, _ = reg.Handler.Execute(ctx, ac)
	}

	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	for _, reg := range regs {
		if _, err := reg.Handler.Execute(cancelled, ac); err == nil {
			t.Fatalf("expected ctx error for skill %s", reg.Definition.Name)
		}
	}
}

func TestSubagentsHandlers(t *testing.T) {
	regs := buildSubagents()
	if len(regs) < 4 {
		t.Fatalf("expected subagents")
	}

	req := subagents.Request{Instruction: "deploy to prod"}
	subCtx := subagents.Context{Model: "", ToolWhitelist: []string{"bash", "read"}}

	ctx := context.Background()
	if _, err := generalPurposeHandler(ctx, subCtx, req); err != nil {
		t.Fatalf("generalPurposeHandler: %v", err)
	}
	if _, err := exploreHandler(ctx, subCtx, req); err != nil {
		t.Fatalf("exploreHandler: %v", err)
	}
	if _, err := planHandler(ctx, subCtx, req); err != nil {
		t.Fatalf("planHandler: %v", err)
	}
	if _, err := deployGuardHandler(ctx, subCtx, req); err != nil {
		t.Fatalf("deployGuardHandler: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := generalPurposeHandler(cancelled, subCtx, req); err == nil {
		t.Fatalf("expected ctx error")
	}

	if got := preferModel(subagents.Context{Model: "x"}, "y"); got != "x" {
		t.Fatalf("preferModel=%q", got)
	}
	if got := preferModel(subagents.Context{}, "y"); got != "y" {
		t.Fatalf("preferModel=%q", got)
	}
}

func TestMCPAndSandboxHelpers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if needsUVX("stdio://uvx mcp-server-time") != true {
		t.Fatalf("needsUVX should be true")
	}
	if needsUVX("stdio://echo mcp") {
		t.Fatalf("needsUVX should be false")
	}

	cfg := runConfig{enableMCP: false}
	if got := buildMCPServers(cfg, logger); got != nil {
		t.Fatalf("expected nil, got=%v", got)
	}
	cfg = runConfig{enableMCP: true, mcpServer: "stdio://echo mcp"}
	got := buildMCPServers(cfg, logger)
	if len(got) != 1 {
		t.Fatalf("expected one server, got=%v", got)
	}

	_ = binaryAvailable("definitely-not-a-binary-xyz")

	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	off := buildSandboxOptions(runConfig{enableSandbox: false}, root)
	if off.Root != "" || len(off.AllowedPaths) != 0 {
		t.Fatalf("unexpected sandbox options: %+v", off)
	}
	on := buildSandboxOptions(runConfig{
		enableSandbox: true,
		sandboxRoot:   filepath.Join(root, "sandbox"),
		allowHost:     "example.com",
		cpuLimit:      1,
		memLimit:      2,
		diskLimit:     3,
	}, root)
	if on.Root == "" || len(on.AllowedPaths) == 0 || len(on.NetworkAllow) != 1 {
		t.Fatalf("unexpected sandbox options: %+v", on)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

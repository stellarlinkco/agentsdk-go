package api

import (
	"context"
	"errors"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/config"
	coreevents "github.com/cexll/agentsdk-go/pkg/core/events"
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
	"github.com/cexll/agentsdk-go/pkg/sandbox"
)

func TestRemoveCommandLines(t *testing.T) {
	prompt := "/tag foo=bar\ncontent"
	inv, err := commands.Parse(prompt)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	clean := removeCommandLines(prompt, inv)
	if clean != "content" {
		t.Fatalf("unexpected clean prompt: %s", clean)
	}
}

func TestApplyPromptMetadata(t *testing.T) {
	meta := map[string]any{"api.prepend_prompt": "intro", "api.append_prompt": "outro"}
	result := applyPromptMetadata("body", meta)
	if result != "intro\nbody\noutro" {
		t.Fatalf("metadata merge failed: %s", result)
	}
}

func TestOrderedForcedSkills(t *testing.T) {
	reg := skills.NewRegistry()
	_ = reg.Register(skills.Definition{Name: "alpha"}, skills.HandlerFunc(func(context.Context, skills.ActivationContext) (skills.Result, error) {
		return skills.Result{}, nil
	}))
	activations := orderedForcedSkills(reg, []string{"alpha", "missing"})
	if len(activations) != 1 {
		t.Fatalf("expected one activation")
	}
}

func TestEnforceSandboxHostNoManager(t *testing.T) {
	if err := enforceSandboxHost(nil, "http://example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnforceSandboxHostDenied(t *testing.T) {
	mgr := sandbox.NewManager(nil, sandbox.NewDomainAllowList("allowed.example"), nil)
	if err := enforceSandboxHost(mgr, "http://bad.example"); err == nil {
		t.Fatal("expected host denial")
	}
}

func TestSnapshotSandboxEmpty(t *testing.T) {
	report := snapshotSandbox(nil)
	if report.ResourceLimits != (sandbox.ResourceLimits{}) {
		t.Fatalf("unexpected limits: %+v", report.ResourceLimits)
	}
}

func TestBuildSandboxManager(t *testing.T) {
	cfg := &config.ProjectConfig{Sandbox: config.SandboxBlock{AllowedPaths: []string{"workspace"}}}
	opts := Options{ProjectRoot: t.TempDir(), Sandbox: SandboxOptions{AllowedPaths: []string{"extra"}, ResourceLimit: sandbox.ResourceLimits{MaxCPUPercent: 10}}}
	mgr, root := buildSandboxManager(opts, cfg)
	if root == "" {
		t.Fatal("expected non-empty root")
	}
	if err := mgr.CheckPath("workspace/file"); err != nil {
		t.Fatalf("expected workspace allowed: %v", err)
	}
	limits := mgr.Limits()
	if limits.MaxCPUPercent != 10 {
		t.Fatalf("unexpected limits: %+v", limits)
	}
}

func TestStringSlice(t *testing.T) {
	values := stringSlice([]any{"a", "b"})
	if len(values) != 2 {
		t.Fatalf("unexpected values: %+v", values)
	}
	values = stringSlice("single")
	if len(values) != 1 || values[0] != "single" {
		t.Fatalf("conversion failed: %+v", values)
	}
	values = stringSlice([]string{"c", "a"})
	if len(values) != 2 || values[0] != "a" || values[1] != "c" {
		t.Fatalf("unexpected sorted slice: %+v", values)
	}
}

func TestApplyCommandMetadata(t *testing.T) {
	req := &Request{}
	meta := map[string]any{"api.target_subagent": "ops", "api.tool_whitelist": []any{"a", "b"}}
	applyCommandMetadata(req, meta)
	if req.TargetSubagent != "ops" || len(req.ToolWhitelist) != 2 {
		t.Fatalf("metadata not applied: %+v", req)
	}
}

func TestCombineAndPrependPrompt(t *testing.T) {
	combined := combinePrompt("existing", "extra")
	if combined == "existing" {
		t.Fatal("combine prompt failed")
	}
	prepended := prependPrompt("body", "intro")
	if prepended[:5] != "intro" {
		t.Fatal("prepend failed")
	}
	if kept := prependPrompt("body", "   "); kept != "body" {
		t.Fatalf("expected body unchanged, got %q", kept)
	}
}

func TestAnyToString(t *testing.T) {
	if val, ok := anyToString(nil); ok || val != "" {
		t.Fatal("expected empty")
	}
	val, ok := anyToString(123)
	if !ok || val == "" {
		t.Fatal("conversion failed")
	}
}

func TestOptionsModeContext(t *testing.T) {
	opts := Options{}
	mode := opts.modeContext()
	if mode.EntryPoint != defaultEntrypoint {
		t.Fatalf("unexpected default entrypoint: %v", mode.EntryPoint)
	}
}

func TestActivationContext(t *testing.T) {
	req := Request{Prompt: "p", Tags: map[string]string{"k": "v"}, Metadata: map[string]any{"m": "v"}, Channels: []string{"cli"}}
	act := req.activationContext("prompt")
	if act.Prompt != "prompt" || len(act.Tags) != 1 {
		t.Fatalf("unexpected activation: %+v", act)
	}
}

func TestDefaultSessionID(t *testing.T) {
	if id := defaultSessionID(EntryPointCI); id == "" {
		t.Fatal("session ID empty")
	}
}

func TestMergeTagsUtility(t *testing.T) {
	req := &Request{Tags: map[string]string{"existing": "x"}}
	meta := map[string]any{"api.tags": map[string]any{"new": "y"}}
	mergeTags(req, meta)
	if req.Tags["new"] != "y" {
		t.Fatalf("tags not merged: %+v", req.Tags)
	}
}

func TestModelFactoryFuncModel(t *testing.T) {
	called := false
	fn := ModelFactoryFunc(func(context.Context) (model.Model, error) {
		called = true
		return &stubModel{}, nil
	})
	m, err := fn.Model(context.Background())
	if err != nil || m == nil || !called {
		t.Fatalf("factory not invoked correctly: m=%v err=%v called=%v", m, err, called)
	}
}

func TestModelFactoryFuncNil(t *testing.T) {
	var fn ModelFactoryFunc
	if _, err := fn.Model(context.Background()); !errors.Is(err, ErrMissingModel) {
		t.Fatalf("expected ErrMissingModel, got %v", err)
	}
}

func TestRuntimeHookAdapterRecordsEvents(t *testing.T) {
	rec := defaultHookRecorder()
	exec := newHookExecutor(Options{}, rec)
	adapter := &runtimeHookAdapter{executor: exec, recorder: rec}

	if err := adapter.PreToolUse(context.Background(), coreevents.ToolUsePayload{Name: "t"}); err != nil {
		t.Fatalf("pre: %v", err)
	}
	if err := adapter.PostToolUse(context.Background(), coreevents.ToolResultPayload{Name: "t"}); err != nil {
		t.Fatalf("post: %v", err)
	}
	if err := adapter.UserPrompt(context.Background(), "p"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if err := adapter.Stop(context.Background(), "reason"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	events := rec.Drain()
	if len(events) == 0 {
		t.Fatal("expected recorded events")
	}
	foundStop := false
	for _, e := range events {
		if e.Type == coreevents.Stop {
			foundStop = true
			break
		}
	}
	if !foundStop {
		t.Fatalf("expected stop event in %+v", events)
	}
	if len(rec.Drain()) != 0 {
		t.Fatal("expected drained recorder to be empty")
	}
}

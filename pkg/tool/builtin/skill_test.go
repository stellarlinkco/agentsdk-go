package toolbuiltin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
)

func TestSkillToolExecutesSkill(t *testing.T) {
	reg := skills.NewRegistry()
	err := reg.Register(skills.Definition{Name: "codex"}, skills.HandlerFunc(func(ctx context.Context, ac skills.ActivationContext) (skills.Result, error) {
		return skills.Result{Skill: "codex", Output: strings.Join(ac.Channels, ",")}, nil
	}))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tool := NewSkillTool(reg, func(ctx context.Context) skills.ActivationContext {
		return skills.ActivationContext{Channels: []string{"cli"}}
	})
	res, err := tool.Execute(context.Background(), map[string]interface{}{"command": "codex"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(res.Output) != "cli" {
		t.Fatalf("unexpected output: %q", res.Output)
	}
}

func TestSkillToolUnknownSkill(t *testing.T) {
	tool := NewSkillTool(skills.NewRegistry(), nil)
	_, err := tool.Execute(context.Background(), map[string]interface{}{"command": "missing"})
	if err == nil || !errors.Is(err, skills.ErrUnknownSkill) {
		t.Fatalf("expected ErrUnknownSkill, got %v", err)
	}
}

func TestSkillToolValidatesInput(t *testing.T) {
	reg := skills.NewRegistry()
	tool := NewSkillTool(reg, nil)
	if _, err := tool.Execute(context.Background(), map[string]interface{}{}); err == nil {
		t.Fatalf("expected error for missing command")
	}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"command": "   "}); err == nil {
		t.Fatalf("expected error for blank command")
	}
	if _, err := tool.Execute(nil, map[string]interface{}{"command": "codex"}); err == nil {
		t.Fatalf("expected context error")
	}
}

func TestSkillToolMetadataAndContextHelpers(t *testing.T) {
	reg := skills.NewRegistry()
	tool := NewSkillTool(reg, nil)
	if tool.Name() != "skill" {
		t.Fatalf("unexpected name %q", tool.Name())
	}
	if tool.Description() == "" || tool.Schema() == nil {
		t.Fatalf("description/schema missing")
	}
	ac := skills.ActivationContext{Prompt: "hi"}
	ctx := WithSkillActivationContext(context.Background(), ac)
	if extracted, ok := SkillActivationContextFromContext(ctx); !ok || extracted.Prompt != "hi" {
		t.Fatalf("expected activation context, got %+v", extracted)
	}
	if extracted, ok := SkillActivationContextFromContext(context.Background()); ok || extracted.Prompt != "" {
		t.Fatalf("unexpected activation context for nil")
	}

	tool = &SkillTool{}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"command": "codex"}); err == nil {
		t.Fatalf("expected registry error")
	}
}

func TestSkillToolDefaultActivationProviderUsesContext(t *testing.T) {
	reg := skills.NewRegistry()
	err := reg.Register(skills.Definition{Name: "echo"}, skills.HandlerFunc(func(ctx context.Context, ac skills.ActivationContext) (skills.Result, error) {
		return skills.Result{Skill: "echo", Output: ac.Prompt}, nil
	}))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tool := NewSkillTool(reg, nil)
	ctx := WithSkillActivationContext(context.Background(), skills.ActivationContext{Prompt: "from-context"})
	res, err := tool.Execute(ctx, map[string]interface{}{"command": "ECHO"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Output != "from-context" {
		t.Fatalf("expected context prompt, got %v", res.Output)
	}
}

type skillStringer struct{}

func (skillStringer) String() string { return "stringer-output" }

func TestFormatSkillOutputVariants(t *testing.T) {
	tests := []struct {
		name   string
		result skills.Result
		want   string
	}{
		{name: "string", result: skills.Result{Skill: "a", Output: "hello"}, want: "hello"},
		{name: "stringer", result: skills.Result{Skill: "a", Output: skillStringer{}}, want: "stringer-output"},
		{name: "map", result: skills.Result{Skill: "a", Output: map[string]string{"k": "v"}}, want: `{"k":"v"}`},
		{name: "empty", result: skills.Result{Skill: "a", Output: ""}, want: "skill a executed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSkillOutput(tc.result); !strings.Contains(got, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, got)
			}
		})
	}
}

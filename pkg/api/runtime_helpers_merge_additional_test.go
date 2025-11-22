package api

import (
	"context"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
	"github.com/cexll/agentsdk-go/pkg/runtime/subagents"
)

func TestMergeSubagentRegistrationsValidatesAndOverrides(t *testing.T) {
	var errs []error
	user := []subagents.SubagentRegistration{
		{
			Definition: subagents.Definition{Name: "alpha"},
			Handler: subagents.HandlerFunc(func(context.Context, subagents.Context, subagents.Request) (subagents.Result, error) {
				return subagents.Result{Output: "user"}, nil
			}),
		},
		{
			Definition: subagents.Definition{Name: ""},
			Handler: subagents.HandlerFunc(func(context.Context, subagents.Context, subagents.Request) (subagents.Result, error) {
				return subagents.Result{}, nil
			}),
		},
	}
	manual := []SubagentRegistration{{Definition: subagents.Definition{Name: "alpha"}, Handler: nil}}
	project := []subagents.SubagentRegistration{
		{
			Definition: subagents.Definition{Name: "alpha"},
			Handler: subagents.HandlerFunc(func(context.Context, subagents.Context, subagents.Request) (subagents.Result, error) {
				return subagents.Result{Output: "project"}, nil
			}),
		},
	}

	merged := mergeSubagentRegistrations(user, manual, project, &errs)
	if len(errs) != 2 {
		t.Fatalf("expected two validation errors, got %d", len(errs))
	}
	if len(merged) != 1 {
		t.Fatalf("expected single merged entry, got %d", len(merged))
	}
	res, err := merged[0].Handler.Handle(context.Background(), subagents.Context{}, subagents.Request{})
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if res.Output != "project" {
		t.Fatalf("expected project handler to override, got %q", res.Output)
	}
}

func TestMergeSkillRegistrationsValidatesAndOverrides(t *testing.T) {
	var errs []error
	loader := []skills.SkillRegistration{
		{
			Definition: skills.Definition{Name: "echo"},
			Handler: skills.HandlerFunc(func(context.Context, skills.ActivationContext) (skills.Result, error) {
				return skills.Result{Output: "fs"}, nil
			}),
		},
		{
			Definition: skills.Definition{Name: ""},
			Handler: skills.HandlerFunc(func(context.Context, skills.ActivationContext) (skills.Result, error) {
				return skills.Result{}, nil
			}),
		},
	}
	manual := []SkillRegistration{
		{Definition: skills.Definition{Name: "echo"}, Handler: nil},
		{
			Definition: skills.Definition{Name: "echo"},
			Handler: skills.HandlerFunc(func(context.Context, skills.ActivationContext) (skills.Result, error) {
				return skills.Result{Output: "manual"}, nil
			}),
		},
	}

	merged := mergeSkillRegistrations(loader, manual, &errs)
	if len(errs) != 2 {
		t.Fatalf("expected two validation errors, got %d", len(errs))
	}
	if len(merged) != 1 {
		t.Fatalf("expected single merged entry, got %d", len(merged))
	}
	res, err := merged[0].Handler.Execute(context.Background(), skills.ActivationContext{})
	if err != nil {
		t.Fatalf("handler execute failed: %v", err)
	}
	if res.Output != "manual" {
		t.Fatalf("expected manual handler to override, got %q", res.Output)
	}
}

func TestMergeCommandRegistrationsValidatesAndOverrides(t *testing.T) {
	var errs []error
	fs := []commands.CommandRegistration{
		{
			Definition: commands.Definition{Name: "ping"},
			Handler: commands.HandlerFunc(func(context.Context, commands.Invocation) (commands.Result, error) {
				return commands.Result{Output: "fs"}, nil
			}),
		},
		{
			Definition: commands.Definition{Name: ""},
			Handler: commands.HandlerFunc(func(context.Context, commands.Invocation) (commands.Result, error) {
				return commands.Result{}, nil
			}),
		},
	}
	manual := []CommandRegistration{
		{Definition: commands.Definition{Name: "ping"}, Handler: nil},
		{
			Definition: commands.Definition{Name: "ping"},
			Handler: commands.HandlerFunc(func(context.Context, commands.Invocation) (commands.Result, error) {
				return commands.Result{Output: "manual"}, nil
			}),
		},
	}

	merged := mergeCommandRegistrations(fs, manual, &errs)
	if len(errs) != 2 {
		t.Fatalf("expected two validation errors, got %d", len(errs))
	}
	if len(merged) != 1 {
		t.Fatalf("expected single merged command, got %d", len(merged))
	}
	res, err := merged[0].Handler.Handle(context.Background(), commands.Invocation{})
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if res.Output != "manual" {
		t.Fatalf("expected manual handler to override, got %q", res.Output)
	}
}

func TestBuildSubagentsManagerHandlesEmptyAndManual(t *testing.T) {
	emptyOpts := Options{ProjectRoot: t.TempDir()}
	mgr, errs := buildSubagentsManager(emptyOpts)
	if mgr != nil {
		t.Fatalf("expected nil manager when no registrations, got %#v", mgr)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no loader errors, got %v", errs)
	}

	opts := Options{
		ProjectRoot: t.TempDir(),
		Subagents: []SubagentRegistration{{
			Definition: subagents.Definition{Name: "manual"},
			Handler: subagents.HandlerFunc(func(context.Context, subagents.Context, subagents.Request) (subagents.Result, error) {
				return subagents.Result{Output: "manual-ok"}, nil
			}),
		}},
	}
	mgr, errs = buildSubagentsManager(opts)
	if len(errs) != 0 {
		t.Fatalf("unexpected build errors: %v", errs)
	}
	if mgr == nil {
		t.Fatal("expected manager to be constructed from manual registration")
	}
	res, err := mgr.Dispatch(subagents.WithTaskDispatch(context.Background()), subagents.Request{
		Target:      "manual",
		Instruction: "do it",
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if res.Output != "manual-ok" {
		t.Fatalf("unexpected dispatch output: %q", res.Output)
	}
}

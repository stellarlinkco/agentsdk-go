package api

import (
	"context"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
)

func TestRuntimeAvailableCommandsSnapshot(t *testing.T) {
	t.Parallel()

	root := newClaudeProject(t)
	mdl := &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", Content: "ok"}}}}
	rt, err := New(context.Background(), Options{
		ProjectRoot: root,
		Model:       mdl,
		Commands: []CommandRegistration{
			{
				Definition: commands.Definition{Name: "plan", Description: "Create a plan", Priority: 10},
				Handler: commands.HandlerFunc(func(context.Context, commands.Invocation) (commands.Result, error) {
					return commands.Result{}, nil
				}),
			},
			{
				Definition: commands.Definition{Name: "review", Description: "Review code", Priority: 5},
				Handler: commands.HandlerFunc(func(context.Context, commands.Invocation) (commands.Result, error) {
					return commands.Result{}, nil
				}),
			},
		},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	got := rt.AvailableCommands()
	if len(got) < 2 {
		t.Fatalf("available commands len=%d, want >=2", len(got))
	}
	if got[0].Name != "plan" || got[1].Name != "review" {
		t.Fatalf("unexpected command ordering: %+v", got)
	}
	if got[0].Description != "Create a plan" {
		t.Fatalf("plan description=%q, want %q", got[0].Description, "Create a plan")
	}
}

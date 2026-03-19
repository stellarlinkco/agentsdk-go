package api

import (
	"context"
	"slices"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestEnabledBuiltinToolKeys(t *testing.T) {
	t.Parallel()

	defaults := EnabledBuiltinToolKeys(Options{})
	for _, want := range []string{"bash", "read", "write", "edit", "glob", "grep", "skill"} {
		if !slices.Contains(defaults, want) {
			t.Fatalf("default builtins missing %q in %v", want, defaults)
		}
	}

	filtered := EnabledBuiltinToolKeys(Options{EnabledBuiltinTools: []string{"WRITE", "bash"}})
	if len(filtered) != 2 || filtered[0] != "bash" || filtered[1] != "write" {
		t.Fatalf("filtered builtins=%v, want [bash write]", filtered)
	}

	disabled := EnabledBuiltinToolKeys(Options{EnabledBuiltinTools: []string{}})
	if len(disabled) != 0 {
		t.Fatalf("disabled builtins=%v, want empty", disabled)
	}
}

func TestRuntimeAvailableToolsFromRegistry(t *testing.T) {
	t.Parallel()

	root := newClaudeProject(t)
	mdl := &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", Content: "ok"}}}}
	rt, err := New(context.Background(), Options{
		ProjectRoot: root,
		Model:       mdl,
		EnabledBuiltinTools: []string{
			"read",
			"write",
			"bash",
		},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	defs := rt.AvailableTools()
	if len(defs) == 0 {
		t.Fatalf("expected non-empty available tools")
	}

	seen := map[string]struct{}{}
	for _, def := range defs {
		seen[canonicalToolName(def.Name)] = struct{}{}
	}
	for _, want := range []string{"read", "write", "bash"} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("missing tool %q in %+v", want, defs)
		}
	}
}

func TestRuntimeAvailableToolsForWhitelist(t *testing.T) {
	t.Parallel()

	root := newClaudeProject(t)
	mdl := &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", Content: "ok"}}}}
	rt, err := New(context.Background(), Options{
		ProjectRoot:         root,
		Model:               mdl,
		EnabledBuiltinTools: []string{"read", "write", "bash"},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	defs := rt.AvailableToolsForWhitelist([]string{"READ"})
	if len(defs) != 1 || canonicalToolName(defs[0].Name) != "read" {
		t.Fatalf("unexpected whitelisted defs: %+v", defs)
	}
}

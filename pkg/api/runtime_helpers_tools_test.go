package api

import (
	"context"
	"slices"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/model"
)

func TestEnabledBuiltinToolKeys(t *testing.T) {
	t.Parallel()

	defaults := EnabledBuiltinToolKeys(Options{})
	for _, want := range []string{"bash", "file_read", "file_write"} {
		if !slices.Contains(defaults, want) {
			t.Fatalf("default builtins missing %q in %v", want, defaults)
		}
	}

	filtered := EnabledBuiltinToolKeys(Options{EnabledBuiltinTools: []string{"FILE_WRITE", "bash"}})
	if len(filtered) != 2 || filtered[0] != "bash" || filtered[1] != "file_write" {
		t.Fatalf("filtered builtins=%v, want [bash file_write]", filtered)
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
			"task_create",
			"task_list",
			"task_get",
			"task_update",
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
		seen[def.Name] = struct{}{}
	}
	for _, want := range []string{"TaskCreate", "TaskList", "TaskGet", "TaskUpdate", "Bash"} {
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
		EnabledBuiltinTools: []string{"task_create", "task_list", "bash"},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	defs := rt.AvailableToolsForWhitelist([]string{"TaskCreate"})
	if len(defs) != 1 || defs[0].Name != "TaskCreate" {
		t.Fatalf("unexpected whitelisted defs: %+v", defs)
	}
}

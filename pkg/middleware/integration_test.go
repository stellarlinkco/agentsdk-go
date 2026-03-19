package middleware

import (
	"context"
	"fmt"
	"testing"
)

func TestIntegrationChainAllStages(t *testing.T) {
	calls := []string{}
	record := func(label string) func(context.Context, *State) error {
		return func(_ context.Context, st *State) error {
			calls = append(calls, label)
			return nil
		}
	}

	chain := NewChain([]Middleware{
		Funcs{
			Identifier: "mutate",
			OnBeforeAgent: func(_ context.Context, st *State) error {
				st.Iteration = 42
				st.SetValue("agent", "ready")
				st.SetModelInput(map[string]any{"prompt": "ping"})
				return nil
			},
			OnBeforeTool: func(_ context.Context, st *State) error {
				st.ToolCall = map[string]any{"name": "probe"}
				return nil
			},
			OnAfterTool: func(_ context.Context, st *State) error {
				st.ToolResult = map[string]any{"ok": true}
				st.SetValue("after_tool", true)
				return nil
			},
			OnAfterAgent: func(_ context.Context, st *State) error {
				st.SetModelOutput(map[string]any{"content": "pong"})
				if st.Iteration != 42 || st.ModelOutput == nil || st.ToolResult == nil {
					return fmt.Errorf("state missing data: %#v", st)
				}
				return nil
			},
		},
		Funcs{
			Identifier:    "observer",
			OnBeforeAgent: record("observer.before_agent"),
			OnBeforeTool:  record("observer.before_tool"),
			OnAfterTool:   record("observer.after_tool"),
			OnAfterAgent:  record("observer.after_agent"),
		},
	})

	stages := []Stage{StageBeforeAgent, StageBeforeTool, StageAfterTool, StageAfterAgent}
	st := &State{}
	for _, stage := range stages {
		if err := chain.Execute(context.Background(), stage, st); err != nil {
			t.Fatalf("stage %v failed: %v", stage, err)
		}
	}

	expected := []string{
		"observer.before_agent",
		"observer.before_tool",
		"observer.after_tool",
		"observer.after_agent",
	}
	if len(calls) != len(expected) {
		t.Fatalf("call count mismatch: %d vs %d", len(calls), len(expected))
	}
	for i := range expected {
		if calls[i] != expected[i] {
			t.Fatalf("call order mismatch at %d: %s vs %s", i, calls[i], expected[i])
		}
	}
}

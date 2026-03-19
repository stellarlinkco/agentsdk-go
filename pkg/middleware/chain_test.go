package middleware

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type emptyName struct{}

func (emptyName) Name() string                              { return "" }
func (emptyName) BeforeAgent(context.Context, *State) error { return nil }
func (emptyName) BeforeTool(context.Context, *State) error  { return nil }
func (emptyName) AfterTool(context.Context, *State) error   { return nil }
func (emptyName) AfterAgent(context.Context, *State) error  { return nil }

func TestChainExecutionOrder(t *testing.T) {
	calls := []string{}

	record := func(label string) func(context.Context, *State) error {
		return func(_ context.Context, _ *State) error {
			calls = append(calls, label)
			return nil
		}
	}

	mw1 := Funcs{
		Identifier:    "mw1",
		OnBeforeAgent: record("mw1.before_agent"),
		OnAfterAgent:  record("mw1.after_agent"),
	}
	mw2 := Funcs{
		Identifier:    "mw2",
		OnBeforeAgent: record("mw2.before_agent"),
		OnAfterAgent:  record("mw2.after_agent"),
	}

	chain := NewChain([]Middleware{mw1, mw2})
	st := &State{}
	if err := chain.Execute(context.Background(), StageBeforeAgent, st); err != nil {
		t.Fatalf("before_agent error: %v", err)
	}
	if err := chain.Execute(context.Background(), StageAfterAgent, st); err != nil {
		t.Fatalf("after_agent error: %v", err)
	}

	expected := []string{
		"mw1.before_agent", "mw2.before_agent",
		"mw1.after_agent", "mw2.after_agent",
	}
	if len(calls) != len(expected) {
		t.Fatalf("unexpected call count: %d vs %d", len(calls), len(expected))
	}
	for i := range expected {
		if calls[i] != expected[i] {
			t.Fatalf("call[%d]=%s want %s", i, calls[i], expected[i])
		}
	}
}

func TestChainShortCircuitOnError(t *testing.T) {
	sentinel := errors.New("boom")
	mw1 := Funcs{
		Identifier: "mw1",
		OnBeforeTool: func(context.Context, *State) error {
			return sentinel
		},
	}
	called := false
	mw2 := Funcs{
		Identifier: "mw2",
		OnBeforeTool: func(context.Context, *State) error {
			called = true
			return nil
		},
	}
	chain := NewChain([]Middleware{mw1, mw2})
	err := chain.Execute(context.Background(), StageBeforeTool, &State{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if called {
		t.Fatalf("second middleware should not be executed after error")
	}
}

func TestChainUseAndAfterHooks(t *testing.T) {
	log := []string{}
	chain := NewChain(nil)
	chain.Use(Funcs{
		OnAfterTool: func(context.Context, *State) error {
			log = append(log, "after_tool")
			return nil
		},
		OnAfterAgent: func(context.Context, *State) error {
			log = append(log, "after_agent")
			return nil
		},
	})

	if err := chain.Execute(context.Background(), StageAfterTool, &State{}); err != nil {
		t.Fatalf("after_tool error: %v", err)
	}
	if err := chain.Execute(context.Background(), StageAfterAgent, &State{}); err != nil {
		t.Fatalf("after_agent error: %v", err)
	}
	expected := []string{"after_tool", "after_agent"}
	if !reflect.DeepEqual(log, expected) {
		t.Fatalf("log mismatch: %v", log)
	}
}

func TestChainTimeout(t *testing.T) {
	mw := Funcs{
		Identifier: "slow",
		OnBeforeAgent: func(ctx context.Context, _ *State) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
				return nil
			}
		},
	}
	chain := NewChain([]Middleware{mw}, WithTimeout(10*time.Millisecond))
	err := chain.Execute(context.Background(), StageBeforeAgent, &State{})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return
	}
	if strings.Contains(err.Error(), "timed out") {
		return
	}
	t.Fatalf("unexpected error: %v", err)
}

func TestFuncsNoOps(t *testing.T) {
	f := Funcs{}
	if f.Name() != "middleware" {
		t.Fatalf("default name mismatch: %s", f.Name())
	}
	if err := f.BeforeAgent(context.Background(), &State{}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := f.BeforeTool(context.Background(), &State{}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := f.AfterAgent(context.Background(), &State{}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := f.AfterTool(context.Background(), &State{}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestExecuteCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chain := NewChain([]Middleware{Funcs{Identifier: "noop"}}, WithTimeout(5*time.Second))
	err := chain.Execute(ctx, StageBeforeAgent, &State{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestMiddlewareNameVariants(t *testing.T) {
	if got := middlewareName(nil); got != "<nil>" {
		t.Fatalf("unexpected nil name: %s", got)
	}
	custom := Funcs{Identifier: ""}
	if got := middlewareName(custom); got != "middleware" {
		t.Fatalf("unexpected default name: %s", got)
	}

	if got := middlewareName(emptyName{}); got != "<unnamed>" {
		t.Fatalf("unexpected unnamed fallback: %s", got)
	}
}

func TestChainStatePropagation(t *testing.T) {
	chain := NewChain([]Middleware{
		Funcs{
			Identifier: "seed",
			OnBeforeTool: func(_ context.Context, st *State) error {
				st.SetValue("step", "seeded")
				st.ToolCall = "call-1"
				return nil
			},
		},
		Funcs{
			Identifier: "observer",
			OnBeforeTool: func(_ context.Context, st *State) error {
				if st.Values["step"] != "seeded" {
					return fmt.Errorf("state not forwarded: %#v", st.Values)
				}
				if st.ToolCall != "call-1" {
					return fmt.Errorf("tool call lost: %v", st.ToolCall)
				}
				st.SetValue("step", "observed")
				return nil
			},
		},
	})
	st := &State{}
	if err := chain.Execute(context.Background(), StageBeforeTool, st); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if st.Values["step"] != "observed" {
		t.Fatalf("state not mutated by chain: %#v", st.Values)
	}
}

func TestChainWithoutMiddlewareIsNoop(t *testing.T) {
	chain := NewChain(nil)
	if err := chain.Execute(context.Background(), StageAfterAgent, &State{}); err != nil {
		t.Fatalf("unexpected error on empty chain: %v", err)
	}
}

func TestChainUnknownStage(t *testing.T) {
	chain := NewChain([]Middleware{Funcs{Identifier: "demo"}})
	err := chain.Execute(context.Background(), Stage(99), &State{})
	if err == nil || !strings.Contains(err.Error(), "unknown stage") {
		t.Fatalf("expected unknown stage error, got %v", err)
	}
}

func TestChainConcurrentUseAndExecute(t *testing.T) {
	var executed atomic.Int32
	chain := NewChain(nil)

	start := make(chan struct{})
	var wg sync.WaitGroup
	mwCount := 8
	wg.Add(mwCount)

	for i := 0; i < mwCount; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			chain.Use(Funcs{
				Identifier: fmt.Sprintf("mw-%d", i),
				OnAfterAgent: func(context.Context, *State) error {
					executed.Add(1)
					return nil
				},
			})
		}(i)
	}

	close(start)
	wg.Wait()

	if err := chain.Execute(context.Background(), StageAfterAgent, &State{}); err != nil {
		t.Fatalf("execute after concurrent use: %v", err)
	}
	if got := executed.Load(); got != int32(mwCount) {
		t.Fatalf("expected %d middleware executions, got %d", mwCount, got)
	}
}

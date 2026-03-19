package main

import (
	"context"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
)

func TestSortedFlags_EmptyReturnsNil(t *testing.T) {
	t.Skip("sortedFlags helper removed with slash commands")
}

func TestChannelMatcher_Unmatched(t *testing.T) {
	m := channelMatcher("cli", 0.5)
	res := m.Match(skills.ActivationContext{Channels: []string{"web"}})
	if res.Matched {
		t.Fatalf("unexpected match: %+v", res)
	}
	res = m.Match(skills.ActivationContext{Channels: []string{" CLI "}})
	if !res.Matched {
		t.Fatalf("expected match: %+v", res)
	}
}

func TestExploreHandler_NoToolLimits(t *testing.T) {
	ctx := context.Background()
	subCtx := subagents.Context{ToolWhitelist: nil}
	req := subagents.Request{Instruction: "read"}
	res, err := exploreHandler(ctx, subCtx, req)
	if err != nil {
		t.Fatalf("exploreHandler: %v", err)
	}
	if res.Output == "" {
		t.Fatalf("empty output")
	}
}

func TestRegistrationFromBuiltin_PanicsOnMissingBuiltin(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = registrationFromBuiltin("definitely-not-a-builtin", nil, nil)
}

func TestSubagentsHandlers_CanceledContext(t *testing.T) {
	subCtx := subagents.Context{ToolWhitelist: []string{"bash"}}
	req := subagents.Request{Instruction: "x"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := exploreHandler(ctx, subCtx, req); err == nil {
		t.Fatalf("expected error")
	}
	if _, err := planHandler(ctx, subCtx, req); err == nil {
		t.Fatalf("expected error")
	}
	if _, err := deployGuardHandler(ctx, subCtx, req); err == nil {
		t.Fatalf("expected error")
	}
}

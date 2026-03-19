package subagents

import (
	"context"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
)

func TestManagerRegisterPropagatesValidationError(t *testing.T) {
	m := NewManager()
	if err := m.Register(Definition{Name: "bad name"}, HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		return Result{}, nil
	})); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestManagerListSortsByNameWhenPriorityEqual(t *testing.T) {
	m := NewManager()
	handler := HandlerFunc(func(context.Context, Context, Request) (Result, error) { return Result{}, nil })
	if err := m.Register(Definition{Name: "b"}, handler); err != nil {
		t.Fatalf("register b: %v", err)
	}
	if err := m.Register(Definition{Name: "a"}, handler); err != nil {
		t.Fatalf("register a: %v", err)
	}

	defs := m.List()
	if len(defs) != 2 || defs[0].Name != "a" || defs[1].Name != "b" {
		t.Fatalf("defs=%v, want a then b", defs)
	}
}

func TestManagerMatchingSkipsNilMatchersAndSortsByScore(t *testing.T) {
	m := NewManager()
	handler := HandlerFunc(func(context.Context, Context, Request) (Result, error) { return Result{}, nil })

	hi := skills.MatcherFunc(func(skills.ActivationContext) skills.MatchResult {
		return skills.MatchResult{Matched: true, Score: 0.9, Reason: "hi"}
	})
	lo := skills.MatcherFunc(func(skills.ActivationContext) skills.MatchResult {
		return skills.MatchResult{Matched: true, Score: 0.8, Reason: "lo"}
	})

	if err := m.Register(Definition{Name: "hi", Matchers: []skills.Matcher{nil, hi}}, handler); err != nil {
		t.Fatalf("register hi: %v", err)
	}
	if err := m.Register(Definition{Name: "lo", Matchers: []skills.Matcher{lo}}, handler); err != nil {
		t.Fatalf("register lo: %v", err)
	}

	matches := m.matching(skills.ActivationContext{})
	if len(matches) < 2 {
		t.Fatalf("matches=%v, want >=2", matches)
	}
	if matches[0].definition.Name != "hi" {
		t.Fatalf("first=%q, want hi", matches[0].definition.Name)
	}
}

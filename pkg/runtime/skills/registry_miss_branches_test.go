package skills

import (
	"context"
	"strings"
	"testing"
)

func TestSkillDefinitionAndExecuteNilBranches(t *testing.T) {
	var nilSkill *Skill
	if got := nilSkill.Definition(); got.Name != "" {
		t.Fatalf("Definition=%v, want empty", got)
	}
	if _, err := nilSkill.Execute(context.Background(), ActivationContext{}); err == nil {
		t.Fatalf("expected nil skill execute error")
	}

	s := &Skill{}
	if _, err := s.Execute(context.Background(), ActivationContext{}); err == nil {
		t.Fatalf("expected nil handler execute error")
	}
}

func TestRegistryMatchCoversNoMatchAndSortingBranches(t *testing.T) {
	r := NewRegistry()

	match := func(score float64, matched bool) Matcher {
		return MatcherFunc(func(ActivationContext) MatchResult {
			return MatchResult{Matched: matched, Score: score, Reason: "x"}
		})
	}

	err := r.Register(Definition{
		Name:        "a-skill",
		Description: "a",
		Priority:    1,
		Matchers:    []Matcher{nil, match(0.8, true)},
	}, HandlerFunc(func(context.Context, ActivationContext) (Result, error) { return Result{}, nil }))
	if err != nil {
		t.Fatalf("register a: %v", err)
	}

	err = r.Register(Definition{
		Name:        "b-skill",
		Description: "b",
		Priority:    1,
		Matchers:    []Matcher{match(0.9, true)},
	}, HandlerFunc(func(context.Context, ActivationContext) (Result, error) { return Result{}, nil }))
	if err != nil {
		t.Fatalf("register b: %v", err)
	}

	err = r.Register(Definition{
		Name:        "c-skill",
		Description: "c",
		Priority:    1,
		Matchers:    []Matcher{match(0.9, true)},
	}, HandlerFunc(func(context.Context, ActivationContext) (Result, error) { return Result{}, nil }))
	if err != nil {
		t.Fatalf("register c: %v", err)
	}

	err = r.Register(Definition{
		Name:        "no-match",
		Description: "n",
		Priority:    10,
		Matchers:    []Matcher{match(1.0, false)},
	}, HandlerFunc(func(context.Context, ActivationContext) (Result, error) { return Result{}, nil }))
	if err != nil {
		t.Fatalf("register no-match: %v", err)
	}

	activations := r.Match(ActivationContext{})
	if len(activations) != 3 {
		t.Fatalf("activations len=%d, want 3", len(activations))
	}

	// priority ties, sort by score then name.
	if activations[0].Definition().Name != "b-skill" {
		t.Fatalf("first=%q, want b-skill", activations[0].Definition().Name)
	}
	if activations[1].Score != 0.9 || activations[2].Score != 0.8 {
		t.Fatalf("scores=%v,%v,%v", activations[0].Score, activations[1].Score, activations[2].Score)
	}

	// Verify `evaluate` returns ok=false (no match) by ensuring no-match is excluded.
	for _, act := range activations {
		if act.Definition().Name == "no-match" {
			t.Fatalf("unexpected no-match activation: %v", act)
		}
	}
}

func TestRegistryEvaluateSkipsUnmatchedAndNilMatchers(t *testing.T) {
	res, ok := evaluate(&Skill{
		definition: Definition{
			Name:        "demo",
			Description: "d",
			Matchers: []Matcher{
				nil,
				MatcherFunc(func(ActivationContext) MatchResult { return MatchResult{} }),
			},
		},
		handler: HandlerFunc(func(context.Context, ActivationContext) (Result, error) { return Result{}, nil }),
	}, ActivationContext{})
	if ok || res.Matched {
		t.Fatalf("ok=%v res=%v, want unmatched", ok, res)
	}
}

func TestRegistryRegisterRejectsNilHandler(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Definition{Name: "demo", Description: "d"}, nil)
	if err == nil || !strings.Contains(err.Error(), "handler is nil") {
		t.Fatalf("err=%v, want handler is nil", err)
	}
}

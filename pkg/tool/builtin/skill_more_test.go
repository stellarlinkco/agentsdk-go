package toolbuiltin

import (
	"context"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
)

func TestParseSkillName_RejectsNonString(t *testing.T) {
	if _, err := parseSkillName(map[string]any{"command": 123}); err == nil || !strings.Contains(err.Error(), "must be string") {
		t.Fatalf("expected string coercion error, got %v", err)
	}
}

func TestParseSkillName_NilParams(t *testing.T) {
	if _, err := parseSkillName(nil); err == nil {
		t.Fatalf("expected error for nil params")
	}
}

func TestSkillActivationContextFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), activationContextKey{}, "not-a-context")
	if ac, ok := SkillActivationContextFromContext(ctx); ok || ac.Prompt != "" {
		t.Fatalf("expected missing activation context, got %+v ok=%v", ac, ok)
	}
}

func TestSkillActivationContextFromContext_NilContext(t *testing.T) {
	if ac, ok := SkillActivationContextFromContext(nil); ok || ac.Prompt != "" {
		t.Fatalf("expected missing activation context, got %+v ok=%v", ac, ok)
	}
}

func TestSkillLocation_PrefersKnownKeys(t *testing.T) {
	def := skills.Definition{
		Name:     "x",
		Metadata: map[string]string{"origin": "  ", "source": "src"},
	}
	if got := skillLocation(def); got != "src" {
		t.Fatalf("expected skillLocation to pick source, got %q", got)
	}
}

func TestSkillLocation_ReturnsEmptyWhenNoKnownKeyPresent(t *testing.T) {
	def := skills.Definition{
		Name:     "x",
		Metadata: map[string]string{"other": "x"},
	}
	if got := skillLocation(def); got != "" {
		t.Fatalf("expected empty location, got %q", got)
	}
}

func TestFormatSkillOutput_MarshalFailureFallsBack(t *testing.T) {
	got := formatSkillOutput(skills.Result{Skill: "a", Output: func() {}})
	if !strings.Contains(got, "skill a executed") {
		t.Fatalf("unexpected output %q", got)
	}
}

func TestFormatSkillOutput_EmptySkillFallsBackToGenericMessage(t *testing.T) {
	got := formatSkillOutput(skills.Result{})
	if got != "skill executed" {
		t.Fatalf("unexpected output %q", got)
	}
}

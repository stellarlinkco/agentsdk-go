package commands

import (
	"strings"
	"testing"
)

func TestParseSuccess(t *testing.T) {
	text := "/deploy app --env=prod --force yes\n/echo \"hello world\""
	inv, err := Parse(text)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(inv) != 2 {
		t.Fatalf("expected 2 invocations, got %d", len(inv))
	}
	first := inv[0]
	force, _ := first.Flag("force")
	if first.Name != "deploy" || first.Flags["env"] != "prod" || force != "yes" {
		t.Fatalf("unexpected first invocation: %+v", first)
	}
	if _, ok := first.Flag("missing"); ok {
		t.Fatalf("expected missing flag to be false")
	}
	second := inv[1]
	if second.Args[0] != "hello world" || second.Position != 2 {
		t.Fatalf("unexpected second invocation: %+v", second)
	}
}

func TestParseInvalid(t *testing.T) {
	if _, err := Parse("just text"); err != ErrNoCommand {
		t.Fatalf("expected ErrNoCommand, got %v", err)
	}
	if _, err := Parse("/broken 'unterminated"); err == nil {
		t.Fatalf("expected error for unterminated quote")
	}
}

func TestParseHandlesEdgeCases(t *testing.T) {
	if _, err := Parse("/cmd dangling\\"); err == nil || !strings.Contains(err.Error(), "dangling escape") {
		t.Fatalf("expected dangling escape error, got %v", err)
	}
	if _, err := Parse("/cmd --=value"); err == nil || !strings.Contains(err.Error(), "invalid flag") {
		t.Fatalf("expected invalid flag error, got %v", err)
	}
}

func TestInvocationFlagNilMap(t *testing.T) {
	if _, ok := (Invocation{}).Flag("any"); ok {
		t.Fatalf("flag lookup on nil map should be false")
	}
}

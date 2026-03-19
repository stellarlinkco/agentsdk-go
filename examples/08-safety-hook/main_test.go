package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestRun_Default(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Safety hook enabled:") || !strings.Contains(got, "Safety hook disabled:") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestMain_Returns(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"08-safety-hook"}

	main()
}

func TestRun_RequiresKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHasArg_EmptyWant(t *testing.T) {
	if got := hasArg([]string{"--help"}, ""); got {
		t.Fatalf("expected false")
	}
}

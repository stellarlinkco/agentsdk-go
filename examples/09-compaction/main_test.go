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
	if !strings.Contains(got, "compaction_calls=") || !strings.Contains(got, "tool_io_stripped=true") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestHasArg_EdgeCases(t *testing.T) {
	if hasArg([]string{"--help"}, "") {
		t.Fatalf("expected hasArg=false for empty want")
	}
	if !hasArg([]string{"  --help "}, "--help") {
		t.Fatalf("expected hasArg=true with trimming")
	}
	if hasArg([]string{"--x"}, "--help") {
		t.Fatalf("expected hasArg=false when missing")
	}
}

func TestMain_DoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := compactionFatal
	compactionFatal = func(_ ...any) { t.Fatalf("unexpected fatal") }
	t.Cleanup(func() { compactionFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"09-compaction"}

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

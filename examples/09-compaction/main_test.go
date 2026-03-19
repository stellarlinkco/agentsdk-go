package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestRun_OfflineDefault(t *testing.T) {
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
	if hasArg([]string{"--online"}, "") {
		t.Fatalf("expected hasArg=false for empty want")
	}
	if !hasArg([]string{"  --online "}, "--online") {
		t.Fatalf("expected hasArg=true with trimming")
	}
	if hasArg([]string{"--offline"}, "--online") {
		t.Fatalf("expected hasArg=false when missing")
	}
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
	oldFatal := compactionFatal
	compactionFatal = func(_ ...any) { t.Fatalf("unexpected fatal") }
	t.Cleanup(func() { compactionFatal = oldFatal })

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"09-compaction"}

	main()
}

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
	if !strings.Contains(got, "Safety hook enabled:") || !strings.Contains(got, "Safety hook disabled:") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestMain_OfflineReturns(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"08-safety-hook"}

	main()
}

func TestHasArg_EmptyWant(t *testing.T) {
	if got := hasArg([]string{"--online"}, ""); got {
		t.Fatalf("expected false")
	}
}

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunWithoutPromptErrors(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()

	originalStdin := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() {
		os.Stdin = originalStdin
	})

	err = run(nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatalf("expected error when no prompt is provided")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "prompt") {
		t.Fatalf("expected prompt-related error, got: %v", err)
	}
}

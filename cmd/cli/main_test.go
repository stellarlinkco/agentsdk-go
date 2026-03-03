package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/api"
)

func TestRunACPModeNoPrompt(t *testing.T) {
	originalServe := serveACPStdio
	t.Cleanup(func() {
		serveACPStdio = originalServe
	})

	called := false
	serveACPStdio = func(ctx context.Context, options api.Options, stdin io.Reader, stdout io.Writer) error {
		called = true
		return nil
	}

	if err := run([]string{"--acp=true"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run with --acp=true should not require prompt: %v", err)
	}
	if !called {
		t.Fatalf("expected ACP serve path to be called")
	}
}

func TestRunNonACPModeWithoutPromptErrors(t *testing.T) {
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
		t.Fatalf("expected error when no prompt is provided in non-ACP mode")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "prompt") {
		t.Fatalf("expected prompt-related error, got: %v", err)
	}
}

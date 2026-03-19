package main

import (
	"errors"
	"log/slog"
	"testing"
)

func TestBuildMCPServers_DefaultRequiresUVX_DisablesWhenMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(ioDiscard{}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(string) (string, error) { return "", errors.New("missing") }

	cfg := runConfig{enableMCP: true, mcpServer: ""}
	if got := buildMCPServers(cfg, logger); got != nil {
		t.Fatalf("expected nil, got=%v", got)
	}
}

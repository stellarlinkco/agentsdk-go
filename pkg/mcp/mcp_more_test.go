package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewSpecClientWith_PropagatesConnectError(t *testing.T) {
	t.Parallel()

	connect := func(context.Context, string) (*ClientSession, error) {
		return nil, errors.New("boom")
	}
	ensureInitialized := func(context.Context, *ClientSession) error { return nil }

	_, err := newSpecClientWith("stdio://echo hi", time.Second, connect, ensureInitialized)
	if err == nil || !strings.Contains(err.Error(), "connect MCP client") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHTTPFamilySpec_TrimsExtraHintsAndAliases(t *testing.T) {
	t.Parallel()

	kind, endpoint, matched, err := parseHTTPFamilySpec("https+json+v1://api.example.com")
	if err != nil {
		t.Fatalf("parseHTTPFamilySpec: %v", err)
	}
	if !matched {
		t.Fatalf("expected matched=true")
	}
	if kind != httpHintType {
		t.Fatalf("unexpected kind %q", kind)
	}
	if endpoint != "https://api.example.com" {
		t.Fatalf("unexpected endpoint %q", endpoint)
	}
}

func TestSpecClientWrapper_ListTools_ReturnsErrorWhenSessionClosed(t *testing.T) {
	t.Parallel()

	session, cleanup := newInMemorySession(t)
	defer cleanup()
	_ = session.Close()

	client := &specClientWrapper{session: session}
	if _, err := client.ListTools(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestPublishToolsChanged_SetsSessionIDAndErrorOnSnapshotFailure(t *testing.T) {
	t.Skip("tool list changed event removed in v2 refactor (Story 5)")
}

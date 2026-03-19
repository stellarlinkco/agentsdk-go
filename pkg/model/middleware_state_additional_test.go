package model

import (
	"context"
	"testing"
)

func TestMiddlewareStateNilContext(t *testing.T) {
	if got := middlewareState(nil); got != nil {
		t.Fatalf("expected nil state")
	}
}

func TestMiddlewareStateWrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), MiddlewareStateKey, "not-a-state")
	if got := middlewareState(ctx); got != nil {
		t.Fatalf("expected nil state for wrong type")
	}
}


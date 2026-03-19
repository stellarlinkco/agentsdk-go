package hooks

import (
	"context"
	"testing"
)

func TestChainOrdersMiddlewares(t *testing.T) {
	t.Parallel()
	var calls []string
	appendCall := func(label string) {
		calls = append(calls, label)
	}
	base := func(context.Context, Event) error {
		appendCall("handler")
		return nil
	}

	mw1 := func(next MiddlewareHandler) MiddlewareHandler {
		return func(ctx context.Context, evt Event) error {
			appendCall("mw1:before")
			err := next(ctx, evt)
			appendCall("mw1:after")
			return err
		}
	}
	mw2 := func(next MiddlewareHandler) MiddlewareHandler {
		return func(ctx context.Context, evt Event) error {
			appendCall("mw2:before")
			err := next(ctx, evt)
			appendCall("mw2:after")
			return err
		}
	}

	handler := Chain(base, mw1, mw2)
	if err := handler(context.Background(), Event{Type: Stop}); err != nil {
		t.Fatalf("handler: %v", err)
	}

	expected := []string{"mw1:before", "mw2:before", "handler", "mw2:after", "mw1:after"}
	if len(calls) != len(expected) {
		t.Fatalf("length mismatch %v vs %v", calls, expected)
	}
	for i := range expected {
		if calls[i] != expected[i] {
			t.Fatalf("order mismatch: got %v want %v", calls, expected)
		}
	}
}

package model

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOpenAIResponsesDoWithRetry_StopsOnContext(t *testing.T) {
	m := &openaiResponsesModel{maxRetries: 10}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(5*time.Millisecond, cancel)

	err := m.doWithRetry(ctx, func(context.Context) error {
		return stubNetError{timeout: true}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestOpenAIResponsesDoWithRetry_RetriesThenSucceeds(t *testing.T) {
	m := &openaiResponsesModel{maxRetries: 2}
	attempts := 0

	err := m.doWithRetry(context.Background(), func(context.Context) error {
		attempts++
		if attempts == 1 {
			return stubNetError{timeout: true}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestOpenAIResponsesDoWithRetry_StopsAtMaxRetries(t *testing.T) {
	m := &openaiResponsesModel{maxRetries: 1}
	start := time.Now()
	err := m.doWithRetry(context.Background(), func(context.Context) error {
		return stubNetError{timeout: true}
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	// One retry implies a single backoff sleep. Keep this bounded so tests stay fast.
	if time.Since(start) > 2*time.Second {
		t.Fatalf("retry backoff took too long")
	}
}

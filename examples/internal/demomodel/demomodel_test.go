package demomodel

import (
	"context"
	"errors"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestAnthropicAPIKeyPriority(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if got := AnthropicAPIKey(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok")
	if got := AnthropicAPIKey(); got != "tok" {
		t.Fatalf("expected token, got %q", got)
	}

	t.Setenv("ANTHROPIC_API_KEY", "key")
	if got := AnthropicAPIKey(); got != "key" {
		t.Fatalf("expected key, got %q", got)
	}
}

func TestEchoModelComplete_UsesLastUserText(t *testing.T) {
	m := &EchoModel{Prefix: "x"}
	resp, err := m.Complete(context.Background(), model.Request{
		Messages: []model.Message{
			{Role: "user", Content: "a"},
			{Role: "assistant", Content: "b"},
			{Role: "user", Content: "c"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp == nil || resp.Message.Content != "x: c" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestEchoModelComplete_DefaultPrefixAndFallbackToLastMessage(t *testing.T) {
	m := &EchoModel{}
	resp, err := m.Complete(context.Background(), model.Request{
		Messages: []model.Message{
			{Role: "assistant", Content: " hello "},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp == nil || resp.Message.Content != "demo: hello" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestEchoModelComplete_EmptyMessages(t *testing.T) {
	m := &EchoModel{}
	resp, err := m.Complete(context.Background(), model.Request{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp == nil || resp.Message.Content != "demo" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestEchoModelComplete_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &EchoModel{}
	if _, err := m.Complete(ctx, model.Request{Messages: []model.Message{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEchoModelCompleteStream_NilCallbackIsNoop(t *testing.T) {
	m := &EchoModel{Prefix: "x"}
	if err := m.CompleteStream(context.Background(), model.Request{}, nil); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
}

func TestEchoModelCompleteStream_PropagatesCompleteError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &EchoModel{Prefix: "x"}
	err := m.CompleteStream(ctx, model.Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	}, func(model.StreamResult) error { return nil })
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestEchoModelCompleteStream_EmitsFinal(t *testing.T) {
	m := &EchoModel{Prefix: "x"}
	var gotFinal bool
	err := m.CompleteStream(context.Background(), model.Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	}, func(sr model.StreamResult) error {
		if sr.Final {
			gotFinal = true
			if sr.Response == nil {
				return errors.New("missing response")
			}
			if sr.Response.Message.Content != "x: hi" {
				return errors.New("unexpected content")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if !gotFinal {
		t.Fatalf("expected final event")
	}
}

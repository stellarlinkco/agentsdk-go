package model

import (
	"context"
	"testing"
	"time"
)

func TestProviderFuncSuccess(t *testing.T) {
	fn := ProviderFunc(func(context.Context) (Model, error) { return &anthropicModel{}, nil })
	got, err := fn.Model(context.Background())
	if err != nil || got == nil {
		t.Fatalf("expected model, got=%v err=%v", got, err)
	}
}

func TestOpenAIProviderResolveAPIKey(t *testing.T) {
	p := &OpenAIProvider{}
	t.Setenv("OPENAI_API_KEY", "envkey")
	if got := p.resolveAPIKey(); got != "envkey" {
		t.Fatalf("expected env key, got %q", got)
	}
}

func TestOpenAIProviderCacheHitAndExpiry(t *testing.T) {
	p := &OpenAIProvider{CacheTTL: time.Minute}
	if got := p.cachedModel(); got != nil {
		t.Fatalf("expected nil cached model before storing")
	}

	p.cached = &openaiModel{}
	p.expires = time.Now().Add(-time.Minute)
	if got := p.cachedModel(); got != nil {
		t.Fatalf("expected expired cache to return nil")
	}

	p.expires = time.Now().Add(time.Minute)
	if got := p.cachedModel(); got == nil {
		t.Fatalf("expected valid cached model")
	}
}

func TestOpenAIProviderModelCaching(t *testing.T) {
	p := &OpenAIProvider{APIKey: "key", CacheTTL: time.Minute}
	m1, err := p.Model(context.Background())
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	m2, err := p.Model(context.Background())
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	if m1 != m2 {
		t.Fatalf("expected cached model")
	}
}

func TestOpenAIProviderModelMissingAPIKey(t *testing.T) {
	p := &OpenAIProvider{}
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := p.Model(context.Background()); err == nil {
		t.Fatalf("expected error for missing api key")
	}
}

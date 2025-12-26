package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Provider gives runtime access to lazily-instantiated models.
type Provider interface {
	Model(ctx context.Context) (Model, error)
}

// ProviderFunc is an adapter to allow use of ordinary functions as providers.
type ProviderFunc func(context.Context) (Model, error)

// Model implements Provider.
func (fn ProviderFunc) Model(ctx context.Context) (Model, error) {
	if fn == nil {
		return nil, errors.New("model provider function is nil")
	}
	return fn(ctx)
}

// AnthropicProvider caches anthropic clients with optional TTL.
type AnthropicProvider struct {
	APIKey      string
	BaseURL     string
	ModelName   string
	MaxTokens   int
	MaxRetries  int
	System      string
	Temperature *float64
	CacheTTL    time.Duration

	mu      sync.RWMutex
	cached  Model
	expires time.Time
}

// Model implements Provider with caching.
func (p *AnthropicProvider) Model(ctx context.Context) (Model, error) {
	if mdl := p.cachedModel(); mdl != nil {
		return mdl, nil
	}
	mdl, err := NewAnthropic(AnthropicConfig{
		APIKey:      p.resolveAPIKey(),
		BaseURL:     strings.TrimSpace(p.BaseURL),
		Model:       strings.TrimSpace(p.ModelName),
		MaxTokens:   p.MaxTokens,
		MaxRetries:  p.MaxRetries,
		System:      p.System,
		Temperature: p.Temperature,
	})
	if err != nil {
		return nil, err
	}
	p.store(mdl)
	return mdl, nil
}

func (p *AnthropicProvider) resolveAPIKey() string {
	if key := strings.TrimSpace(p.APIKey); key != "" {
		return key
	}
	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
}

func (p *AnthropicProvider) cachedModel() Model {
	if p.CacheTTL <= 0 {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cached == nil || time.Now().After(p.expires) {
		return nil
	}
	return p.cached
}

func (p *AnthropicProvider) store(m Model) {
	if p.CacheTTL <= 0 || m == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cached = m
	p.expires = time.Now().Add(p.CacheTTL)
}

// MustProvider materialises a model immediately and panics on failure.
func MustProvider(p Provider) Model {
	if p == nil {
		panic("model provider is nil")
	}
	mdl, err := p.Model(context.Background())
	if err != nil {
		panic(fmt.Sprintf("model provider failed: %v", err))
	}
	return mdl
}

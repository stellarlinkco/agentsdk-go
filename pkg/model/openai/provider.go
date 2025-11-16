package openai

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

// Provider wires OpenAI-backed model implementations into the factory.
type Provider struct {
	HTTPClient *http.Client
}

// Ensure Provider satisfies the model.Provider interface.
var _ modelpkg.Provider = (*Provider)(nil)

// NewProvider builds an OpenAI provider with the supplied HTTP client. When
// client is nil, a client with sane defaults will be used.
func NewProvider(client *http.Client) *Provider {
	return &Provider{HTTPClient: client}
}

// Name advertises the provider identifier used by the factory.
func (p *Provider) Name() string {
	return "openai"
}

// NewModel materializes an OpenAIModel configured according to cfg.
func (p *Provider) NewModel(ctx context.Context, cfg modelpkg.ModelConfig) (modelpkg.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai api key is required")
	}

	modelName := strings.TrimSpace(cfg.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(cfg.Name)
	}
	if modelName == "" {
		return nil, errors.New("openai model name is required")
	}

	baseURL := sanitizeBaseURL(cfg.BaseURL)
	headers := buildDefaultHeaders(apiKey)
	for k, v := range cfg.Headers {
		if strings.TrimSpace(k) == "" || v == "" {
			continue
		}
		headers[k] = v
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: time.Duration(defaultHTTPTimeout) * time.Second}
	}

	return &Model{
		client:  client,
		baseURL: baseURL,
		model:   modelName,
		headers: headers,
		opts:    parseModelOptions(cfg.Extra),
	}, nil
}

func sanitizeBaseURL(base string) string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return defaultBaseURL
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if trimmed == "" {
		return defaultBaseURL
	}
	return trimmed
}

func buildDefaultHeaders(apiKey string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
		"Accept":        "application/json",
		"User-Agent":    userAgent,
	}
}

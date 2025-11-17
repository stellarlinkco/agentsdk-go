package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const defaultOpenAIBatchSize = 32

// OpenAIEmbedderOption customizes embedding behaviour.
type OpenAIEmbedderOption func(*openAIEmbedderConfig)

type openAIEmbedderConfig struct {
	batchSize   int
	dimensions  int
	requestOpts []option.RequestOption
}

// WithOpenAIEmbedderBatchSize overrides the batch size (default 32).
func WithOpenAIEmbedderBatchSize(size int) OpenAIEmbedderOption {
	return func(cfg *openAIEmbedderConfig) {
		cfg.batchSize = size
	}
}

// WithOpenAIEmbedderDimensions truncates embeddings to the provided size when supported.
func WithOpenAIEmbedderDimensions(dim int) OpenAIEmbedderOption {
	return func(cfg *openAIEmbedderConfig) {
		cfg.dimensions = dim
	}
}

// WithOpenAIEmbedderOptions injects additional request options (e.g. base URL, organization).
func WithOpenAIEmbedderOptions(opts ...option.RequestOption) OpenAIEmbedderOption {
	return func(cfg *openAIEmbedderConfig) {
		cfg.requestOpts = append(cfg.requestOpts, opts...)
	}
}

// OpenAIEmbedder implements the Embedder interface via the official SDK.
type OpenAIEmbedder struct {
	client    openaisdk.Client
	model     openaisdk.EmbeddingModel
	batchSize int
	dims      int
}

// NewOpenAIEmbedder creates an embedder backed by OpenAI's embeddings API.
func NewOpenAIEmbedder(apiKey, model string, opts ...OpenAIEmbedderOption) (*OpenAIEmbedder, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("openai embedder: api key is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("openai embedder: model is required")
	}
	cfg := openAIEmbedderConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	reqOpts := append([]option.RequestOption{option.WithAPIKey(apiKey)}, cfg.requestOpts...)
	emb := &OpenAIEmbedder{
		client:    openaisdk.NewClient(reqOpts...),
		model:     openaisdk.EmbeddingModel(model),
		batchSize: cfg.batchSize,
		dims:      cfg.dimensions,
	}
	if emb.batchSize <= 0 {
		emb.batchSize = defaultOpenAIBatchSize
	}
	return emb, nil
}

// Embed converts the provided texts into dense vectors.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if e == nil {
		return nil, errors.New("openai embedder is nil")
	}
	if len(texts) == 0 {
		return nil, errors.New("openai embedder: no texts provided")
	}
	batch := e.batchSize
	if batch <= 0 {
		batch = defaultOpenAIBatchSize
	}
	result := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += batch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + batch
		if end > len(texts) {
			end = len(texts)
		}
		chunk := texts[start:end]
		params := openaisdk.EmbeddingNewParams{
			Model: e.model,
			Input: openaisdk.EmbeddingNewParamsInputUnion{OfArrayOfStrings: chunk},
		}
		if e.dims > 0 {
			params.Dimensions = openaisdk.Int(int64(e.dims))
		}
		resp, err := e.client.Embeddings.New(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("openai embed request: %w", err)
		}
		if len(resp.Data) != len(chunk) {
			return nil, fmt.Errorf("openai embedder: expected %d vectors got %d", len(chunk), len(resp.Data))
		}
		for _, data := range resp.Data {
			vector := append([]float64(nil), data.Embedding...)
			result = append(result, vector)
		}
	}
	return result, nil
}

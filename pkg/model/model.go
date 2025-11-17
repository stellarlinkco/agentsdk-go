package model

import "context"

// Model describes the behavior every language-model backend must support.
// Generate is a unary request/response call, while GenerateStream delivers
// incremental updates through the supplied callback.
type Model interface {
	Generate(ctx context.Context, messages []Message) (Message, error)
	GenerateStream(ctx context.Context, messages []Message, cb StreamCallback) error
}

// ModelWithTools is an optional interface for models that support tool calling.
// If implemented, Agent will pass tool schemas to enable LLM-driven tool selection.
type ModelWithTools interface {
	Model
	GenerateWithTools(ctx context.Context, messages []Message, tools []map[string]any) (Message, error)
}

// StreamCallback consumes incremental output produced by GenerateStream.
// Implementations should call the callback in order, using StreamResult.Final
// to signal completion.
type StreamCallback func(StreamResult) error

// StreamResult wraps a partial or final model response. When Final is true the
// stream is complete and no more chunks should be delivered.
type StreamResult struct {
	Message Message
	Final   bool
}

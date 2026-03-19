package demomodel

import (
	"context"
	"os"
	"strings"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func AnthropicAPIKey() string {
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")); v != "" {
		return v
	}
	return ""
}

func AnthropicBaseURL() string {
	return strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
}

type EchoModel struct {
	Prefix string
}

func (m *EchoModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content := m.Prefix
	if content == "" {
		content = "demo"
	}
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.TrimSpace(req.Messages[i].Role) == "user" {
			last = strings.TrimSpace(req.Messages[i].TextContent())
			break
		}
	}
	if last == "" && len(req.Messages) > 0 {
		last = strings.TrimSpace(req.Messages[len(req.Messages)-1].TextContent())
	}
	if last != "" {
		content = content + ": " + last
	}
	return &model.Response{
		Message:    model.Message{Role: "assistant", Content: content},
		StopReason: "stop",
	}, nil
}

func (m *EchoModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	if cb == nil {
		return nil
	}
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	return cb(model.StreamResult{Final: true, Response: resp})
}

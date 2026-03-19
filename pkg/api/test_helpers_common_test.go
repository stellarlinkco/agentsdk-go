package api

import (
	"context"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

type staticModel struct {
	content string
}

func (m staticModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return &model.Response{Message: model.Message{Role: "assistant", Content: m.content}}, nil
}

func (m staticModel) CompleteStream(_ context.Context, req model.Request, cb model.StreamHandler) error {
	resp, err := m.Complete(context.Background(), req)
	if err != nil {
		return err
	}
	return cb(model.StreamResult{Final: true, Response: resp})
}

func boolPtr(v bool) *bool { return &v }

type stubTool struct {
	name string
}

func (t *stubTool) Name() string             { return t.name }
func (t *stubTool) Description() string      { return "" }
func (t *stubTool) Schema() *tool.JSONSchema { return &tool.JSONSchema{Type: "object"} }
func (t *stubTool) Execute(context.Context, map[string]any) (*tool.ToolResult, error) {
	return &tool.ToolResult{}, nil
}

func newTestRuntime(t *testing.T, mdl model.Model, auto CompactConfig) *Runtime {
	t.Helper()
	root := t.TempDir()
	opts := Options{
		ProjectRoot:         root,
		Model:               mdl,
		EnabledBuiltinTools: []string{},
		RulesEnabled:        boolPtr(false),
		AutoCompact:         auto,
		TokenLimit:          50,
	}
	rt, err := New(context.Background(), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

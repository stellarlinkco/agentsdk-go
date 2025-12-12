package api

import (
	"context"
	"sync"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/model"
)

// mockModel implements model.Model for testing
type mockModel struct {
	name string
}

func (m *mockModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	return &model.Response{
		Message: model.Message{Role: "assistant", Content: "mock response from " + m.name},
	}, nil
}

func (m *mockModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	return nil
}

func TestModelTierConstants(t *testing.T) {
	tests := []struct {
		tier     ModelTier
		expected string
	}{
		{ModelTierLow, "low"},
		{ModelTierMid, "mid"},
		{ModelTierHigh, "high"},
	}
	for _, tt := range tests {
		if string(tt.tier) != tt.expected {
			t.Errorf("ModelTier %v = %q, want %q", tt.tier, string(tt.tier), tt.expected)
		}
	}
}

func TestWithModelPool(t *testing.T) {
	haiku := &mockModel{name: "haiku"}
	sonnet := &mockModel{name: "sonnet"}
	pool := map[string]model.Model{
		"low": haiku,
		"mid": sonnet,
	}

	opts := &Options{}
	WithModelPool(pool)(opts)

	if len(opts.ModelPool) != 2 {
		t.Errorf("ModelPool length = %d, want 2", len(opts.ModelPool))
	}
	if opts.ModelPool["low"] != haiku {
		t.Error("ModelPool[\"low\"] not set correctly")
	}
}

func TestWithModelPoolNil(t *testing.T) {
	opts := &Options{ModelPool: map[string]model.Model{"existing": &mockModel{}}}
	WithModelPool(nil)(opts)
	if opts.ModelPool == nil {
		t.Error("WithModelPool(nil) should not clear existing pool")
	}
}

func TestWithToolModelMapping(t *testing.T) {
	mapping := map[string]string{
		"grep": "low",
		"task": "high",
	}

	opts := &Options{}
	WithToolModelMapping(mapping)(opts)

	if len(opts.ToolModelMapping) != 2 {
		t.Errorf("ToolModelMapping length = %d, want 2", len(opts.ToolModelMapping))
	}
	if opts.ToolModelMapping["grep"] != "low" {
		t.Error("ToolModelMapping[\"grep\"] not set correctly")
	}
}

func TestWithToolModelMappingNil(t *testing.T) {
	opts := &Options{ToolModelMapping: map[string]string{"existing": "low"}}
	WithToolModelMapping(nil)(opts)
	if opts.ToolModelMapping == nil {
		t.Error("WithToolModelMapping(nil) should not clear existing mapping")
	}
}

func TestSelectModelForTool(t *testing.T) {
	defaultModel := &mockModel{name: "default"}
	haiku := &mockModel{name: "haiku"}
	opus := &mockModel{name: "opus"}

	rt := &Runtime{
		opts: Options{
			Model: defaultModel,
			ModelPool: map[string]model.Model{
				"low":  haiku,
				"high": opus,
			},
			ToolModelMapping: map[string]string{
				"grep": "low",
				"task": "high",
			},
		},
	}

	tests := []struct {
		toolName     string
		expectedName string
		expectedTier string
	}{
		{"grep", "haiku", "low"},
		{"task", "opus", "high"},
		{"bash", "default", ""},    // Not in mapping, use default
		{"unknown", "default", ""}, // Unknown tool, use default
	}

	for _, tt := range tests {
		mdl, tier := rt.selectModelForTool(tt.toolName)
		mock := mdl.(*mockModel)
		if mock.name != tt.expectedName {
			t.Errorf("selectModelForTool(%q) model = %q, want %q", tt.toolName, mock.name, tt.expectedName)
		}
		if tier != tt.expectedTier {
			t.Errorf("selectModelForTool(%q) tier = %q, want %q", tt.toolName, tier, tt.expectedTier)
		}
	}
}

func TestSelectModelForToolNoPool(t *testing.T) {
	defaultModel := &mockModel{name: "default"}

	rt := &Runtime{
		opts: Options{
			Model: defaultModel,
			ToolModelMapping: map[string]string{
				"grep": "low",
			},
		},
	}

	mdl, tier := rt.selectModelForTool("grep")
	mock := mdl.(*mockModel)
	if mock.name != "default" {
		t.Errorf("selectModelForTool with no pool = %q, want default", mock.name)
	}
	if tier != "" {
		t.Errorf("tier should be empty when pool is nil, got %q", tier)
	}
}

func TestSelectModelForToolNoMapping(t *testing.T) {
	defaultModel := &mockModel{name: "default"}
	haiku := &mockModel{name: "haiku"}

	rt := &Runtime{
		opts: Options{
			Model: defaultModel,
			ModelPool: map[string]model.Model{
				"low": haiku,
			},
		},
	}

	mdl, tier := rt.selectModelForTool("grep")
	mock := mdl.(*mockModel)
	if mock.name != "default" {
		t.Errorf("selectModelForTool with no mapping = %q, want default", mock.name)
	}
	if tier != "" {
		t.Errorf("tier should be empty when mapping is nil, got %q", tier)
	}
}

func TestSelectModelForToolConcurrent(t *testing.T) {
	defaultModel := &mockModel{name: "default"}
	haiku := &mockModel{name: "haiku"}

	rt := &Runtime{
		opts: Options{
			Model: defaultModel,
			ModelPool: map[string]model.Model{
				"low": haiku,
			},
			ToolModelMapping: map[string]string{
				"grep": "low",
			},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rt.selectModelForTool("grep")
			rt.selectModelForTool("bash")
		}()
	}
	wg.Wait()
}

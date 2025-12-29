package api

import (
	"context"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/message"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	"github.com/cexll/agentsdk-go/pkg/model"
)

func TestConversationModelGenerateNilModel(t *testing.T) {
	conv := &conversationModel{hooks: &runtimeHookAdapter{}, history: message.NewHistory()}
	if _, err := conv.Generate(context.Background(), &agent.Context{}); err == nil {
		t.Fatal("expected nil model error")
	}
}

func TestConversationModelGenerateTracksStateAndToolCalls(t *testing.T) {
	hist := message.NewHistory()
	hist.Append(message.Message{Role: "system", Content: "intro"})

	response := &model.Response{
		Message: model.Message{
			Role:    "assistant",
			Content: " trimmed ",
			ToolCalls: []model.ToolCall{{
				ID:        "t1",
				Name:      "echo",
				Arguments: map[string]any{"x": "y"},
			}},
		},
		Usage:      model.Usage{OutputTokens: 10},
		StopReason: "stop",
	}
	stub := &stubModel{responses: []*model.Response{response}}

	state := &middleware.State{Values: map[string]any{}}
	ctx := context.WithValue(context.Background(), middlewareStateKey, state)

	conv := &conversationModel{
		base:         stub,
		history:      hist,
		prompt:       " user input ",
		trimmer:      message.NewTrimmer(100, nil),
		tools:        []model.ToolDefinition{{Name: "echo"}},
		systemPrompt: "sys",
		hooks:        &runtimeHookAdapter{},
	}

	out, err := conv.Generate(ctx, &agent.Context{})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if out == nil || len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "echo" {
		t.Fatalf("unexpected model output: %+v", out)
	}
	if conv.stopReason != "stop" || conv.usage.OutputTokens != 10 {
		t.Fatalf("usage/stop reason not recorded: %+v %s", conv.usage, conv.stopReason)
	}
	if hist.Len() == 0 {
		t.Fatal("history not appended")
	}
	if state.ModelInput == nil || state.ModelOutput == nil {
		t.Fatalf("middleware state not populated: %+v", state)
	}
}

func TestConversationModelEnableCachePassthrough(t *testing.T) {
	tests := []struct {
		name        string
		enableCache bool
		wantCache   bool
	}{
		{
			name:        "cache enabled",
			enableCache: true,
			wantCache:   true,
		},
		{
			name:        "cache disabled",
			enableCache: false,
			wantCache:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hist := message.NewHistory()
			response := &model.Response{
				Message:    model.Message{Role: "assistant", Content: "ok"},
				Usage:      model.Usage{OutputTokens: 5},
				StopReason: "end",
			}
			stub := &stubModel{responses: []*model.Response{response}}

			conv := &conversationModel{
				base:         stub,
				history:      hist,
				prompt:       "test",
				systemPrompt: "sys",
				enableCache:  tt.enableCache,
				hooks:        &runtimeHookAdapter{},
			}

			_, err := conv.Generate(context.Background(), &agent.Context{})
			if err != nil {
				t.Fatalf("generate error: %v", err)
			}

			// Verify the model request had correct EnablePromptCache
			if len(stub.requests) == 0 {
				t.Fatal("expected at least one model request")
			}
			gotCache := stub.requests[0].EnablePromptCache
			if gotCache != tt.wantCache {
				t.Errorf("EnablePromptCache = %v, want %v", gotCache, tt.wantCache)
			}
		})
	}
}

package model

import (
	"net/http"
	"testing"
)

func TestNewOpenAI_AcceptsBaseURLAndHTTPClient(t *testing.T) {
	_, err := NewOpenAI(OpenAIConfig{
		APIKey:     "key",
		BaseURL:    "http://example.invalid",
		HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
}

func TestOpenAIModelBuildParamsCoversTemperatureAndUser(t *testing.T) {
	tempA := 0.3
	tempB := 0.7
	m := &openaiModel{
		model:       "gpt-4o",
		maxTokens:   123,
		maxRetries:  1,
		system:      "sys",
		temperature: &tempA,
	}

	params := m.buildParams(Request{
		SessionID:   "sess",
		Temperature: &tempB,
	})
	if params.Model == "" {
		t.Fatalf("expected model to be set")
	}
	if !params.User.Valid() || params.User.Value != "sess" {
		t.Fatalf("user=%v, want sess", params.User)
	}
	if !params.Temperature.Valid() {
		t.Fatalf("expected temperature to be set")
	}
}

func TestBuildOpenAIUserContentPartsEmptyAddsPlaceholder(t *testing.T) {
	parts := buildOpenAIUserContentParts(Message{
		Content:       " ",
		ContentBlocks: []ContentBlock{{Type: ContentBlockText, Text: " "}},
	})
	if len(parts) == 0 {
		t.Fatalf("expected placeholder content part")
	}
}

func TestBuildOpenAIAssistantMessageSkipsToolCallsMissingIDOrName(t *testing.T) {
	_ = buildOpenAIAssistantMessage(Message{
		Role:    "assistant",
		Content: "hi",
		ToolCalls: []ToolCall{
			{ID: " ", Name: "tool"},
			{ID: "id", Name: " "},
		},
	})
}

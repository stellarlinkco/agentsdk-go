package main

import (
	"context"
	"testing"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

func TestWeatherToolsSchema(t *testing.T) {
	t.Parallel()
	if len(weatherTools) != 1 {
		t.Fatalf("expected single tool, got %d", len(weatherTools))
	}
	tool := weatherTools[0]
	if tool["type"] != "function" {
		t.Fatalf("unexpected tool type: %v", tool["type"])
	}
	fn, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatalf("function payload not a map: %T", tool["function"])
	}
	if fn["name"] != "lookup_weather" {
		t.Fatalf("function name mismatch: %v", fn["name"])
	}
	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters payload mismatch: %T", fn["parameters"])
	}
	if params["type"] != "object" {
		t.Fatalf("parameter type mismatch: %v", params["type"])
	}
}

func TestNewOpenAIModelValidation(t *testing.T) {
	t.Parallel()
	if _, err := newOpenAIModel(context.Background(), "   "); err == nil {
		t.Fatal("expected error for blank api key")
	}
}

func TestNewOpenAIModelImplementsTools(t *testing.T) {
	t.Parallel()
	model, err := newOpenAIModel(context.Background(), "demo-key")
	if err != nil {
		t.Fatalf("newOpenAIModel error: %v", err)
	}
	if _, ok := model.(modelpkg.ModelWithTools); !ok {
		t.Fatal("model should implement ModelWithTools")
	}
}

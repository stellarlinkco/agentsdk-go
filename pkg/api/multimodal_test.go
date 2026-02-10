package api

import (
	"testing"

	"github.com/cexll/agentsdk-go/pkg/message"
	"github.com/cexll/agentsdk-go/pkg/model"
)

func TestConvertMessages_WithContentBlocks(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{
			Role:    "user",
			Content: "text",
			ContentBlocks: []message.ContentBlock{
				{Type: message.ContentBlockText, Text: "hello"},
				{Type: message.ContentBlockImage, MediaType: "image/png", Data: "base64"},
			},
		},
	}
	result := convertMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].ContentBlocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(result[0].ContentBlocks))
	}
	if result[0].ContentBlocks[0].Type != model.ContentBlockText {
		t.Fatalf("expected text block type, got %s", result[0].ContentBlocks[0].Type)
	}
	if result[0].ContentBlocks[1].Type != model.ContentBlockImage {
		t.Fatalf("expected image block type, got %s", result[0].ContentBlocks[1].Type)
	}
	if result[0].ContentBlocks[1].Data != "base64" {
		t.Fatalf("expected data 'base64', got %q", result[0].ContentBlocks[1].Data)
	}
}

func TestConvertMessages_WithoutContentBlocks(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		{Role: "user", Content: "plain"},
	}
	result := convertMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].ContentBlocks != nil {
		t.Fatalf("expected nil ContentBlocks, got %v", result[0].ContentBlocks)
	}
	if result[0].Content != "plain" {
		t.Fatalf("expected 'plain', got %q", result[0].Content)
	}
}

func TestConvertAPIContentBlocks(t *testing.T) {
	t.Parallel()

	blocks := []model.ContentBlock{
		{Type: model.ContentBlockText, Text: "hello"},
		{Type: model.ContentBlockImage, MediaType: "image/jpeg", Data: "data", URL: "url"},
		{Type: model.ContentBlockDocument, MediaType: "application/pdf", Data: "pdf"},
	}
	result := convertAPIContentBlocks(blocks)
	if len(result) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(result))
	}
	if result[0].Type != message.ContentBlockText || result[0].Text != "hello" {
		t.Fatalf("block 0 mismatch: %+v", result[0])
	}
	if result[1].Type != message.ContentBlockImage || result[1].Data != "data" || result[1].URL != "url" {
		t.Fatalf("block 1 mismatch: %+v", result[1])
	}
	if result[2].Type != message.ContentBlockDocument || result[2].Data != "pdf" {
		t.Fatalf("block 2 mismatch: %+v", result[2])
	}
}

func TestConvertAPIContentBlocks_Nil(t *testing.T) {
	t.Parallel()

	result := convertAPIContentBlocks(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestConvertContentBlocksToModel_Nil(t *testing.T) {
	t.Parallel()

	result := convertContentBlocksToModel(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

package acp

import (
	"errors"
	"strings"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/model"
	acpproto "github.com/coder/acp-go-sdk"
)

func TestConvertPromptBlocksSupportsImageAndResourceContent(t *testing.T) {
	t.Parallel()

	resourceLink := acpproto.ResourceLinkBlock("design.pdf", "file:///tmp/design.pdf")
	resourceLink.ResourceLink.MimeType = acpproto.Ptr("application/pdf")

	promptText, contentBlocks := convertPromptBlocks([]acpproto.ContentBlock{
		acpproto.TextBlock("Analyze this"),
		acpproto.ImageBlock("aGVsbG8=", "image/png"),
		acpproto.ResourceBlock(acpproto.EmbeddedResourceResource{
			TextResourceContents: &acpproto.TextResourceContents{
				Uri:      "file:///tmp/README.md",
				MimeType: acpproto.Ptr("text/markdown"),
				Text:     "# Hello",
			},
		}),
		acpproto.ResourceBlock(acpproto.EmbeddedResourceResource{
			BlobResourceContents: &acpproto.BlobResourceContents{
				Uri:      "file:///tmp/image.bin",
				MimeType: acpproto.Ptr("application/octet-stream"),
				Blob:     "AAEC",
			},
		}),
		resourceLink,
	})

	if !strings.Contains(promptText, "Analyze this") {
		t.Fatalf("prompt text missing user text: %q", promptText)
	}
	if !strings.Contains(promptText, "# Hello") {
		t.Fatalf("prompt text missing embedded text resource: %q", promptText)
	}
	if !strings.Contains(promptText, "file:///tmp/design.pdf") {
		t.Fatalf("prompt text missing resource link URI: %q", promptText)
	}

	var sawImage bool
	var sawBlobDocument bool
	var sawLinkedDocument bool
	for _, block := range contentBlocks {
		switch block.Type {
		case model.ContentBlockImage:
			if block.Data == "aGVsbG8=" && block.MediaType == "image/png" {
				sawImage = true
			}
		case model.ContentBlockDocument:
			if block.Data == "AAEC" && block.MediaType == "application/octet-stream" {
				sawBlobDocument = true
			}
			if block.URL == "file:///tmp/design.pdf" {
				sawLinkedDocument = true
			}
		}
	}

	if !sawImage {
		t.Fatalf("expected image content block, got %+v", contentBlocks)
	}
	if !sawBlobDocument {
		t.Fatalf("expected blob document content block, got %+v", contentBlocks)
	}
	if !sawLinkedDocument {
		t.Fatalf("expected linked document content block, got %+v", contentBlocks)
	}
}

func TestConvertPromptBlocksSupportsImageURI(t *testing.T) {
	t.Parallel()

	uri := "https://example.com/image.jpg"
	_, contentBlocks := convertPromptBlocks([]acpproto.ContentBlock{
		{
			Image: &acpproto.ContentBlockImage{
				Type:     "image",
				MimeType: "image/jpeg",
				Data:     "",
				Uri:      &uri,
			},
		},
	})

	if len(contentBlocks) != 1 {
		t.Fatalf("contentBlocks len=%d, want 1", len(contentBlocks))
	}
	if contentBlocks[0].Type != model.ContentBlockImage {
		t.Fatalf("content block type=%q, want %q", contentBlocks[0].Type, model.ContentBlockImage)
	}
	if contentBlocks[0].URL != uri {
		t.Fatalf("image URL=%q, want %q", contentBlocks[0].URL, uri)
	}
}

func TestMapStopReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  acpproto.StopReason
	}{
		{input: "cancelled", want: acpproto.StopReasonCancelled},
		{input: "max_tokens", want: acpproto.StopReasonMaxTokens},
		{input: "max_turn_requests", want: acpproto.StopReasonMaxTurnRequests},
		{input: "refusal", want: acpproto.StopReasonRefusal},
		{input: "tool_use", want: acpproto.StopReasonEndTurn}, // unknown => end_turn
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			if got := mapStopReason(tc.input); got != tc.want {
				t.Fatalf("mapStopReason(%q)=%q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsCancelledStreamError(t *testing.T) {
	t.Parallel()

	if !isCancelledStreamError("request canceled by user") {
		t.Fatalf("expected canceled message to be recognized")
	}
	if !isCancelledStreamError(errors.New("operation cancelled")) {
		t.Fatalf("expected cancelled error to be recognized")
	}
	if isCancelledStreamError("tool execution failed") {
		t.Fatalf("unexpected cancellation recognition for generic error")
	}
	if isCancelledStreamError(nil) {
		t.Fatalf("nil should not be recognized as cancellation")
	}
}

func TestStreamEventError(t *testing.T) {
	t.Parallel()

	if err := streamEventError(api.StreamEvent{Type: api.EventMessageStart}); err != nil {
		t.Fatalf("non-error event should not return error, got %v", err)
	}
	err := streamEventError(api.StreamEvent{Type: api.EventError, Output: "boom"})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("unexpected stream error: %v", err)
	}
	err = streamEventError(api.StreamEvent{Type: api.EventError, Output: ""})
	if err == nil || err.Error() != "runtime stream error" {
		t.Fatalf("unexpected default stream error: %v", err)
	}
}

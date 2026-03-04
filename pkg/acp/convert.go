package acp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/model"
	acpproto "github.com/coder/acp-go-sdk"
)

func convertPromptBlocks(prompt []acpproto.ContentBlock) (string, []model.ContentBlock) {
	if len(prompt) == 0 {
		return "", nil
	}

	textParts := make([]string, 0, len(prompt))
	contentBlocks := make([]model.ContentBlock, 0, len(prompt))

	for _, block := range prompt {
		switch {
		case block.Text != nil:
			if text := strings.TrimSpace(block.Text.Text); text != "" {
				textParts = append(textParts, text)
				contentBlocks = append(contentBlocks, model.ContentBlock{
					Type: model.ContentBlockText,
					Text: text,
				})
			}

		case block.Image != nil:
			image := block.Image
			switch {
			case image.Uri != nil && strings.TrimSpace(*image.Uri) != "":
				contentBlocks = append(contentBlocks, model.ContentBlock{
					Type:      model.ContentBlockImage,
					MediaType: image.MimeType,
					URL:       strings.TrimSpace(*image.Uri),
				})
			case strings.TrimSpace(image.Data) != "":
				contentBlocks = append(contentBlocks, model.ContentBlock{
					Type:      model.ContentBlockImage,
					MediaType: image.MimeType,
					Data:      image.Data,
				})
			}

		case block.ResourceLink != nil:
			uri := strings.TrimSpace(block.ResourceLink.Uri)
			if uri != "" {
				textParts = append(textParts, fmt.Sprintf("Resource: %s", uri))
				contentBlocks = append(contentBlocks, model.ContentBlock{
					Type:      model.ContentBlockDocument,
					MediaType: strings.TrimSpace(derefString(block.ResourceLink.MimeType)),
					URL:       uri,
				})
			}

		case block.Resource != nil:
			switch {
			case block.Resource.Resource.TextResourceContents != nil:
				resource := block.Resource.Resource.TextResourceContents
				if text := strings.TrimSpace(resource.Text); text != "" {
					textParts = append(textParts, text)
				}
				if uri := strings.TrimSpace(resource.Uri); uri != "" {
					textParts = append(textParts, fmt.Sprintf("Resource: %s", uri))
				}

			case block.Resource.Resource.BlobResourceContents != nil:
				resource := block.Resource.Resource.BlobResourceContents
				if data := strings.TrimSpace(resource.Blob); data != "" {
					contentBlocks = append(contentBlocks, model.ContentBlock{
						Type:      model.ContentBlockDocument,
						MediaType: strings.TrimSpace(derefString(resource.MimeType)),
						Data:      data,
					})
				}
				if uri := strings.TrimSpace(resource.Uri); uri != "" {
					textParts = append(textParts, fmt.Sprintf("Resource: %s", uri))
				}
			}
		}
	}

	return strings.TrimSpace(strings.Join(textParts, "\n")), contentBlocks
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func extractTextDelta(evt api.StreamEvent) string {
	if evt.Type != api.EventContentBlockDelta || evt.Delta == nil {
		return ""
	}
	if evt.Delta.Type != "text_delta" {
		return ""
	}
	return evt.Delta.Text
}

func extractStopReason(evt api.StreamEvent) string {
	if evt.Type != api.EventMessageDelta || evt.Delta == nil {
		return ""
	}
	return strings.TrimSpace(evt.Delta.StopReason)
}

func mapStopReason(reason string) acpproto.StopReason {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case string(acpproto.StopReasonCancelled):
		return acpproto.StopReasonCancelled
	case string(acpproto.StopReasonMaxTokens):
		return acpproto.StopReasonMaxTokens
	case string(acpproto.StopReasonMaxTurnRequests):
		return acpproto.StopReasonMaxTurnRequests
	case string(acpproto.StopReasonRefusal):
		return acpproto.StopReasonRefusal
	default:
		return acpproto.StopReasonEndTurn
	}
}

func isCancelledStreamError(output any) bool {
	if output == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(fmt.Sprint(output)))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "cancel") || strings.Contains(msg, "canceled") || strings.Contains(msg, "cancelled")
}

func streamEventError(evt api.StreamEvent) error {
	if evt.Type != api.EventError {
		return nil
	}
	msg := strings.TrimSpace(fmt.Sprint(evt.Output))
	if msg == "" {
		msg = "runtime stream error"
	}
	return errors.New(msg)
}

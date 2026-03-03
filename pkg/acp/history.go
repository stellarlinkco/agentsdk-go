package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/message"
	acpproto "github.com/coder/acp-go-sdk"
)

type persistedHistorySnapshot struct {
	Version   int               `json:"version"`
	SessionID string            `json:"session_id,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Messages  []message.Message `json:"messages,omitempty"`
}

func loadPersistedHistory(projectRoot string, sessionID acpproto.SessionId) ([]message.Message, bool, error) {
	path := historyFilePath(projectRoot, sessionID)
	if path == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read history: %w", err)
	}

	var wrapper persistedHistorySnapshot
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if wrapper.Version != 0 || wrapper.SessionID != "" || !wrapper.UpdatedAt.IsZero() || wrapper.Messages != nil {
			return message.CloneMessages(wrapper.Messages), true, nil
		}
	}

	var msgs []message.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, false, fmt.Errorf("decode history: %w", err)
	}
	return message.CloneMessages(msgs), true, nil
}

func historyFilePath(projectRoot string, sessionID acpproto.SessionId) string {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return ""
	}
	name := sanitizePathComponent(string(sessionID))
	if name == "" {
		return ""
	}
	return filepath.Join(projectRoot, ".claude", "history", name+".json")
}

func sanitizePathComponent(value string) string {
	const fallback = "default"
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		return fallback
	}
	return sanitized
}

func historyMessagesToSessionUpdates(msgs []message.Message) []acpproto.SessionUpdate {
	updates := make([]acpproto.SessionUpdate, 0, len(msgs))
	nextToolCallID := 0
	for _, msg := range msgs {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "user":
			updates = appendMessageContentUpdates(updates, msg, true)
		case "assistant":
			updates = appendMessageContentUpdates(updates, msg, false)
			if text := strings.TrimSpace(msg.ReasoningContent); text != "" {
				updates = append(updates, acpproto.UpdateAgentThoughtText(text))
			}
			for _, call := range msg.ToolCalls {
				toolID := normalizedToolCallID(call.ID, &nextToolCallID)
				title := strings.TrimSpace(call.Name)
				if title == "" {
					title = "tool"
				}
				updates = append(updates, acpproto.StartToolCall(
					toolID,
					title,
					acpproto.WithStartStatus(acpproto.ToolCallStatusPending),
					acpproto.WithStartRawInput(call.Arguments),
				))
			}
		case "tool":
			for _, call := range msg.ToolCalls {
				toolID := normalizedToolCallID(call.ID, &nextToolCallID)
				opts := []acpproto.ToolCallUpdateOpt{
					acpproto.WithUpdateStatus(acpproto.ToolCallStatusCompleted),
				}
				if output := strings.TrimSpace(call.Result); output != "" {
					opts = append(opts, acpproto.WithUpdateRawOutput(output))
				}
				updates = append(updates, acpproto.UpdateToolCall(toolID, opts...))
			}
		default:
			// system or unknown roles are not replayed to avoid exposing internal state.
		}
	}
	return updates
}

func appendMessageContentUpdates(dst []acpproto.SessionUpdate, msg message.Message, user bool) []acpproto.SessionUpdate {
	if len(msg.ContentBlocks) > 0 {
		for _, block := range msg.ContentBlocks {
			content, ok := messageBlockToContentBlock(block)
			if !ok {
				continue
			}
			if user {
				dst = append(dst, acpproto.UpdateUserMessage(content))
			} else {
				dst = append(dst, acpproto.UpdateAgentMessage(content))
			}
		}
		return dst
	}

	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return dst
	}
	if user {
		return append(dst, acpproto.UpdateUserMessageText(text))
	}
	return append(dst, acpproto.UpdateAgentMessageText(text))
}

func messageBlockToContentBlock(block message.ContentBlock) (acpproto.ContentBlock, bool) {
	switch block.Type {
	case message.ContentBlockText:
		text := strings.TrimSpace(block.Text)
		if text == "" {
			return acpproto.ContentBlock{}, false
		}
		return acpproto.TextBlock(text), true
	case message.ContentBlockImage:
		if data := strings.TrimSpace(block.Data); data != "" {
			return acpproto.ImageBlock(data, block.MediaType), true
		}
		if url := strings.TrimSpace(block.URL); url != "" {
			return acpproto.ResourceLinkBlock("image", url), true
		}
		return acpproto.ContentBlock{}, false
	case message.ContentBlockDocument:
		if url := strings.TrimSpace(block.URL); url != "" {
			return acpproto.ResourceLinkBlock("document", url), true
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			return acpproto.TextBlock(text), true
		}
		return acpproto.ContentBlock{}, false
	default:
		text := strings.TrimSpace(block.Text)
		if text == "" {
			return acpproto.ContentBlock{}, false
		}
		return acpproto.TextBlock(text), true
	}
}

func normalizedToolCallID(raw string, counter *int) acpproto.ToolCallId {
	id := strings.TrimSpace(raw)
	if id != "" {
		return acpproto.ToolCallId(id)
	}
	if counter != nil {
		*counter = *counter + 1
		return acpproto.ToolCallId(fmt.Sprintf("tool_call_%d", *counter))
	}
	return acpproto.ToolCallId("tool_call")
}

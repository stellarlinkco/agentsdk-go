package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

type chunkStream struct {
	cb          modelpkg.StreamCallback
	accumulator strings.Builder
	role        string

	toolBuilders map[int]*toolCallBuilder
	finalCalls   []modelpkg.ToolCall
	finished     bool
}

type toolCallBuilder struct {
	id   string
	name string
	args strings.Builder
}

func (b *toolCallBuilder) append(delta AssistantToolCallDelta) {
	if delta.ID != "" {
		b.id = delta.ID
	}
	if delta.Function != nil {
		if delta.Function.Name != "" {
			b.name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			b.args.WriteString(delta.Function.Arguments)
		}
	}
}

func (b *toolCallBuilder) finalize() (modelpkg.ToolCall, error) {
	raw := strings.TrimSpace(b.args.String())
	if raw == "" {
		return modelpkg.ToolCall{
			ID:        b.id,
			Name:      b.name,
			Arguments: map[string]any{},
		}, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return modelpkg.ToolCall{}, fmt.Errorf("decode streaming tool arguments: %w", err)
	}
	return modelpkg.ToolCall{
		ID:        b.id,
		Name:      b.name,
		Arguments: parsed,
	}, nil
}

func newChunkStream(cb modelpkg.StreamCallback) *chunkStream {
	return &chunkStream{
		cb:           cb,
		toolBuilders: map[int]*toolCallBuilder{},
	}
}

func (s *chunkStream) consume(ctx context.Context, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, initialStreamBufSize), maxStreamLineBytes)
	var dataBuf strings.Builder
	flush := func() error {
		if dataBuf.Len() == 0 {
			return nil
		}
		payload := strings.TrimSpace(dataBuf.String())
		dataBuf.Reset()
		if payload == "" {
			return nil
		}
		if payload == "[DONE]" {
			s.finished = true
			return io.EOF
		}
		var chunk ChatCompletionStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return fmt.Errorf("decode openai stream chunk: %w", err)
		}
		return s.processChunk(chunk)
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(strings.TrimSpace(line[5:]))
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flush(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (s *chunkStream) processChunk(chunk ChatCompletionStreamChunk) error {
	if len(chunk.Choices) == 0 {
		return nil
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Role != "" {
			s.role = choice.Delta.Role
		}
		if text := choice.Delta.Content.Text(); text != "" {
			s.accumulator.WriteString(text)
			if err := s.cb(modelpkg.StreamResult{
				Message: modelpkg.Message{Role: currentRole(s.role), Content: text},
			}); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.Index < 0 {
				continue
			}
			builder := s.toolBuilders[call.Index]
			if builder == nil {
				builder = &toolCallBuilder{}
				s.toolBuilders[call.Index] = builder
			}
			builder.append(call)
		}
		switch choice.FinishReason {
		case "tool_calls":
			if err := s.flushToolCalls(); err != nil {
				return err
			}
			s.finished = true
		case "stop":
			s.finished = true
		}
	}
	return nil
}

func (s *chunkStream) flushToolCalls() error {
	calls, err := s.buildToolCalls()
	if err != nil || len(calls) == 0 {
		return err
	}
	s.finalCalls = calls
	return s.cb(modelpkg.StreamResult{
		Message: modelpkg.Message{
			Role:      currentRole(s.role),
			ToolCalls: cloneToolCalls(calls),
		},
	})
}

func (s *chunkStream) buildToolCalls() ([]modelpkg.ToolCall, error) {
	if len(s.toolBuilders) == 0 {
		return nil, nil
	}
	indices := make([]int, 0, len(s.toolBuilders))
	for idx := range s.toolBuilders {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	calls := make([]modelpkg.ToolCall, 0, len(indices))
	for _, idx := range indices {
		builder := s.toolBuilders[idx]
		if builder == nil {
			continue
		}
		call, err := builder.finalize()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(call.Name) == "" {
			continue
		}
		calls = append(calls, call)
	}
	return calls, nil
}

func (s *chunkStream) finalize() error {
	if len(s.finalCalls) == 0 {
		calls, err := s.buildToolCalls()
		if err != nil {
			return err
		}
		s.finalCalls = calls
	}
	msg := modelpkg.Message{
		Role:      currentRole(s.role),
		Content:   s.accumulator.String(),
		ToolCalls: cloneToolCalls(s.finalCalls),
	}
	return s.cb(modelpkg.StreamResult{
		Message: msg,
		Final:   true,
	})
}

func cloneToolCalls(calls []modelpkg.ToolCall) []modelpkg.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]modelpkg.ToolCall, len(calls))
	copy(out, calls)
	return out
}

func currentRole(role string) string {
	if role == "" {
		return "assistant"
	}
	return role
}

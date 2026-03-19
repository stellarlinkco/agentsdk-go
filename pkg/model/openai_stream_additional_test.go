package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
)

type fakeSSEDecoder struct {
	events []ssestream.Event
	idx    int
	err    error
}

func (d *fakeSSEDecoder) Next() bool {
	if d.err != nil {
		return false
	}
	if d.idx >= len(d.events) {
		return false
	}
	d.idx++
	return true
}

func (d *fakeSSEDecoder) Event() ssestream.Event {
	if d.idx == 0 || d.idx > len(d.events) {
		return ssestream.Event{}
	}
	return d.events[d.idx-1]
}

func (d *fakeSSEDecoder) Close() error { return nil }
func (d *fakeSSEDecoder) Err() error   { return d.err }

type stubCompletions struct {
	newStreamingFn func(context.Context, openai.ChatCompletionNewParams, ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk]
}

func (s stubCompletions) New(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) (*openai.ChatCompletion, error) {
	_ = ctx
	_ = params
	_ = opts
	return nil, errors.New("not implemented")
}

func (s stubCompletions) NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams, opts ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk] {
	if s.newStreamingFn == nil {
		return nil
	}
	return s.newStreamingFn(ctx, params, opts...)
}

func TestNewOpenAI_RequiresAPIKey(t *testing.T) {
	_, err := NewOpenAI(OpenAIConfig{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestIsOpenAIRetryable_OpenAIErrorUnauthorized_NoRetry(t *testing.T) {
	if isOpenAIRetryable(&openai.Error{StatusCode: http.StatusUnauthorized}) {
		t.Fatalf("unauthorized should not be retryable")
	}
	if !isOpenAIRetryable(&openai.Error{StatusCode: http.StatusInternalServerError}) {
		t.Fatalf("500 should be retryable")
	}
}

func TestOpenAIModel_DoWithRetry_RetriesThenSucceeds(t *testing.T) {
	m := &openaiModel{maxRetries: 1}
	attempts := 0
	start := time.Now()
	err := m.doWithRetry(context.Background(), func(context.Context) error {
		attempts++
		if attempts == 1 {
			return stubNetError{timeout: true}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d", attempts)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("retry backoff took too long")
	}
}

func TestOpenAIDoWithRetry_ContextCanceledAfterFn(t *testing.T) {
	m := &openaiModel{maxRetries: 10}
	ctx, cancel := context.WithCancel(context.Background())
	err := m.doWithRetry(ctx, func(context.Context) error {
		cancel()
		return stubNetError{timeout: true}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenAIModel_CompleteStream_CallbackRequired(t *testing.T) {
	m := &openaiModel{}
	if err := m.CompleteStream(context.Background(), Request{}, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestOpenAIModel_CompleteStream_StreamNil(t *testing.T) {
	m := &openaiModel{
		completions: stubCompletions{
			newStreamingFn: func(context.Context, openai.ChatCompletionNewParams, ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk] {
				return nil
			},
		},
		maxRetries: 0,
	}
	err := m.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}, func(StreamResult) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestOpenAIModel_CompleteStream_AccumulatesDeltasReasoningUsageAndToolCalls(t *testing.T) {
	decoder := &fakeSSEDecoder{
		events: []ssestream.Event{
			{Data: []byte(`{"choices":[{"delta":{"role":"assistant","reasoning_content":"r1","content":"a"},"finish_reason":""}],"usage":{"total_tokens":0}}`)},
			{Data: []byte(`{"choices":[{"delta":{"content":"b","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"x\":1"}}]},"finish_reason":""}],"usage":{"total_tokens":0}}`)},
			{Data: []byte(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)},
		},
	}
	stream := ssestream.NewStream[openai.ChatCompletionChunk](decoder, nil)
	m := &openaiModel{
		completions: stubCompletions{
			newStreamingFn: func(context.Context, openai.ChatCompletionNewParams, ...option.RequestOption) *ssestream.Stream[openai.ChatCompletionChunk] {
				return stream
			},
		},
		model:      "gpt",
		maxTokens:  16,
		maxRetries: 0,
	}

	var (
		deltas []string
		final  *Response
		calls  []*ToolCall
	)
	err := m.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}, func(sr StreamResult) error {
		if sr.Delta != "" {
			deltas = append(deltas, sr.Delta)
		}
		if sr.ToolCall != nil {
			calls = append(calls, sr.ToolCall)
		}
		if sr.Final {
			final = sr.Response
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "a" || deltas[1] != "b" {
		t.Fatalf("deltas=%v", deltas)
	}
	if final == nil {
		t.Fatalf("expected final response")
	}
	if final.Message.Content != "ab" {
		t.Fatalf("content=%q", final.Message.Content)
	}
	if final.Message.ReasoningContent != "r1" {
		t.Fatalf("reasoning=%q", final.Message.ReasoningContent)
	}
	if final.Usage.TotalTokens != 3 {
		t.Fatalf("usage=%+v", final.Usage)
	}
	if len(final.Message.ToolCalls) != 1 {
		t.Fatalf("toolCalls=%v", final.Message.ToolCalls)
	}
	if len(calls) != 1 || calls[0] == nil || calls[0].Name != "bash" {
		t.Fatalf("streamed calls=%v", calls)
	}
	if v, ok := final.Message.ToolCalls[0].Arguments["raw"]; !ok || v != "{\"x\":1" {
		t.Fatalf("args=%v", final.Message.ToolCalls[0].Arguments)
	}
}

func TestConvertOpenAIResponse_ExtractsReasoningContentAndToolCalls(t *testing.T) {
	var completion openai.ChatCompletion
	if err := json.Unmarshal([]byte(`{
		"choices":[{"finish_reason":"stop","message":{
			"role":"assistant",
			"content":"ok",
			"tool_calls":[{"id":"t1","type":"function","function":{"name":"bash","arguments":"{\"a\":1}"}}],
			"reasoning_content":"why"
		}}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
	}`), &completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := convertOpenAIResponse(&completion)
	if resp == nil {
		t.Fatalf("nil resp")
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content=%q", resp.Message.Content)
	}
	if resp.Message.ReasoningContent != "why" {
		t.Fatalf("reasoning=%q", resp.Message.ReasoningContent)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "bash" {
		t.Fatalf("toolCalls=%v", resp.Message.ToolCalls)
	}
	if resp.Usage.TotalTokens != 2 {
		t.Fatalf("usage=%+v", resp.Usage)
	}
}

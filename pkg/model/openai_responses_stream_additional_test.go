package model

import (
	"context"
	"errors"
	"testing"

	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
)

func TestOpenAIResponsesModel_CompleteStream_AccumulatesDeltasUsageAndToolCalls(t *testing.T) {
	decoder := &fakeSSEDecoder{
		events: []ssestream.Event{
			{Data: []byte(`{"type":"response.output_text.delta","delta":"hi"}`)},
			{Data: []byte(`{"type":"response.output_item.added","item":{"type":"function_call","id":"item_1","name":"bash","call_id":"call_1"}}`)},
			{Data: []byte(`{"type":"response.function_call_arguments.delta","item_id":"item_1","arguments":"{\"a\":"}`)},
			{Data: []byte(`{"type":"response.function_call_arguments.done","item_id":"item_1","arguments":"{\"a\":1}"}`)},
			{Data: []byte(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`)},
		},
	}
	stream := ssestream.NewStream[responses.ResponseStreamEventUnion](decoder, nil)

	mock := &mockOpenAIResponses{
		streamFunc: func(context.Context, responses.ResponseNewParams, ...option.RequestOption) *ssestream.Stream[responses.ResponseStreamEventUnion] {
			return stream
		},
	}
	mdl := &openaiResponsesModel{
		responses:   mock,
		model:       "gpt-4o",
		maxTokens:   16,
		maxRetries:  0,
		system:      "",
		temperature: nil,
	}

	var (
		deltas []string
		calls  []*ToolCall
		final  *Response
	)
	err := mdl.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}, func(sr StreamResult) error {
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
	if len(deltas) != 1 || deltas[0] != "hi" {
		t.Fatalf("deltas=%v", deltas)
	}
	if len(calls) != 1 || calls[0] == nil {
		t.Fatalf("calls=%v", calls)
	}
	if calls[0].ID != "call_1" || calls[0].Name != "bash" {
		t.Fatalf("toolcall=%+v", calls[0])
	}
	if final == nil {
		t.Fatalf("expected final response")
	}
	if final.Message.Content != "hi" {
		t.Fatalf("content=%q", final.Message.Content)
	}
	if len(final.Message.ToolCalls) != 1 || final.Message.ToolCalls[0].ID != "call_1" {
		t.Fatalf("final toolcalls=%v", final.Message.ToolCalls)
	}
	if final.StopReason != "tool_calls" {
		t.Fatalf("stopReason=%q", final.StopReason)
	}
	if final.Usage.TotalTokens != 3 {
		t.Fatalf("usage=%+v", final.Usage)
	}
}

func TestOpenAIResponsesModel_CompleteStream_NilStream(t *testing.T) {
	mock := &mockOpenAIResponses{}
	mdl := &openaiResponsesModel{
		responses:  mock,
		model:      "gpt-4o",
		maxTokens:  16,
		maxRetries: 0,
	}
	err := mdl.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}, func(StreamResult) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestOpenAIResponsesModel_CompleteStream_PropagatesStreamErr(t *testing.T) {
	stream := ssestream.NewStream[responses.ResponseStreamEventUnion](&fakeSSEDecoder{err: errors.New("sse")}, nil)
	mock := &mockOpenAIResponses{
		streamFunc: func(context.Context, responses.ResponseNewParams, ...option.RequestOption) *ssestream.Stream[responses.ResponseStreamEventUnion] {
			return stream
		},
	}
	mdl := &openaiResponsesModel{
		responses:  mock,
		model:      "gpt-4o",
		maxTokens:  16,
		maxRetries: 0,
	}
	err := mdl.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}, func(StreamResult) error {
		return nil
	})
	if err == nil || err.Error() != "sse" {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenAIResponsesModel_CompleteStream_CallbackErrors(t *testing.T) {
	t.Run("delta callback error", func(t *testing.T) {
		decoder := &fakeSSEDecoder{
			events: []ssestream.Event{
				{Data: []byte(`{"type":"response.output_text.delta","delta":"hi"}`)},
			},
		}
		stream := ssestream.NewStream[responses.ResponseStreamEventUnion](decoder, nil)
		mock := &mockOpenAIResponses{
			streamFunc: func(context.Context, responses.ResponseNewParams, ...option.RequestOption) *ssestream.Stream[responses.ResponseStreamEventUnion] {
				return stream
			},
		}
		mdl := &openaiResponsesModel{responses: mock, model: "gpt-4o", maxTokens: 16, maxRetries: 0}
		want := errors.New("cb")
		err := mdl.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}, func(sr StreamResult) error {
			if sr.Delta != "" {
				return want
			}
			return nil
		})
		if !errors.Is(err, want) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("toolcall callback error", func(t *testing.T) {
		decoder := &fakeSSEDecoder{
			events: []ssestream.Event{
				{Data: []byte(`{"type":"response.output_item.added","item":{"type":"function_call","id":"item_1","name":"bash","call_id":"call_1"}}`)},
				{Data: []byte(`{"type":"response.function_call_arguments.done","item_id":"item_1","arguments":"{\"a\":1}"}`)},
			},
		}
		stream := ssestream.NewStream[responses.ResponseStreamEventUnion](decoder, nil)
		mock := &mockOpenAIResponses{
			streamFunc: func(context.Context, responses.ResponseNewParams, ...option.RequestOption) *ssestream.Stream[responses.ResponseStreamEventUnion] {
				return stream
			},
		}
		mdl := &openaiResponsesModel{responses: mock, model: "gpt-4o", maxTokens: 16, maxRetries: 0}
		want := errors.New("cb")
		err := mdl.CompleteStream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}, func(sr StreamResult) error {
			if sr.ToolCall != nil {
				return want
			}
			return nil
		})
		if !errors.Is(err, want) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestOpenAIResponsesDoWithRetry_ContextCanceledAfterFn(t *testing.T) {
	m := &openaiResponsesModel{maxRetries: 10}
	ctx, cancel := context.WithCancel(context.Background())
	err := m.doWithRetry(ctx, func(context.Context) error {
		cancel()
		return stubNetError{timeout: true}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

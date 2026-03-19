package api

import (
	"context"
	"encoding/json"

	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

// streamEmitFunc is stored on context so tools can push incremental output
// without depending on middleware details.
type streamEmitFunc func(context.Context, StreamEvent)

// newProgressMiddleware surfaces Anthropic-compatible SSE progress events at
// each middleware interception point. The event ordering mirrors Anthropic's
// streaming payloads while adding agent/tool lifecycle markers.
func newProgressMiddleware(events chan<- StreamEvent) *progressMiddleware {
	return &progressMiddleware{emitter: progressEmitter{ch: events}}
}

// progressMiddleware centralises guarded writes to the event channel so the
// middleware hooks stay terse and ordered.
type progressMiddleware struct {
	emitter progressEmitter
}

func (p *progressMiddleware) Name() string { return "progress" }

func (p *progressMiddleware) emit(ctx context.Context, evt StreamEvent) {
	p.emitter.emit(ctx, evt)
}

func (p *progressMiddleware) streamEmit() streamEmitFunc {
	return p.emit
}

func (p *progressMiddleware) BeforeAgent(ctx context.Context, st *middleware.State) error {
	iter := 0
	if st != nil {
		iter = st.Iteration
	}
	if iter == 0 {
		p.emit(context.Background(), StreamEvent{Type: EventAgentStart})
	}
	p.emit(ctx, StreamEvent{Type: EventIterationStart, Iteration: &iter})
	p.emit(ctx, StreamEvent{Type: EventMessageStart, Message: &Message{Role: "assistant"}})
	return nil
}

func (p *progressMiddleware) AfterAgent(ctx context.Context, st *middleware.State) error {
	resp, ok := st.ModelOutput.(*model.Response)
	if !ok || resp == nil {
		return nil
	}

	idx := 0
	text := resp.Message.Content
	p.textBlock(ctx, idx, text)
	if text != "" {
		idx++
	}

	for _, call := range resp.Message.ToolCalls {
		p.toolBlock(ctx, idx, call)
		idx++
	}

	reason := "end_turn"
	if len(resp.Message.ToolCalls) > 0 {
		reason = "tool_use"
	}
	p.emit(ctx, StreamEvent{Type: EventMessageDelta, Delta: &Delta{StopReason: reason}, Usage: &Usage{}})
	p.emit(ctx, StreamEvent{Type: EventMessageStop})
	iter := st.Iteration
	p.emit(ctx, StreamEvent{Type: EventIterationStop, Iteration: &iter})
	if len(resp.Message.ToolCalls) == 0 {
		p.emit(ctx, StreamEvent{Type: EventAgentStop})
	}
	return nil
}

func (p *progressMiddleware) BeforeTool(ctx context.Context, st *middleware.State) error {
	call, ok := st.ToolCall.(model.ToolCall)
	if !ok {
		return nil
	}
	iter := st.Iteration
	p.emit(ctx, StreamEvent{Type: EventToolExecutionStart, ToolUseID: call.ID, Name: call.Name, Iteration: &iter})
	return nil
}

func (p *progressMiddleware) AfterTool(ctx context.Context, st *middleware.State) error {
	call, ok := st.ToolCall.(model.ToolCall)
	if !ok {
		return nil
	}
	cr, ok := st.ToolResult.(*tool.CallResult)
	if !ok || cr == nil {
		return nil
	}

	output := ""
	if cr.Result != nil {
		output = cr.Result.Output
	}
	if output != "" {
		p.emit(ctx, StreamEvent{Type: EventToolExecutionOutput, ToolUseID: call.ID, Name: call.Name, Output: output})
	}

	payload := map[string]any{}
	if output != "" {
		payload["output"] = output
	}
	meta := map[string]any{}
	if cr.Err != nil {
		meta["error"] = cr.Err.Error()
	}
	if cr.Result != nil {
		if cr.Result.Data != nil {
			meta["data"] = cr.Result.Data
		}
		if cr.Result.OutputRef != nil {
			meta["output_ref"] = cr.Result.OutputRef
		}
	}
	if len(meta) > 0 {
		payload["metadata"] = meta
	}
	p.emit(ctx, StreamEvent{Type: EventToolExecutionResult, ToolUseID: call.ID, Name: call.Name, Output: payload})
	return nil
}

func (p *progressMiddleware) textBlock(ctx context.Context, idx int, content string) {
	if content == "" {
		return
	}
	p.emit(ctx, StreamEvent{Type: EventContentBlockStart, Index: &idx, ContentBlock: &ContentBlock{Type: "text"}})
	for _, r := range content {
		p.emit(ctx, StreamEvent{Type: EventContentBlockDelta, Index: &idx, Delta: &Delta{Type: "text_delta", Text: string(r)}})
	}
	p.emit(ctx, StreamEvent{Type: EventContentBlockStop, Index: &idx})
}

func (p *progressMiddleware) toolBlock(ctx context.Context, idx int, call model.ToolCall) {
	p.emit(ctx, StreamEvent{Type: EventContentBlockStart, Index: &idx, ContentBlock: &ContentBlock{Type: "tool_use", ID: call.ID, Name: call.Name}})
	raw, err := json.Marshal(call.Arguments)
	if err != nil {
		raw = []byte("{}")
	}
	for _, chunk := range chunkString(string(raw), 10) {
		encoded, err := json.Marshal(chunk)
		if err != nil {
			encoded = []byte(`""`)
		}
		p.emit(ctx, StreamEvent{Type: EventContentBlockDelta, Index: &idx, Delta: &Delta{Type: "input_json_delta", PartialJSON: json.RawMessage(encoded)}})
	}
	p.emit(ctx, StreamEvent{Type: EventContentBlockStop, Index: &idx})
}

type progressEmitter struct {
	ch chan<- StreamEvent
}

func (e progressEmitter) emit(ctx context.Context, evt StreamEvent) {
	if e.ch == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 阻塞发送保证事件不会静默丢失；在 context 取消时优雅返回。
	select {
	case <-ctx.Done():
		return
	case e.ch <- evt:
	}
}

// chunkString splits s into roughly equal sized pieces without dropping
// remainder characters to support streaming partial JSON/tool output.
func chunkString(s string, size int) []string {
	if size <= 0 || s == "" {
		return nil
	}
	out := make([]string, 0, (len(s)+size-1)/size)
	for start := 0; start < len(s); start += size {
		end := start + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[start:end])
	}
	return out
}

package api

import "encoding/json"

// Anthropic-compatible SSE event types.
const (
	// EventMessageStart marks the beginning of a new message envelope.
	EventMessageStart = "message_start"
	// EventContentBlockStart indicates that a content block (text/tool use) begins streaming.
	EventContentBlockStart = "content_block_start"
	// EventContentBlockDelta carries incremental text or tool input deltas.
	EventContentBlockDelta = "content_block_delta"
	// EventContentBlockStop signals that the content block transmission is finished.
	EventContentBlockStop = "content_block_stop"
	// EventMessageDelta represents aggregated message-level deltas such as usage updates.
	EventMessageDelta = "message_delta"
	// EventMessageStop denotes completion of the message, including final stop reasons.
	EventMessageStop = "message_stop"
	// EventPing is a keepalive heartbeat used by Anthropic streams.
	EventPing = "ping"

	// Agent-specific extension events.
	EventAgentStart          = "agent_start"
	EventAgentStop           = "agent_stop"
	EventIterationStart      = "iteration_start"
	EventIterationStop       = "iteration_stop"
	EventToolExecutionStart  = "tool_execution_start"
	EventToolExecutionOutput = "tool_execution_output"
	EventToolExecutionResult = "tool_execution_result"
	EventError               = "error"
)

// StreamEvent represents a single SSE dispatch compatible with Anthropic's schema
// while carrying additional metadata needed by the agent runtime.
type StreamEvent struct {
	Type string `json:"type"` // Type identifies the concrete SSE event kind.

	// Anthropic-compatible payloads.
	Message      *Message      `json:"message,omitempty"`       // Message holds the envelope for message_* events.
	Index        *int          `json:"index,omitempty"`         // Index orders content blocks/deltas within the message.
	ContentBlock *ContentBlock `json:"content_block,omitempty"` // ContentBlock contains partial or full block data.
	Delta        *Delta        `json:"delta,omitempty"`         // Delta carries incremental text/tool updates.
	Usage        *Usage        `json:"usage,omitempty"`         // Usage records token consumption snapshots.

	// Agent-specific extensions.
	ToolUseID string      `json:"tool_use_id,omitempty"`      // ToolUseID associates tool events with tool execution records.
	Name      string      `json:"name,omitempty"`             // Name describes the agent, tool, or block responsible for the event.
	Output    interface{} `json:"output,omitempty"`           // Output captures arbitrary structured payloads (e.g., tool stdout).
	IsError   *bool       `json:"is_error,omitempty"`         // IsError flags whether the event describes an error condition.
	SessionID string      `json:"session_id,omitempty"`       // SessionID ties events to a long-lived agent session.
	Iteration *int        `json:"iteration,omitempty"`        // Iteration indicates the current agent iteration, if applicable.
	TotalIter *int        `json:"total_iterations,omitempty"` // TotalIter reports the planned maximum iteration count.
}

// Message represents the Anthropic message envelope streamed over SSE.
type Message struct {
	ID    string `json:"id,omitempty"`    // ID uniquely identifies the message on the Anthropic service.
	Type  string `json:"type,omitempty"`  // Type is typically "message".
	Role  string `json:"role,omitempty"`  // Role is "user", "assistant", etc.
	Model string `json:"model,omitempty"` // Model states which Anthropic model generated the content.
	Usage *Usage `json:"usage,omitempty"` // Usage accumulates total tokens for the message lifecycle.
}

// ContentBlock carries either text segments or tool invocation details.
type ContentBlock struct {
	Type  string          `json:"type,omitempty"`  // Type is "text" or "tool_use".
	Text  string          `json:"text,omitempty"`  // Text contains streamed text when Type == "text".
	ID    string          `json:"id,omitempty"`    // ID uniquely names the content block.
	Name  string          `json:"name,omitempty"`  // Name describes the tool/function when Type == "tool_use".
	Input json.RawMessage `json:"input,omitempty"` // Input stores raw JSON arguments supplied to the tool.
}

// Delta models incremental updates for content blocks or message-level stop data.
type Delta struct {
	Type        string          `json:"type,omitempty"`         // Type is "text_delta" or "input_json_delta".
	Text        string          `json:"text,omitempty"`         // Text contains the appended text fragment when Type == "text_delta".
	PartialJSON json.RawMessage `json:"partial_json,omitempty"` // PartialJSON carries incremental JSON payload slices for tools.
	StopReason  string          `json:"stop_reason,omitempty"`  // StopReason explains why a message terminated.
}

// Usage records token accounting snapshots compatible with Anthropic payloads.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`  // InputTokens counts tokens provided in the prompt.
	OutputTokens int `json:"output_tokens,omitempty"` // OutputTokens counts tokens produced by the model/toolchain.
}

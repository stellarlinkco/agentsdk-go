package hooks

import (
	"fmt"
	"time"
)

// EventType enumerates all hookable lifecycle events supported by the SDK.
// Keeping the list small and explicit prevents accidental proliferation of
// loosely defined event names.
type EventType string

const (
	PreToolUse    EventType = "PreToolUse"
	PostToolUse   EventType = "PostToolUse"
	SessionStart  EventType = "SessionStart"
	SessionEnd    EventType = "SessionEnd"
	Stop          EventType = "Stop"
	SubagentStart EventType = "SubagentStart"
	SubagentStop  EventType = "SubagentStop"
)

// Event represents a single occurrence in the system. It is intentionally
// lightweight; any structured payloads are stored in the Payload field.
type Event struct {
	ID        string      // optional explicit identifier; generated when empty
	Type      EventType   // required
	Timestamp time.Time   // auto-populated when zero
	SessionID string      // optional session identifier for hook payloads
	RequestID string      // optional request identifier for distributed tracing
	Payload   interface{} // optional, type asserted by hook executors
}

// Validate performs cheap sanity checks for callers that need stronger
// contracts than the zero-value guarantees.
func (e Event) Validate() error {
	if e.Type == "" {
		return fmt.Errorf("events: missing type")
	}
	return nil
}

// ToolUsePayload is emitted before tool execution.
type ToolUsePayload struct {
	Name      string
	Params    map[string]any
	ToolUseID string // unique identifier for this tool use
}

// ToolResultPayload is emitted after tool execution.
type ToolResultPayload struct {
	Name      string
	Params    map[string]any // original tool input params
	ToolUseID string         // matches the ToolUsePayload.ToolUseID
	Result    any
	Duration  time.Duration
	Err       error
}

// SessionStartPayload is emitted when a session starts.
type SessionStartPayload struct {
	SessionID string
	Source    string // entry point source (e.g., "cli", "api")
	Model     string // model being used
	AgentType string // agent type (e.g., "main", "subagent")
	Metadata  map[string]any
}

// SessionEndPayload is emitted when a session ends.
type SessionEndPayload struct {
	SessionID string
	Reason    string // reason for ending (e.g., "user_exit", "error")
	Metadata  map[string]any
}

// StopPayload indicates a stop notification for the main agent.
type StopPayload struct {
	Reason         string
	StopHookActive bool // whether a stop hook is currently active
}

// SubagentStopPayload is emitted when a subagent stops independently.
type SubagentStopPayload struct {
	Name           string
	Reason         string
	AgentID        string // unique identifier for the subagent instance
	AgentType      string // type of the subagent
	TranscriptPath string // path to the subagent transcript file
	StopHookActive bool   // whether a stop hook is currently active
}

// SubagentStartPayload is emitted when a subagent starts.
type SubagentStartPayload struct {
	Name      string
	AgentID   string         // unique identifier for the subagent instance
	AgentType string         // type of the subagent
	Metadata  map[string]any // optional metadata
}

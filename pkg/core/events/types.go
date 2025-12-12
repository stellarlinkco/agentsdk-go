package events

import (
	"fmt"
	"time"
)

// EventType enumerates all hookable lifecycle events supported by the SDK.
// Keeping the list small and explicit prevents accidental proliferation of
// loosely defined event names.
type EventType string

const (
	PreToolUse        EventType = "PreToolUse"
	PostToolUse       EventType = "PostToolUse"
	UserPromptSubmit  EventType = "UserPromptSubmit"
	SessionStart      EventType = "SessionStart"
	SessionEnd        EventType = "SessionEnd"
	Stop              EventType = "Stop"
	SubagentStart     EventType = "SubagentStart"
	SubagentStop      EventType = "SubagentStop"
	Notification      EventType = "Notification"
	PermissionRequest EventType = "PermissionRequest"
)

// Event represents a single occurrence in the system. It is intentionally
// lightweight; any structured payloads are stored in the Payload field.
type Event struct {
	ID        string      // optional explicit identifier; generated when empty
	Type      EventType   // required
	Timestamp time.Time   // auto-populated when zero
	SessionID string      // optional session identifier for hook payloads
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
	Name   string
	Params map[string]any
}

// ToolResultPayload is emitted after tool execution.
type ToolResultPayload struct {
	Name     string
	Result   any
	Duration time.Duration
	Err      error
}

// UserPromptPayload captures a user supplied prompt.
type UserPromptPayload struct {
	Prompt string
}

// SessionPayload signals session lifecycle transitions.
type SessionPayload struct {
	SessionID string
	Metadata  map[string]any
}

// StopPayload indicates a stop notification for the main agent.
type StopPayload struct {
	Reason string
}

// SubagentStopPayload is emitted when a subagent stops independently.
type SubagentStopPayload struct {
	Name           string
	Reason         string
	AgentID        string // unique identifier for the subagent instance
	TranscriptPath string // path to the subagent transcript file
}

// SubagentStartPayload is emitted when a subagent starts.
type SubagentStartPayload struct {
	Name     string
	AgentID  string         // unique identifier for the subagent instance
	Metadata map[string]any // optional metadata
}

// PermissionRequestPayload is emitted when a tool requests permission.
type PermissionRequestPayload struct {
	ToolName   string
	ToolParams map[string]any
	Reason     string // optional reason for the permission request
}

// PermissionDecisionType represents the decision from a permission request hook.
type PermissionDecisionType string

const (
	PermissionAllow PermissionDecisionType = "allow"
	PermissionDeny  PermissionDecisionType = "deny"
	PermissionAsk   PermissionDecisionType = "ask"
)

// NotificationPayload transports informational messages.
type NotificationPayload struct {
	Message string
	Meta    map[string]any
}

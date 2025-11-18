package hooks

import (
	"context"

	"github.com/cexll/agentsdk-go/pkg/core/events"
)

// Individual hook interfaces allow hook implementations to opt-in to only the
// callbacks they care about while keeping type safety.
type (
	PreToolUseHook interface {
		PreToolUse(context.Context, events.ToolUsePayload) error
	}
	PostToolUseHook interface {
		PostToolUse(context.Context, events.ToolResultPayload) error
	}
	UserPromptSubmitHook interface {
		UserPromptSubmit(context.Context, events.UserPromptPayload) error
	}
	SessionStartHook interface {
		SessionStart(context.Context, events.SessionPayload) error
	}
	StopHook interface {
		Stop(context.Context, events.StopPayload) error
	}
	SubagentStopHook interface {
		SubagentStop(context.Context, events.SubagentStopPayload) error
	}
	NotificationHook interface {
		Notification(context.Context, events.NotificationPayload) error
	}
)

// AllHook represents a strongly typed hook object; individual methods remain
// optional via the narrow interfaces above.
type AllHook interface {
	PreToolUseHook
	PostToolUseHook
	UserPromptSubmitHook
	SessionStartHook
	StopHook
	SubagentStopHook
	NotificationHook
}

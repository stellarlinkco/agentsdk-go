package acp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/api"
	coreevents "github.com/cexll/agentsdk-go/pkg/core/events"
	acpproto "github.com/coder/acp-go-sdk"
)

const (
	permissionOptionAllowOnce    acpproto.PermissionOptionId = "allow_once"
	permissionOptionAllowAlways  acpproto.PermissionOptionId = "allow_always"
	permissionOptionRejectOnce   acpproto.PermissionOptionId = "reject_once"
	permissionOptionRejectAlways acpproto.PermissionOptionId = "reject_always"
)

func (a *Adapter) newPermissionBridge(state *sessionState, fallback api.PermissionRequestHandler) api.PermissionRequestHandler {
	return func(ctx context.Context, req api.PermissionRequest) (coreevents.PermissionDecisionType, error) {
		switch state.permissionMode() {
		case permissionModeAllowAlways:
			return coreevents.PermissionAllow, nil
		case permissionModeDenyAlways:
			return coreevents.PermissionDeny, nil
		}

		decision, handled, err := a.requestPermissionFromClient(ctx, state.id, req)
		if err != nil {
			if fallback != nil {
				return fallback(ctx, req)
			}
			return coreevents.PermissionAsk, err
		}
		if handled {
			return decision, nil
		}

		if fallback != nil {
			return fallback(ctx, req)
		}
		return coreevents.PermissionAsk, nil
	}
}

func (a *Adapter) requestPermissionFromClient(ctx context.Context, sessionID acpproto.SessionId, req api.PermissionRequest) (coreevents.PermissionDecisionType, bool, error) {
	conn := a.connection()
	if conn == nil {
		return coreevents.PermissionAsk, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	toolName := strings.TrimSpace(req.ToolName)
	if toolName == "" {
		toolName = "tool"
	}
	title := fmt.Sprintf("Allow %s?", toolName)

	toolCall := acpproto.ToolCallUpdate{
		ToolCallId: acpproto.ToolCallId(fmt.Sprintf("perm_%d", time.Now().UnixNano())),
		Title:      acpproto.Ptr(title),
		Status:     acpproto.Ptr(acpproto.ToolCallStatusPending),
		RawInput:   req.ToolParams,
	}
	if target := strings.TrimSpace(req.Target); target != "" && filepath.IsAbs(target) {
		toolCall.Locations = []acpproto.ToolCallLocation{{Path: target}}
	}

	resp, err := conn.RequestPermission(ctx, acpproto.RequestPermissionRequest{
		SessionId: sessionID,
		ToolCall:  toolCall,
		Options: []acpproto.PermissionOption{
			{Kind: acpproto.PermissionOptionKindAllowOnce, Name: "Allow once", OptionId: permissionOptionAllowOnce},
			{Kind: acpproto.PermissionOptionKindAllowAlways, Name: "Allow always", OptionId: permissionOptionAllowAlways},
			{Kind: acpproto.PermissionOptionKindRejectOnce, Name: "Reject once", OptionId: permissionOptionRejectOnce},
			{Kind: acpproto.PermissionOptionKindRejectAlways, Name: "Reject always", OptionId: permissionOptionRejectAlways},
		},
	})
	if err != nil {
		return coreevents.PermissionAsk, true, err
	}

	return mapPermissionOutcome(resp.Outcome), true, nil
}

func mapPermissionOutcome(outcome acpproto.RequestPermissionOutcome) coreevents.PermissionDecisionType {
	if outcome.Cancelled != nil {
		return coreevents.PermissionDeny
	}
	if outcome.Selected == nil {
		return coreevents.PermissionAsk
	}

	switch outcome.Selected.OptionId {
	case permissionOptionAllowOnce, permissionOptionAllowAlways:
		return coreevents.PermissionAllow
	case permissionOptionRejectOnce, permissionOptionRejectAlways:
		return coreevents.PermissionDeny
	default:
		return coreevents.PermissionAsk
	}
}

package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	"github.com/stellarlinkco/agentsdk-go/pkg/message"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

type runtimeToolExecutor struct {
	executor  *tool.Executor
	hooks     *runtimeHookAdapter
	history   *message.History
	allow     map[string]struct{}
	root      string
	host      string
	sessionID string
}

func (t *runtimeToolExecutor) measureUsage() sandbox.ResourceUsage {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return sandbox.ResourceUsage{MemoryBytes: stats.Alloc}
}

func (t *runtimeToolExecutor) isAllowed(ctx context.Context, name string) bool {
	canon := canonicalToolName(name)
	if canon == "" {
		return false
	}
	reqAllowed := len(t.allow) == 0
	if len(t.allow) > 0 {
		_, reqAllowed = t.allow[canon]
	}
	subCtx, ok := subagents.FromContext(ctx)
	if !ok || len(subCtx.ToolWhitelist) == 0 {
		return reqAllowed
	}
	subSet := toLowerSet(subCtx.ToolWhitelist)
	if len(subSet) == 0 {
		return reqAllowed
	}
	_, subAllowed := subSet[canon]
	if len(t.allow) == 0 {
		return subAllowed
	}
	return reqAllowed && subAllowed
}

func (t *runtimeToolExecutor) Execute(ctx context.Context, call model.ToolCall) (*tool.CallResult, error) {
	if t.executor == nil {
		return nil, errors.New("tool executor not initialised")
	}
	if !t.isAllowed(ctx, call.Name) {
		return nil, fmt.Errorf("tool %s is not whitelisted", call.Name)
	}

	appendToolResult := func(content string) {
		if t.history == nil {
			return
		}
		t.history.Append(message.Message{
			Role: "tool",
			ToolCalls: []message.ToolCall{{
				ID:     call.ID,
				Name:   call.Name,
				Result: content,
			}},
		})
	}

	if len(call.Arguments) == 0 {
		if reg := t.executor.Registry(); reg != nil {
			if impl, err := reg.Get(call.Name); err == nil {
				if schema := impl.Schema(); schema != nil && len(schema.Required) > 0 {
					errMsg := fmt.Sprintf(
						"tool %q called with empty arguments but requires %v; "+
							"the API proxy likely stripped tool_use.input — check proxy configuration",
						call.Name, schema.Required)
					log.Printf("WARNING: %s (id=%s)", errMsg, call.ID)
					appendToolResult(errMsg)
					now := time.Now()
					return &tool.CallResult{
						Call:        tool.Call{Name: call.Name, Params: map[string]any{}, SessionID: t.sessionID},
						Result:      &tool.ToolResult{Success: false, Output: errMsg, Data: map[string]any{"error": "empty_arguments"}},
						StartedAt:   now,
						CompletedAt: now,
					}, nil
				}
			}
		}
	}

	var (
		params map[string]any
		preErr error
	)
	if t.hooks != nil {
		params, preErr = t.hooks.PreToolUse(ctx, coreToolUsePayload(call))
	}
	if preErr != nil {
		errContent := fmt.Sprintf(`{"error":%q}`, preErr.Error())
		appendToolResult(errContent)
		now := time.Now()
		return &tool.CallResult{
			Call:        tool.Call{Name: call.Name, Params: map[string]any{}, SessionID: t.sessionID},
			Result:      &tool.ToolResult{Success: false, Output: errContent, Data: map[string]any{"error": preErr.Error()}},
			Err:         preErr,
			StartedAt:   now,
			CompletedAt: now,
		}, preErr
	}
	if params != nil {
		call.Arguments = params
	}

	callSpec := tool.Call{
		Name:      call.Name,
		Params:    call.Arguments,
		Path:      t.root,
		Host:      t.host,
		Usage:     t.measureUsage(),
		SessionID: t.sessionID,
	}
	if emit := streamEmitFromContext(ctx); emit != nil {
		callSpec.StreamSink = func(chunk string, isStderr bool) {
			evt := StreamEvent{
				Type:      EventToolExecutionOutput,
				ToolUseID: call.ID,
				Name:      call.Name,
				Output:    chunk,
			}
			evt.IsStderr = &isStderr
			emit(ctx, evt)
		}
	}

	result, err := t.executor.Execute(ctx, callSpec)

	content := ""
	if result != nil && result.Result != nil {
		content = result.Result.Output
	}
	if err != nil {
		content = fmt.Sprintf(`{"error":%q}`, err.Error())
	}

	if t.hooks != nil {
		if hookErr := t.hooks.PostToolUse(ctx, coreToolResultPayload(call, result, err)); hookErr != nil && err == nil {
			appendToolResult(content)
			return result, hookErr
		}
	}

	appendToolResult(content)
	return result, err
}

func coreToolUsePayload(call model.ToolCall) hooks.ToolUsePayload {
	return hooks.ToolUsePayload{Name: call.Name, Params: call.Arguments}
}

func coreToolResultPayload(call model.ToolCall, res *tool.CallResult, err error) hooks.ToolResultPayload {
	payload := hooks.ToolResultPayload{Name: call.Name}
	if res != nil && res.Result != nil {
		payload.Result = res.Result.Output
		payload.Duration = res.Duration()
	}
	payload.Err = err
	return payload
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
	skylark   *skylarkAllowState
	root      string
	host      string
	sessionID string

	outputInlineMaxRunes  int
	outputSnippetMaxRunes int
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
	if t.skylark != nil {
		if !t.skylark.isAllowed(name) {
			return false
		}
		subCtx, ok := subagents.FromContext(ctx)
		if !ok || len(subCtx.ToolWhitelist) == 0 {
			return true
		}
		subSet := toLowerSet(subCtx.ToolWhitelist)
		_, subAllowed := subSet[canon]
		return subAllowed
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

func (t *runtimeToolExecutor) appendToolMessage(call model.ToolCall, content string) {
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

func (t *runtimeToolExecutor) maybeCompressToolOutput(call model.ToolCall, result *tool.CallResult, content string) string {
	// If executor already persisted output and returned a reference, keep it as-is.
	if result != nil && result.Result != nil && result.Result.OutputRef != nil {
		return content
	}
	text := strings.TrimSpace(content)
	if text == "" {
		return content
	}
	inlineMax := t.outputInlineMaxRunes
	if inlineMax <= 0 {
		inlineMax = 4000
	}
	snippetMax := t.outputSnippetMaxRunes
	if snippetMax <= 0 {
		snippetMax = 900
	}

	if len([]rune(text)) <= inlineMax {
		return content
	}

	path := ""
	if t != nil {
		base := toolOutputSessionDir(t.sessionID)
		toolDir := sanitizePathComponent(call.Name)
		dir := filepath.Join(base, toolDir)
		if err := os.MkdirAll(dir, 0o700); err == nil {
			filename := fmt.Sprintf("%d.output", time.Now().UnixNano())
			outPath := filepath.Join(dir, filename)
			if err := os.WriteFile(outPath, []byte(text), 0o600); err == nil {
				path = outPath
			}
		}
	}

	snippet := summarizeToolOutput(text, snippetMax)
	if path != "" {
		return fmt.Sprintf("[Output saved to: %s]\n\n%s", path, snippet)
	}
	return snippet
}

func summarizeToolOutput(raw string, maxRunes int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 900
	}

	// JSON summary: try object/array. If it parses, summarize keys and size.
	var anyVal any
	if json.Unmarshal([]byte(raw), &anyVal) == nil {
		switch v := anyVal.(type) {
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) > 30 {
				keys = append(keys[:30], "…")
			}
			head := fmt.Sprintf("JSON object (keys=%d): %s", len(v), strings.Join(keys, ", "))
			return cropToolOutputRunes(head, maxRunes)
		case []any:
			head := fmt.Sprintf("JSON array (len=%d)", len(v))
			return cropToolOutputRunes(head, maxRunes)
		default:
			// fall through to generic
		}
	}

	// Log-ish summary: keep head + tail lines.
	lines := strings.Split(raw, "\n")
	if len(lines) <= 1 {
		return cropToolOutputRunes(raw, maxRunes)
	}
	headN, tailN := 12, 8
	if len(lines) < headN+tailN+1 {
		return cropToolOutputRunes(raw, maxRunes)
	}
	head := lines[:headN]
	tail := lines[len(lines)-tailN:]
	s := strings.TrimSpace(strings.Join(head, "\n") + "\n...\n" + strings.Join(tail, "\n"))
	return cropToolOutputRunes(s, maxRunes)
}

func cropToolOutputRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// toolPreparation holds the outcome of allow/empty-args/pre-hook phases (no registry invoke).
type toolPreparation struct {
	Call model.ToolCall

	// Denied is set when the tool is not allowed (no history mutation here).
	Denied error

	// EmptyArgsResult is returned when required params are missing (caller should append + skip invoke).
	EmptyArgsResult *tool.CallResult

	// PreHookErr is set when PreToolUse fails (caller should append error JSON + skip invoke).
	PreHookErr error
}

func (t *runtimeToolExecutor) prepareToolCall(ctx context.Context, call model.ToolCall) toolPreparation {
	if t.executor == nil {
		return toolPreparation{Call: call, Denied: errors.New("tool executor not initialised")}
	}
	if !t.isAllowed(ctx, call.Name) {
		return toolPreparation{Call: call, Denied: fmt.Errorf("tool %s is not whitelisted", call.Name)}
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
					now := time.Now()
					return toolPreparation{
						Call: call,
						EmptyArgsResult: &tool.CallResult{
							Call:        tool.Call{Name: call.Name, Params: map[string]any{}, SessionID: t.sessionID},
							Result:      &tool.ToolResult{Success: false, Output: errMsg, Data: map[string]any{"error": "empty_arguments"}},
							StartedAt:   now,
							CompletedAt: now,
						},
					}
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
		return toolPreparation{Call: call, PreHookErr: preErr}
	}
	if params != nil {
		call.Arguments = params
	}
	return toolPreparation{Call: call}
}

func (t *runtimeToolExecutor) invokeToolCall(ctx context.Context, call model.ToolCall) (*tool.CallResult, error, string) {
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
	return result, err, content
}

// finalizeToolCall runs PostToolUse when applicable and appends the tool message to history.
func (t *runtimeToolExecutor) finalizeToolCall(ctx context.Context, call model.ToolCall, result *tool.CallResult, execErr error, content string, prep toolPreparation) error {
	switch {
	case prep.Denied != nil:
		return prep.Denied
	case prep.EmptyArgsResult != nil:
		t.appendToolMessage(call, prep.EmptyArgsResult.Result.Output)
		return nil
	case prep.PreHookErr != nil:
		errContent := fmt.Sprintf(`{"error":%q}`, prep.PreHookErr.Error())
		t.appendToolMessage(call, errContent)
		return prep.PreHookErr
	}
	if t.hooks != nil {
		if hookErr := t.hooks.PostToolUse(ctx, coreToolResultPayload(call, result, execErr)); hookErr != nil && execErr == nil {
			t.appendToolMessage(call, t.maybeCompressToolOutput(call, result, content))
			return hookErr
		}
	}
	t.appendToolMessage(call, t.maybeCompressToolOutput(call, result, content))
	return nil
}

func (t *runtimeToolExecutor) Execute(ctx context.Context, call model.ToolCall) (*tool.CallResult, error) {
	prep := t.prepareToolCall(ctx, call)
	call = prep.Call
	switch {
	case prep.Denied != nil:
		return nil, prep.Denied
	case prep.EmptyArgsResult != nil:
		t.appendToolMessage(call, prep.EmptyArgsResult.Result.Output)
		return prep.EmptyArgsResult, nil
	case prep.PreHookErr != nil:
		errContent := fmt.Sprintf(`{"error":%q}`, prep.PreHookErr.Error())
		t.appendToolMessage(call, errContent)
		now := time.Now()
		return &tool.CallResult{
			Call:        tool.Call{Name: call.Name, Params: map[string]any{}, SessionID: t.sessionID},
			Result:      &tool.ToolResult{Success: false, Output: errContent, Data: map[string]any{"error": prep.PreHookErr.Error()}},
			Err:         prep.PreHookErr,
			StartedAt:   now,
			CompletedAt: now,
		}, prep.PreHookErr
	}
	result, err, content := t.invokeToolCall(ctx, call)
	// After a skill tool call, propagate allowed-tools to the Skylark whitelist
	// so that subsequent retrieve_capabilities unlock can expose MCP tools.
	if call.Name == "skill" && t.skylark != nil && result != nil && result.Result != nil {
		if data, _ := result.Result.Data.(map[string]any); data != nil {
			if meta, _ := data["metadata"].(map[string]any); meta != nil {
				if raw, ok := meta["allowed-tools"]; ok {
					var names []string
					switch v := raw.(type) {
					case []string:
						names = v
					case string:
						names = strings.Split(v, ",")
					case []any:
						for _, item := range v {
							if s, _ := item.(string); s != "" {
								names = append(names, s)
							}
						}
					}
					if len(names) > 0 {
						t.skylark.updateWhitelist(names)
					}
				}
			}
		}
	}
	if finErr := t.finalizeToolCall(ctx, call, result, err, content, toolPreparation{}); finErr != nil {
		return result, finErr
	}
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

package acp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/runtime/tasks"
	"github.com/cexll/agentsdk-go/pkg/tool"
	acpproto "github.com/coder/acp-go-sdk"
)

type promptStreamMapper struct {
	nextToolCallID int
	toolByID       map[string]*promptToolCall
	toolByIndex    map[int]*promptToolCall
}

type promptToolCall struct {
	id           acpproto.ToolCallId
	name         string
	announced    bool
	inputBuilder strings.Builder
	inputEmitted bool
}

func newPromptStreamMapper() *promptStreamMapper {
	return &promptStreamMapper{
		toolByID:    make(map[string]*promptToolCall),
		toolByIndex: make(map[int]*promptToolCall),
	}
}

func (m *promptStreamMapper) updatesForEvent(evt api.StreamEvent) []acpproto.SessionUpdate {
	updates := make([]acpproto.SessionUpdate, 0, 2)

	if delta := extractTextDelta(evt); delta != "" {
		updates = append(updates, acpproto.UpdateAgentMessageText(delta))
	}

	switch evt.Type {
	case api.EventContentBlockStart:
		updates = append(updates, m.handleContentBlockStart(evt)...)
	case api.EventContentBlockDelta:
		updates = append(updates, m.handleContentBlockDelta(evt)...)
	case api.EventContentBlockStop:
		updates = append(updates, m.handleContentBlockStop(evt)...)
	case api.EventToolExecutionStart:
		updates = append(updates, m.handleToolExecutionStart(evt)...)
	case api.EventToolExecutionOutput:
		updates = append(updates, m.handleToolExecutionOutput(evt)...)
	case api.EventToolExecutionResult:
		updates = append(updates, m.handleToolExecutionResult(evt)...)
	}

	return updates
}

func (m *promptStreamMapper) handleContentBlockStart(evt api.StreamEvent) []acpproto.SessionUpdate {
	if evt.ContentBlock == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(evt.ContentBlock.Type), "tool_use") {
		return nil
	}

	state := m.ensureToolCall(evt.ContentBlock.ID, evt.ContentBlock.Name, indexPointerFromEvent(evt))
	return m.startToolCallUpdate(state)
}

func (m *promptStreamMapper) handleContentBlockDelta(evt api.StreamEvent) []acpproto.SessionUpdate {
	if evt.Delta == nil || evt.Delta.Type != "input_json_delta" {
		return nil
	}
	index, ok := indexFromEvent(evt)
	if !ok {
		return nil
	}
	state, ok := m.toolByIndex[index]
	if !ok || state == nil {
		return nil
	}

	chunk := decodePartialJSONChunk(evt.Delta.PartialJSON)
	if chunk == "" {
		return nil
	}
	state.inputBuilder.WriteString(chunk)
	return nil
}

func (m *promptStreamMapper) handleContentBlockStop(evt api.StreamEvent) []acpproto.SessionUpdate {
	index, ok := indexFromEvent(evt)
	if !ok {
		return nil
	}
	state, ok := m.toolByIndex[index]
	if !ok || state == nil || state.inputEmitted {
		return nil
	}

	raw := strings.TrimSpace(state.inputBuilder.String())
	if raw == "" {
		return nil
	}

	parsed := parseToolRawPayload(raw)
	opts := []acpproto.ToolCallUpdateOpt{acpproto.WithUpdateRawInput(parsed)}

	state.inputEmitted = true
	return []acpproto.SessionUpdate{acpproto.UpdateToolCall(state.id, opts...)}
}

func (m *promptStreamMapper) handleToolExecutionStart(evt api.StreamEvent) []acpproto.SessionUpdate {
	state := m.ensureToolCall(evt.ToolUseID, evt.Name, nil)
	updates := m.startToolCallUpdate(state)
	updates = append(updates, acpproto.UpdateToolCall(
		state.id,
		acpproto.WithUpdateStatus(acpproto.ToolCallStatusInProgress),
	))
	return updates
}

func (m *promptStreamMapper) handleToolExecutionOutput(evt api.StreamEvent) []acpproto.SessionUpdate {
	state := m.ensureToolCall(evt.ToolUseID, evt.Name, nil)
	updates := m.startToolCallUpdate(state)

	text := fmt.Sprint(evt.Output)
	if text == "" {
		updates = append(updates, acpproto.UpdateToolCall(
			state.id,
			acpproto.WithUpdateStatus(acpproto.ToolCallStatusInProgress),
		))
		return updates
	}
	if evt.IsStderr != nil && *evt.IsStderr {
		text = "[stderr] " + text
	}

	updates = append(updates, acpproto.UpdateToolCall(
		state.id,
		acpproto.WithUpdateStatus(acpproto.ToolCallStatusInProgress),
		acpproto.WithUpdateContent([]acpproto.ToolCallContent{
			acpproto.ToolContent(acpproto.TextBlock(text)),
		}),
	))
	return updates
}

func (m *promptStreamMapper) handleToolExecutionResult(evt api.StreamEvent) []acpproto.SessionUpdate {
	state := m.ensureToolCall(evt.ToolUseID, evt.Name, nil)
	updates := m.startToolCallUpdate(state)

	payload := normalizeToolExecutionPayload(evt.Output)

	status := acpproto.ToolCallStatusCompleted
	if toolExecutionFailed(evt, payload) {
		status = acpproto.ToolCallStatusFailed
	}

	opts := []acpproto.ToolCallUpdateOpt{
		acpproto.WithUpdateStatus(status),
	}
	if payload.rawOutput != nil {
		opts = append(opts, acpproto.WithUpdateRawOutput(payload.rawOutput))
	}
	if len(payload.content) > 0 {
		opts = append(opts, acpproto.WithUpdateContent(payload.content))
	}
	if len(payload.locations) > 0 {
		opts = append(opts, acpproto.WithUpdateLocations(payload.locations))
	}

	updates = append(updates, acpproto.UpdateToolCall(state.id, opts...))
	if status == acpproto.ToolCallStatusCompleted {
		if entries := planEntriesFromTaskTool(state.name, payload.data); len(entries) > 0 {
			updates = append(updates, acpproto.UpdatePlan(entries...))
		}
	}
	return updates
}

func (m *promptStreamMapper) ensureToolCall(rawID, name string, index *int) *promptToolCall {
	if m == nil {
		return &promptToolCall{
			id:   acpproto.ToolCallId("tool_call"),
			name: "tool",
		}
	}

	trimmedID := strings.TrimSpace(rawID)
	lookupID := trimmedID
	if lookupID == "" && index != nil {
		if existing := m.toolByIndex[*index]; existing != nil {
			lookupID = string(existing.id)
		}
	}

	if lookupID != "" {
		if existing := m.toolByID[lookupID]; existing != nil {
			if strings.TrimSpace(name) != "" {
				existing.name = strings.TrimSpace(name)
			}
			if index != nil {
				m.toolByIndex[*index] = existing
			}
			return existing
		}
	}

	id := trimmedID
	if id == "" {
		m.nextToolCallID++
		id = fmt.Sprintf("tool_call_%d", m.nextToolCallID)
	}
	title := strings.TrimSpace(name)
	if title == "" {
		title = "tool"
	}

	state := &promptToolCall{
		id:   acpproto.ToolCallId(id),
		name: title,
	}

	m.toolByID[id] = state
	if index != nil {
		m.toolByIndex[*index] = state
	}
	return state
}

func (m *promptStreamMapper) startToolCallUpdate(state *promptToolCall) []acpproto.SessionUpdate {
	if state == nil || state.announced {
		return nil
	}
	state.announced = true
	return []acpproto.SessionUpdate{
		acpproto.StartToolCall(
			state.id,
			state.name,
			acpproto.WithStartStatus(acpproto.ToolCallStatusPending),
		),
	}
}

func indexFromEvent(evt api.StreamEvent) (int, bool) {
	if evt.Index == nil {
		return 0, false
	}
	return *evt.Index, true
}

func indexPointerFromEvent(evt api.StreamEvent) *int {
	index, ok := indexFromEvent(evt)
	if !ok {
		return nil
	}
	return &index
}

func decodePartialJSONChunk(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var chunk string
	if err := json.Unmarshal(raw, &chunk); err == nil {
		return chunk
	}
	return string(raw)
}

func parseToolRawPayload(raw string) any {
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}
	return payload
}

type normalizedToolExecution struct {
	rawOutput any
	metadata  map[string]any
	data      any
	content   []acpproto.ToolCallContent
	locations []acpproto.ToolCallLocation
}

func normalizeToolExecutionPayload(raw any) normalizedToolExecution {
	payload := normalizedToolExecution{
		rawOutput: raw,
		metadata:  map[string]any{},
	}

	if mapped, ok := raw.(map[string]any); ok {
		if output, exists := mapped["output"]; exists {
			payload.rawOutput = output
		}
		if metadata, ok := mapped["metadata"].(map[string]any); ok {
			payload.metadata = metadata
		}
	}
	payload.data = payload.metadata["data"]

	if text := toolPayloadText(payload.rawOutput); text != "" {
		payload.content = append(payload.content, acpproto.ToolContent(acpproto.TextBlock(text)))
	}

	if terminalID := terminalIDFromToolData(payload.data); terminalID != "" {
		payload.content = append(payload.content, acpproto.ToolTerminalRef(terminalID))
	}
	payload.locations = outputRefLocations(payload.metadata["output_ref"])
	return payload
}

func toolExecutionFailed(evt api.StreamEvent, payload normalizedToolExecution) bool {
	if evt.IsError != nil && *evt.IsError {
		return true
	}
	return strings.TrimSpace(metadataError(payload.metadata)) != ""
}

func metadataError(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(meta["error"]))
	if text == "<nil>" {
		return ""
	}
	return text
}

func toolPayloadText(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(encoded)
	}
}

func terminalIDFromToolData(data any) string {
	switch value := data.(type) {
	case map[string]any:
		return strings.TrimSpace(fmt.Sprint(value["terminal_id"]))
	default:
		return ""
	}
}

func outputRefLocations(raw any) []acpproto.ToolCallLocation {
	ref := outputRefFromAny(raw)
	if ref == nil {
		return nil
	}
	path := strings.TrimSpace(ref.Path)
	if path == "" {
		return nil
	}
	return []acpproto.ToolCallLocation{{Path: path}}
}

func outputRefFromAny(raw any) *tool.OutputRef {
	switch value := raw.(type) {
	case *tool.OutputRef:
		if value == nil {
			return nil
		}
		cp := *value
		return &cp
	case tool.OutputRef:
		cp := value
		return &cp
	case map[string]any:
		path := strings.TrimSpace(fmt.Sprint(value["path"]))
		if path == "" {
			return nil
		}
		return &tool.OutputRef{Path: path}
	default:
		return nil
	}
}

func planEntriesFromTaskTool(toolName string, rawData any) []acpproto.PlanEntry {
	kind := canonicalTaskToolName(toolName)
	if kind == "" {
		return nil
	}

	dataMap, dataMapOK := rawData.(map[string]any)
	switch kind {
	case "taskcreate":
		if !dataMapOK || dataMap == nil {
			return nil
		}
		taskID := strings.TrimSpace(fmt.Sprint(dataMap["taskId"]))
		if taskID == "" {
			return nil
		}
		return []acpproto.PlanEntry{
			{
				Content:  fmt.Sprintf("Task %s", taskID),
				Priority: acpproto.PlanEntryPriorityMedium,
				Status:   acpproto.PlanEntryStatusPending,
			},
		}
	case "tasklist":
		if !dataMapOK || dataMap == nil {
			return nil
		}
		return planEntriesFromTasks(dataMap["tasks"])
	case "taskget", "taskupdate":
		if !dataMapOK || dataMap == nil {
			return nil
		}
		taskAny, ok := dataMap["task"]
		if !ok || taskAny == nil {
			return nil
		}
		return planEntriesFromTasks([]any{taskAny})
	default:
		return nil
	}
}

func canonicalTaskToolName(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return ""
	}
	key = strings.NewReplacer("_", "", "-", "", " ", "").Replace(key)
	switch key {
	case "taskcreate", "tasklist", "taskget", "taskupdate":
		return key
	default:
		return ""
	}
}

func planEntriesFromTasks(raw any) []acpproto.PlanEntry {
	tasksList := tasksFromAny(raw)
	if len(tasksList) == 0 {
		return nil
	}
	out := make([]acpproto.PlanEntry, 0, len(tasksList))
	for _, taskItem := range tasksList {
		entry, ok := planEntryFromTask(taskItem)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tasksFromAny(raw any) []tasks.Task {
	switch value := raw.(type) {
	case nil:
		return nil
	case tasks.Task:
		return []tasks.Task{value}
	case *tasks.Task:
		if value == nil {
			return nil
		}
		return []tasks.Task{*value}
	case []tasks.Task:
		if len(value) == 0 {
			return nil
		}
		return append([]tasks.Task(nil), value...)
	case []*tasks.Task:
		out := make([]tasks.Task, 0, len(value))
		for _, item := range value {
			if item == nil {
				continue
			}
			out = append(out, *item)
		}
		return out
	case []any:
		out := make([]tasks.Task, 0, len(value))
		for _, item := range value {
			taskItem, ok := taskFromAny(item)
			if !ok {
				continue
			}
			out = append(out, taskItem)
		}
		return out
	default:
		taskItem, ok := taskFromAny(value)
		if !ok {
			return nil
		}
		return []tasks.Task{taskItem}
	}
}

func taskFromAny(raw any) (tasks.Task, bool) {
	switch value := raw.(type) {
	case tasks.Task:
		return value, true
	case *tasks.Task:
		if value == nil {
			return tasks.Task{}, false
		}
		return *value, true
	case map[string]any:
		id := strings.TrimSpace(fmt.Sprint(value["id"]))
		subject := strings.TrimSpace(fmt.Sprint(value["subject"]))
		status := strings.TrimSpace(fmt.Sprint(value["status"]))
		return tasks.Task{
			ID:      id,
			Subject: subject,
			Status:  tasks.TaskStatus(status),
		}, id != "" || subject != ""
	default:
		return tasks.Task{}, false
	}
}

func planEntryFromTask(task tasks.Task) (acpproto.PlanEntry, bool) {
	content := strings.TrimSpace(task.Subject)
	if content == "" {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			return acpproto.PlanEntry{}, false
		}
		content = fmt.Sprintf("Task %s", taskID)
	}
	return acpproto.PlanEntry{
		Content:  content,
		Priority: planPriorityFromTaskStatus(task.Status),
		Status:   planStatusFromTaskStatus(task.Status),
	}, true
}

func planStatusFromTaskStatus(status tasks.TaskStatus) acpproto.PlanEntryStatus {
	switch strings.ToLower(strings.TrimSpace(string(status))) {
	case string(tasks.TaskCompleted):
		return acpproto.PlanEntryStatusCompleted
	case string(tasks.TaskInProgress):
		return acpproto.PlanEntryStatusInProgress
	default:
		return acpproto.PlanEntryStatusPending
	}
}

func planPriorityFromTaskStatus(status tasks.TaskStatus) acpproto.PlanEntryPriority {
	switch strings.ToLower(strings.TrimSpace(string(status))) {
	case string(tasks.TaskInProgress):
		return acpproto.PlanEntryPriorityHigh
	case string(tasks.TaskCompleted):
		return acpproto.PlanEntryPriorityLow
	default:
		return acpproto.PlanEntryPriorityMedium
	}
}

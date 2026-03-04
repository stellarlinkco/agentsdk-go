package acp

import (
	"encoding/json"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/tool"
	acpproto "github.com/coder/acp-go-sdk"
)

func TestPromptStreamMapperEmitsToolLifecycleUpdates(t *testing.T) {
	t.Parallel()

	mapper := newPromptStreamMapper()
	index := 0

	events := []api.StreamEvent{
		{
			Type:  api.EventContentBlockStart,
			Index: &index,
			ContentBlock: &api.ContentBlock{
				Type: "tool_use",
				ID:   "call_1",
				Name: "Read",
			},
		},
		{
			Type:  api.EventContentBlockDelta,
			Index: &index,
			Delta: &api.Delta{
				Type:        "input_json_delta",
				PartialJSON: mustRawChunk(t, `{"file_path":"C:\\repo\\a.txt"}`),
			},
		},
		{
			Type:  api.EventContentBlockStop,
			Index: &index,
		},
		{
			Type:      api.EventToolExecutionStart,
			ToolUseID: "call_1",
			Name:      "Read",
		},
		{
			Type:      api.EventToolExecutionOutput,
			ToolUseID: "call_1",
			Name:      "Read",
			Output:    "reading...",
		},
		{
			Type:      api.EventToolExecutionResult,
			ToolUseID: "call_1",
			Name:      "Read",
			Output: map[string]any{
				"output": "done",
				"metadata": map[string]any{
					"data": map[string]any{
						"path": "C:\\repo\\a.txt",
					},
				},
			},
		},
	}

	var updates []acpproto.SessionUpdate
	for _, evt := range events {
		updates = append(updates, mapper.updatesForEvent(evt)...)
	}

	var sawStart bool
	var sawRawInput bool
	var sawInProgress bool
	var sawStreamOutput bool
	var sawCompleted bool
	for _, update := range updates {
		if tc := update.ToolCall; tc != nil {
			sawStart = true
			if tc.ToolCallId != "call_1" {
				t.Fatalf("tool_call id=%q, want %q", tc.ToolCallId, "call_1")
			}
			continue
		}
		if tu := update.ToolCallUpdate; tu != nil {
			if tu.RawInput != nil {
				sawRawInput = true
			}
			if tu.Status != nil && *tu.Status == acpproto.ToolCallStatusInProgress {
				sawInProgress = true
			}
			if len(tu.Content) > 0 && tu.Content[0].Content != nil && tu.Content[0].Content.Content.Text != nil {
				if tu.Content[0].Content.Content.Text.Text == "reading..." {
					sawStreamOutput = true
				}
			}
			if tu.Status != nil && *tu.Status == acpproto.ToolCallStatusCompleted {
				sawCompleted = true
			}
		}
	}

	if !sawStart {
		t.Fatalf("expected tool_call start update, got %+v", updates)
	}
	if !sawRawInput {
		t.Fatalf("expected tool_call_update rawInput, got %+v", updates)
	}
	if !sawInProgress {
		t.Fatalf("expected tool_call_update in_progress, got %+v", updates)
	}
	if !sawStreamOutput {
		t.Fatalf("expected tool_call_update with streamed output, got %+v", updates)
	}
	if !sawCompleted {
		t.Fatalf("expected tool_call_update completed, got %+v", updates)
	}
}

func TestPromptStreamMapperEmitsTerminalAndOutputRefContent(t *testing.T) {
	t.Parallel()

	mapper := newPromptStreamMapper()

	updates := mapper.updatesForEvent(api.StreamEvent{
		Type:      api.EventToolExecutionResult,
		ToolUseID: "call_term",
		Name:      "Bash",
		Output: map[string]any{
			"output": "command failed",
			"metadata": map[string]any{
				"error": "exit 1",
				"data": map[string]any{
					"terminal_id": "term_1",
				},
				"output_ref": &tool.OutputRef{Path: "C:\\repo\\output.txt"},
			},
		},
	})

	if len(updates) < 2 {
		t.Fatalf("updates len=%d, want >=2", len(updates))
	}
	if updates[0].ToolCall == nil {
		t.Fatalf("expected first update to start tool call, got %+v", updates[0])
	}

	final := updates[len(updates)-1].ToolCallUpdate
	if final == nil {
		t.Fatalf("expected final tool_call_update, got %+v", updates[len(updates)-1])
	}
	if final.Status == nil || *final.Status != acpproto.ToolCallStatusFailed {
		t.Fatalf("tool status=%v, want failed", final.Status)
	}

	var sawTerminal bool
	var sawLocation bool
	for _, content := range final.Content {
		if content.Terminal != nil && content.Terminal.TerminalId == "term_1" {
			sawTerminal = true
		}
	}
	for _, location := range final.Locations {
		if location.Path == "C:\\repo\\output.txt" {
			sawLocation = true
		}
	}
	if !sawTerminal {
		t.Fatalf("expected terminal tool content, got %+v", final.Content)
	}
	if !sawLocation {
		t.Fatalf("expected output_ref location, got %+v", final.Locations)
	}
}

func TestPromptStreamMapperEmitsPlanUpdatesForTaskTools(t *testing.T) {
	t.Parallel()

	mapper := newPromptStreamMapper()
	updates := mapper.updatesForEvent(api.StreamEvent{
		Type:      api.EventToolExecutionResult,
		ToolUseID: "call_task_list",
		Name:      "TaskList",
		Output: map[string]any{
			"output": "listed",
			"metadata": map[string]any{
				"data": map[string]any{
					"tasks": []any{
						map[string]any{
							"id":      "task_1",
							"subject": "Design API",
							"status":  "in_progress",
						},
						map[string]any{
							"id":      "task_2",
							"subject": "Write tests",
							"status":  "completed",
						},
					},
				},
			},
		},
	})

	var plan *acpproto.SessionUpdatePlan
	for _, update := range updates {
		if update.Plan != nil {
			plan = update.Plan
			break
		}
	}
	if plan == nil {
		t.Fatalf("expected plan update from task list tool, got %+v", updates)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("plan entries len=%d, want 2", len(plan.Entries))
	}
	if plan.Entries[0].Content != "Design API" ||
		plan.Entries[0].Status != acpproto.PlanEntryStatusInProgress ||
		plan.Entries[0].Priority != acpproto.PlanEntryPriorityHigh {
		t.Fatalf("unexpected first plan entry: %+v", plan.Entries[0])
	}
	if plan.Entries[1].Content != "Write tests" ||
		plan.Entries[1].Status != acpproto.PlanEntryStatusCompleted ||
		plan.Entries[1].Priority != acpproto.PlanEntryPriorityLow {
		t.Fatalf("unexpected second plan entry: %+v", plan.Entries[1])
	}
}

func TestPromptStreamMapperEmitsPlanUpdateForTaskCreate(t *testing.T) {
	t.Parallel()

	mapper := newPromptStreamMapper()
	updates := mapper.updatesForEvent(api.StreamEvent{
		Type:      api.EventToolExecutionResult,
		ToolUseID: "call_task_create",
		Name:      "task_create",
		Output: map[string]any{
			"output": "{\"taskId\":\"abc123\"}",
			"metadata": map[string]any{
				"data": map[string]any{
					"taskId": "abc123",
				},
			},
		},
	})

	var plan *acpproto.SessionUpdatePlan
	for _, update := range updates {
		if update.Plan != nil {
			plan = update.Plan
			break
		}
	}
	if plan == nil {
		t.Fatalf("expected plan update from task_create, got %+v", updates)
	}
	if len(plan.Entries) != 1 {
		t.Fatalf("plan entries len=%d, want 1", len(plan.Entries))
	}
	entry := plan.Entries[0]
	if entry.Status != acpproto.PlanEntryStatusPending || entry.Priority != acpproto.PlanEntryPriorityMedium {
		t.Fatalf("unexpected plan entry defaults: %+v", entry)
	}
	if entry.Content != "Task abc123" {
		t.Fatalf("plan entry content=%q, want %q", entry.Content, "Task abc123")
	}
}

func TestPromptStreamMapperDoesNotEmitPlanUpdateOnTaskFailure(t *testing.T) {
	t.Parallel()

	mapper := newPromptStreamMapper()
	updates := mapper.updatesForEvent(api.StreamEvent{
		Type:      api.EventToolExecutionResult,
		ToolUseID: "call_task_update",
		Name:      "TaskUpdate",
		Output: map[string]any{
			"output": "failed",
			"metadata": map[string]any{
				"error": "boom",
				"data": map[string]any{
					"task": map[string]any{"id": "task_1", "subject": "x", "status": "pending"},
				},
			},
		},
	})

	for _, update := range updates {
		if update.Plan != nil {
			t.Fatalf("did not expect plan update for failed task tool execution, got %+v", update.Plan)
		}
	}
}

func mustRawChunk(t *testing.T, chunk string) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	return json.RawMessage(encoded)
}

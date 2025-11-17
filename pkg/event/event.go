package event

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// EventType 表示事件类型，按业务语义划分。
type EventType string

const (
	// Progress channel
	EventProgress   EventType = "progress"
	EventThinking   EventType = "thinking"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventCompletion EventType = "completion"

	// Control channel
	EventApprovalReq       EventType = "approval_req"
	EventApprovalResp      EventType = "approval_resp"
	EventApprovalRequested EventType = "approval_requested"
	EventApprovalDecided   EventType = "approval_decided"
	EventInterrupt         EventType = "interrupt"
	EventResume            EventType = "resume"

	// Monitor channel
	EventMetrics EventType = "metrics"
	EventAudit   EventType = "audit"
	EventError   EventType = "error"
)

// Channel 描述 Progress/Control/Monitor 三条物理通道。
type Channel string

const (
	ChannelProgress Channel = "progress"
	ChannelControl  Channel = "control"
	ChannelMonitor  Channel = "monitor"
)

var typeToChannel = map[EventType]Channel{
	EventProgress:          ChannelProgress,
	EventThinking:          ChannelProgress,
	EventToolCall:          ChannelProgress,
	EventToolResult:        ChannelProgress,
	EventCompletion:        ChannelProgress,
	EventApprovalReq:       ChannelControl,
	EventApprovalResp:      ChannelControl,
	EventApprovalRequested: ChannelControl,
	EventApprovalDecided:   ChannelControl,
	EventInterrupt:         ChannelControl,
	EventResume:            ChannelControl,
	EventMetrics:           ChannelMonitor,
	EventAudit:             ChannelMonitor,
	EventError:             ChannelMonitor,
}

// Event 描述一次事件推送。
type Event struct {
	ID        string    `json:"id"`
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id,omitempty"`
	Data      any       `json:"data,omitempty"`
	Bookmark  *Bookmark `json:"bookmark,omitempty"`
}

// NewEvent 构造函数，自动填充 ID/Timestamp。
func NewEvent(typ EventType, sessionID string, data any) Event {
	evt := Event{Type: typ, SessionID: sessionID, Data: data}
	return normalizeEvent(evt)
}

// Validate 检查事件是否符合约束。
func (e Event) Validate() error {
	if e.Type == "" {
		return errors.New("event: type is empty")
	}
	if _, ok := typeToChannel[e.Type]; !ok {
		return fmt.Errorf("event: unknown type %q", e.Type)
	}
	return nil
}

// Channel 返回事件所属的物理通道。
func (t EventType) Channel() (Channel, bool) {
	ch, ok := typeToChannel[t]
	return ch, ok
}

func normalizeEvent(evt Event) Event {
	if evt.ID == "" {
		evt.ID = newEventID()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	if evt.Bookmark != nil {
		evt.Bookmark = evt.Bookmark.Clone()
	}
	return evt
}

func newEventID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// ProgressData 描述长期运行任务的阶段信息。
type ProgressData struct {
	Stage   string         `json:"stage"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// ToolCallData 描述一次工具调用。
type ToolCallData struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
}

// ToolResultData 描述工具调用结果。
type ToolResultData struct {
	Name     string        `json:"name"`
	Output   any           `json:"output,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
}

// CompletionData 汇总一次 Agent 回合最终结果。
type CompletionData struct {
	Output     string         `json:"output"`
	StopReason string         `json:"stop_reason"`
	ToolCalls  []ToolCallData `json:"tool_calls,omitempty"`
	Usage      *UsageData     `json:"usage,omitempty"`
}

// UsageData 为流式消费提供轻量的 token 统计。
type UsageData struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	CacheTokens  int `json:"cache_tokens"`
}

// ErrorData 对监控/审计友好的错误表示。
type ErrorData struct {
	Message     string `json:"message"`
	Kind        string `json:"kind,omitempty"`
	Recoverable bool   `json:"recoverable,omitempty"`
}

// ApprovalRequest 描述一次审批请求。
type ApprovalRequest struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Params   map[string]any `json:"params"`
	Reason   string         `json:"reason,omitempty"`
}

// ApprovalResponse 描述审批结果。
type ApprovalResponse struct {
	ID       string `json:"id"`
	Approved bool   `json:"approved"`
	Comment  string `json:"comment,omitempty"`
}

// channelForType 返回通道（供内部使用）。
func channelForType(t EventType) (Channel, bool) {
	return t.Channel()
}

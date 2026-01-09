package toolbuiltin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const bashOutputDescription = `
- Retrieves output from a running or completed background bash shell
- Takes a bash_id for legacy shells or task_id for async Bash tasks
- Always returns only new output since the last check
- Returns stdout and stderr output along with shell status
- Supports optional regex filtering to show only lines matching a pattern
- Use this tool when you need to monitor or check the output of a long-running shell
- Shell IDs can be found using the /bashes command
`

var bashOutputSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"bash_id": map[string]interface{}{
			"type":        "string",
			"description": "The ID of the background shell to retrieve output from",
		},
		"task_id": map[string]interface{}{
			"type":        "string",
			"description": "Async task ID returned from Bash async mode",
		},
		"filter": map[string]interface{}{
			"type":        "string",
			"description": "Optional regular expression to filter the output lines. Only lines matching this regex will be included in the result. Any lines that do not match will no longer be available to read.",
		},
	},
}

var defaultShellStore = newShellStore()

// BashOutputTool exposes incremental output retrieval for background shells.
type BashOutputTool struct {
	store *ShellStore
}

// NewBashOutputTool creates a tool backed by the provided shell store.
func NewBashOutputTool(store *ShellStore) *BashOutputTool {
	if store == nil {
		store = defaultShellStore
	}
	return &BashOutputTool{store: store}
}

func (b *BashOutputTool) Name() string { return "BashOutput" }

func (b *BashOutputTool) Description() string { return bashOutputDescription }

func (b *BashOutputTool) Schema() *tool.JSONSchema { return bashOutputSchema }

func (b *BashOutputTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if b == nil || b.store == nil {
		return nil, errors.New("bash output tool is not initialised")
	}
	id, isAsync, err := parseOutputID(params)
	if err != nil {
		return nil, err
	}
	filter, err := parseFilter(params)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if isAsync {
		chunk, done, taskErr := DefaultAsyncTaskManager().GetOutput(id)
		outputFile := DefaultAsyncTaskManager().OutputFile(id)
		status := "running"
		if done {
			if taskErr != nil {
				status = "failed"
			} else {
				status = "completed"
			}
		}
		display := chunk
		if strings.TrimSpace(display) == "" && outputFile != "" {
			display = formatBashOutputReference(outputFile)
		}
		output := renderAsyncRead(id, status, display, taskErr)
		data := map[string]interface{}{
			"task_id": id,
			"status":  status,
			"output":  chunk,
		}
		if outputFile != "" {
			data["output_file"] = outputFile
		}
		if taskErr != nil {
			data["error"] = taskErr.Error()
		}
		return &tool.ToolResult{Success: true, Output: output, Data: data}, nil
	}

	read, err := b.store.Consume(id, filter)
	if err != nil {
		return nil, err
	}
	output := renderShellRead(read)
	data := map[string]interface{}{
		"shell_id":      read.ShellID,
		"status":        string(read.Status),
		"lines":         read.Lines,
		"stdout":        collectStream(read.Lines, ShellStreamStdout),
		"stderr":        collectStream(read.Lines, ShellStreamStderr),
		"dropped_lines": read.Dropped,
		"exit_code":     read.ExitCode,
		"error":         read.Error,
		"updated_at":    read.UpdatedAt,
	}

	return &tool.ToolResult{
		Success: true,
		Output:  output,
		Data:    data,
	}, nil
}

func parseOutputID(params map[string]interface{}) (string, bool, error) {
	if params == nil {
		return "", false, errors.New("params is nil")
	}
	if raw, ok := params["task_id"]; ok && raw != nil {
		id, err := coerceString(raw)
		if err != nil {
			return "", false, fmt.Errorf("task_id must be string: %w", err)
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return "", false, errors.New("task_id cannot be empty")
		}
		return id, true, nil
	}

	raw, ok := params["bash_id"]
	if !ok || raw == nil {
		return "", false, errors.New("bash_id or task_id is required")
	}
	id, err := coerceString(raw)
	if err != nil {
		return "", false, fmt.Errorf("bash_id must be string: %w", err)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false, errors.New("bash_id cannot be empty")
	}
	if _, exists := DefaultAsyncTaskManager().lookup(id); exists {
		return id, true, nil
	}
	return id, false, nil
}

func parseFilter(params map[string]interface{}) (*regexp.Regexp, error) {
	if params == nil {
		return nil, nil
	}
	raw, ok := params["filter"]
	if !ok || raw == nil {
		return nil, nil
	}
	value, err := coerceString(raw)
	if err != nil {
		return nil, fmt.Errorf("filter must be string: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	re, err := regexp.Compile(value)
	if err != nil {
		return nil, fmt.Errorf("invalid filter regex: %w", err)
	}
	return re, nil
}

func renderShellRead(read ShellRead) string {
	if len(read.Lines) == 0 {
		msg := fmt.Sprintf("shell %s status=%s (no new output)", read.ShellID, read.Status)
		if read.Error != "" {
			msg += ": " + read.Error
		}
		return msg
	}
	var b strings.Builder
	fmt.Fprintf(&b, "shell %s status=%s\n", read.ShellID, read.Status)
	for _, line := range read.Lines {
		fmt.Fprintf(&b, "[%s] %s\n", line.Stream, line.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

func collectStream(lines []ShellLine, stream ShellStream) string {
	var parts []string
	for _, line := range lines {
		if line.Stream == stream {
			parts = append(parts, line.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func renderAsyncRead(id, status, chunk string, taskErr error) string {
	if strings.TrimSpace(chunk) == "" {
		msg := fmt.Sprintf("task %s status=%s (no new output)", id, status)
		if taskErr != nil {
			msg += ": " + taskErr.Error()
		}
		return msg
	}
	return strings.TrimRight(fmt.Sprintf("task %s status=%s\n%s", id, status, chunk), "\n")
}

// ShellStatus represents the lifecycle status of a shell.
type ShellStatus string

const (
	// ShellStatusRunning indicates the shell is still producing output.
	ShellStatusRunning ShellStatus = "running"
	// ShellStatusCompleted indicates the shell exited without error.
	ShellStatusCompleted ShellStatus = "completed"
	// ShellStatusFailed indicates the shell exited with an error.
	ShellStatusFailed ShellStatus = "failed"
)

// ShellStream distinguishes stdout/stderr streams.
type ShellStream string

const (
	// ShellStreamStdout captures stdout data.
	ShellStreamStdout ShellStream = "stdout"
	// ShellStreamStderr captures stderr data.
	ShellStreamStderr ShellStream = "stderr"
)

// ShellLine represents a single line of shell output.
type ShellLine struct {
	Stream    ShellStream `json:"stream"`
	Content   string      `json:"content"`
	Sequence  int         `json:"sequence"`
	Timestamp time.Time   `json:"timestamp"`
}

// ShellRead reports the outcome of consuming buffered shell output.
type ShellRead struct {
	ShellID   string      `json:"shell_id"`
	Status    ShellStatus `json:"status"`
	Lines     []ShellLine `json:"lines"`
	Dropped   int         `json:"dropped"`
	ExitCode  int         `json:"exit_code"`
	Error     string      `json:"error,omitempty"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// ShellStore tracks background shell buffers safely.
type ShellStore struct {
	mu     sync.RWMutex
	shells map[string]*shellState
}

type shellState struct {
	id       string
	lines    []ShellLine
	status   ShellStatus
	exitCode int
	err      error
	seq      int
	updated  time.Time
}

// ShellHandle allows append/close operations scoped to a shell.
type ShellHandle struct {
	store *ShellStore
	id    string
}

func newShellStore() *ShellStore {
	return &ShellStore{shells: map[string]*shellState{}}
}

// DefaultShellStore exposes the shared global store.
func DefaultShellStore() *ShellStore {
	return defaultShellStore
}

// Register initialises a shell buffer with the given id.
func (s *ShellStore) Register(id string) (*ShellHandle, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return nil, errors.New("shell id cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.shells[trimmed]; exists {
		return nil, fmt.Errorf("shell %s already exists", trimmed)
	}
	state := &shellState{
		id:      trimmed,
		status:  ShellStatusRunning,
		updated: time.Now(),
	}
	s.shells[trimmed] = state
	return &ShellHandle{store: s, id: trimmed}, nil
}

// Append appends output to the shell buffer.
func (s *ShellStore) Append(id string, stream ShellStream, data string) error {
	lines := splitLines(data)
	if len(lines) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.shells[id]
	if !ok {
		state = &shellState{id: id, status: ShellStatusRunning}
		s.shells[id] = state
	}
	now := time.Now()
	for _, line := range lines {
		state.seq++
		state.lines = append(state.lines, ShellLine{
			Stream:    stream,
			Content:   line,
			Sequence:  state.seq,
			Timestamp: now,
		})
	}
	state.updated = now
	if state.status != ShellStatusRunning {
		state.status = ShellStatusRunning
		state.err = nil
		state.exitCode = 0
	}
	return nil
}

// Close marks the shell as completed with the provided exit code.
func (s *ShellStore) Close(id string, exitCode int) error {
	return s.finalize(id, exitCode, nil)
}

// Fail marks the shell as failed with an error.
func (s *ShellStore) Fail(id string, err error) error {
	if err == nil {
		err = errors.New("unknown shell error")
	}
	return s.finalize(id, -1, err)
}

// Consume drains pending lines applying an optional regex filter.
func (s *ShellStore) Consume(id string, filter *regexp.Regexp) (ShellRead, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.shells[id]
	if !ok {
		return ShellRead{}, fmt.Errorf("shell %s not found", id)
	}
	lines := state.lines
	state.lines = nil
	read := ShellRead{
		ShellID:   id,
		Status:    state.status,
		ExitCode:  state.exitCode,
		UpdatedAt: state.updated,
	}
	if state.err != nil {
		read.Error = state.err.Error()
	}
	if len(lines) == 0 {
		return read, nil
	}
	for _, line := range lines {
		if filter != nil && !filter.MatchString(line.Content) {
			read.Dropped++
			continue
		}
		read.Lines = append(read.Lines, line)
	}
	return read, nil
}

func (s *ShellStore) finalize(id string, exitCode int, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.shells[id]
	if !ok {
		return fmt.Errorf("shell %s not found", id)
	}
	state.exitCode = exitCode
	state.updated = time.Now()
	state.err = err
	if err != nil {
		state.status = ShellStatusFailed
	} else {
		state.status = ShellStatusCompleted
	}
	return nil
}

// Append sends stdout/stderr chunks to the shell buffer.
func (h *ShellHandle) Append(stream ShellStream, data string) error {
	if h == nil || h.store == nil {
		return errors.New("shell handle is nil")
	}
	return h.store.Append(h.id, stream, data)
}

// Close marks the shell as completed.
func (h *ShellHandle) Close(exitCode int) error {
	if h == nil || h.store == nil {
		return errors.New("shell handle is nil")
	}
	return h.store.Close(h.id, exitCode)
}

// Fail marks the shell as failed.
func (h *ShellHandle) Fail(err error) error {
	if h == nil || h.store == nil {
		return errors.New("shell handle is nil")
	}
	return h.store.Fail(h.id, err)
}

func splitLines(data string) []string {
	if data == "" {
		return nil
	}
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	parts := strings.Split(normalized, "\n")
	// Preserve trailing empty segments (real shells render blank lines).
	return parts
}

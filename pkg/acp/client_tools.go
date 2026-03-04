package acp

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/tool"
	acpproto "github.com/coder/acp-go-sdk"
)

const acpTerminalCleanupTimeout = 5 * time.Second

func buildClientCapabilityTools(sessionID acpproto.SessionId, connFn func() *acpproto.AgentSideConnection, caps acpproto.ClientCapabilities) ([]tool.Tool, []string) {
	tools := make([]tool.Tool, 0, 4)
	shadowBuiltinKeys := make([]string, 0, 4)

	if caps.Fs.ReadTextFile {
		tools = append(tools, &acpReadTool{sessionID: sessionID, conn: connFn})
		shadowBuiltinKeys = append(shadowBuiltinKeys, "file_read")
	}
	if caps.Fs.WriteTextFile {
		tools = append(tools, &acpWriteTool{sessionID: sessionID, conn: connFn})
		// Edit is mutating too; keep it on the ACP capability path when fs/write is available.
		shadowBuiltinKeys = append(shadowBuiltinKeys, "file_write", "file_edit")
	}
	if caps.Fs.ReadTextFile && caps.Fs.WriteTextFile {
		tools = append(tools, &acpEditTool{sessionID: sessionID, conn: connFn})
	}
	if caps.Terminal {
		tools = append(tools, &acpBashTool{sessionID: sessionID, conn: connFn})
		shadowBuiltinKeys = append(shadowBuiltinKeys, "bash")
	}

	return tools, shadowBuiltinKeys
}

var acpReadSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]any{
		"file_path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the text file to read.",
		},
		"offset": map[string]any{
			"type":        "number",
			"description": "Optional 1-based starting line number.",
		},
		"limit": map[string]any{
			"type":        "number",
			"description": "Optional maximum number of lines to return.",
		},
	},
	Required: []string{"file_path"},
}

type acpReadTool struct {
	sessionID acpproto.SessionId
	conn      func() *acpproto.AgentSideConnection
}

func (t *acpReadTool) Name() string { return "Read" }

func (t *acpReadTool) Description() string {
	return "Read a text file via ACP client capability fs/read_text_file."
}

func (t *acpReadTool) Schema() *tool.JSONSchema { return acpReadSchema }

func (t *acpReadTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	conn := t.currentConnection()
	if conn == nil {
		return nil, errors.New("acp read: connection is not available")
	}
	path, err := stringParam(params, "file_path", true)
	if err != nil {
		return nil, err
	}

	line, err := optionalPositiveIntParam(params, "offset")
	if err != nil {
		return nil, err
	}
	limit, err := optionalPositiveIntParam(params, "limit")
	if err != nil {
		return nil, err
	}

	resp, err := conn.ReadTextFile(ctx, acpproto.ReadTextFileRequest{
		SessionId: t.sessionID,
		Path:      path,
		Line:      line,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("acp read_text_file: %w", err)
	}

	return &tool.ToolResult{
		Success: true,
		Output:  resp.Content,
		Data: map[string]any{
			"path": path,
		},
	}, nil
}

var acpWriteSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]any{
		"file_path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the text file to write.",
		},
		"content": map[string]any{
			"type":        "string",
			"description": "Text content to write.",
		},
	},
	Required: []string{"file_path", "content"},
}

type acpWriteTool struct {
	sessionID acpproto.SessionId
	conn      func() *acpproto.AgentSideConnection
}

func (t *acpWriteTool) Name() string { return "Write" }

func (t *acpWriteTool) Description() string {
	return "Write a text file via ACP client capability fs/write_text_file."
}

func (t *acpWriteTool) Schema() *tool.JSONSchema { return acpWriteSchema }

func (t *acpWriteTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	conn := t.currentConnection()
	if conn == nil {
		return nil, errors.New("acp write: connection is not available")
	}
	path, err := stringParam(params, "file_path", true)
	if err != nil {
		return nil, err
	}
	content, err := stringParam(params, "content", true)
	if err != nil {
		return nil, err
	}

	if _, err := conn.WriteTextFile(ctx, acpproto.WriteTextFileRequest{
		SessionId: t.sessionID,
		Path:      path,
		Content:   content,
	}); err != nil {
		return nil, fmt.Errorf("acp write_text_file: %w", err)
	}

	return &tool.ToolResult{
		Success: true,
		Output:  fmt.Sprintf("wrote %d bytes to %s", len(content), path),
		Data: map[string]any{
			"path":  path,
			"bytes": len(content),
		},
	}, nil
}

var acpEditSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]any{
		"file_path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the text file to modify.",
		},
		"old_string": map[string]any{
			"type":        "string",
			"description": "Exact text to replace.",
		},
		"new_string": map[string]any{
			"type":        "string",
			"description": "Replacement text.",
		},
		"replace_all": map[string]any{
			"type":        "boolean",
			"default":     false,
			"description": "Replace all matches when true (default false).",
		},
	},
	Required: []string{"file_path", "old_string", "new_string"},
}

type acpEditTool struct {
	sessionID acpproto.SessionId
	conn      func() *acpproto.AgentSideConnection
}

func (t *acpEditTool) Name() string { return "Edit" }

func (t *acpEditTool) Description() string {
	return "Perform exact string replacements via ACP fs/read_text_file and fs/write_text_file."
}

func (t *acpEditTool) Schema() *tool.JSONSchema { return acpEditSchema }

func (t *acpEditTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	conn := t.currentConnection()
	if conn == nil {
		return nil, errors.New("acp edit: connection is not available")
	}
	path, err := stringParam(params, "file_path", true)
	if err != nil {
		return nil, err
	}
	oldString, err := stringParam(params, "old_string", true)
	if err != nil {
		return nil, err
	}
	if oldString == "" {
		return nil, errors.New("old_string cannot be empty")
	}
	newString, err := stringParam(params, "new_string", true)
	if err != nil {
		return nil, err
	}
	if oldString == newString {
		return nil, errors.New("new_string must differ from old_string")
	}
	replaceAll, err := optionalBoolParam(params, "replace_all")
	if err != nil {
		return nil, err
	}

	readResp, err := conn.ReadTextFile(ctx, acpproto.ReadTextFileRequest{
		SessionId: t.sessionID,
		Path:      path,
	})
	if err != nil {
		return nil, fmt.Errorf("acp read_text_file: %w", err)
	}

	updated, matches, replaced, err := applyEditReplacement(readResp.Content, oldString, newString, replaceAll)
	if err != nil {
		return nil, err
	}

	if _, err := conn.WriteTextFile(ctx, acpproto.WriteTextFileRequest{
		SessionId: t.sessionID,
		Path:      path,
		Content:   updated,
	}); err != nil {
		return nil, fmt.Errorf("acp write_text_file: %w", err)
	}

	return &tool.ToolResult{
		Success: true,
		Output:  fmt.Sprintf("applied %d replacement(s)", replaced),
		Data: map[string]any{
			"path":        path,
			"matches":     matches,
			"replaced":    replaced,
			"replace_all": replaceAll,
		},
	}, nil
}

var acpBashSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "Command string executed through the ACP terminal capability.",
		},
		"timeout": map[string]any{
			"type":        "number",
			"description": "Optional timeout in seconds.",
		},
		"workdir": map[string]any{
			"type":        "string",
			"description": "Optional absolute working directory.",
		},
	},
	Required: []string{"command"},
}

type acpBashTool struct {
	sessionID acpproto.SessionId
	conn      func() *acpproto.AgentSideConnection
}

func (t *acpBashTool) Name() string { return "Bash" }

func (t *acpBashTool) Description() string {
	return "Execute shell commands via ACP client terminal/create and terminal/* methods."
}

func (t *acpBashTool) Schema() *tool.JSONSchema { return acpBashSchema }

func (t *acpBashTool) Execute(ctx context.Context, params map[string]interface{}) (result *tool.ToolResult, err error) {
	conn := t.currentConnection()
	if conn == nil {
		return nil, errors.New("acp bash: connection is not available")
	}
	command, err := stringParam(params, "command", true)
	if err != nil {
		return nil, err
	}
	workdir, err := stringParam(params, "workdir", false)
	if err != nil {
		return nil, err
	}
	timeout, err := optionalTimeoutParam(params, "timeout")
	if err != nil {
		return nil, err
	}

	shellCmd, shellArgs := shellInvocation(command)
	req := acpproto.CreateTerminalRequest{
		SessionId: t.sessionID,
		Command:   shellCmd,
		Args:      shellArgs,
	}
	if strings.TrimSpace(workdir) != "" {
		req.Cwd = &workdir
	}

	createResp, err := conn.CreateTerminal(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("acp create_terminal: %w", err)
	}
	terminalID := strings.TrimSpace(createResp.TerminalId)
	if terminalID == "" {
		return nil, errors.New("acp create_terminal returned empty terminal id")
	}
	defer func() {
		releaseCtx, cancelRelease := context.WithTimeout(context.Background(), acpTerminalCleanupTimeout)
		defer cancelRelease()
		_, releaseErr := conn.ReleaseTerminal(releaseCtx, acpproto.ReleaseTerminalRequest{
			SessionId:  t.sessionID,
			TerminalId: terminalID,
		})
		if releaseErr != nil {
			if err == nil {
				err = fmt.Errorf("acp release_terminal: %w", releaseErr)
				if result == nil {
					result = &tool.ToolResult{Success: false}
				}
				return
			}
			err = errors.Join(err, fmt.Errorf("acp release_terminal: %w", releaseErr))
		}
	}()

	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	waitResp, waitErr := conn.WaitForTerminalExit(waitCtx, acpproto.WaitForTerminalExitRequest{
		SessionId:  t.sessionID,
		TerminalId: terminalID,
	})
	if waitErr != nil {
		if errors.Is(waitErr, context.DeadlineExceeded) {
			killCtx, cancelKill := context.WithTimeout(context.Background(), acpTerminalCleanupTimeout)
			defer cancelKill()
			_, killErr := conn.KillTerminalCommand(killCtx, acpproto.KillTerminalCommandRequest{
				SessionId:  t.sessionID,
				TerminalId: terminalID,
			})
			if killErr != nil {
				return nil, errors.Join(
					fmt.Errorf("acp wait_for_terminal_exit: %w", waitErr),
					fmt.Errorf("acp kill_terminal_command: %w", killErr),
				)
			}
		}
		return nil, fmt.Errorf("acp wait_for_terminal_exit: %w", waitErr)
	}

	outputResp, err := conn.TerminalOutput(ctx, acpproto.TerminalOutputRequest{
		SessionId:  t.sessionID,
		TerminalId: terminalID,
	})
	if err != nil {
		return nil, fmt.Errorf("acp terminal_output: %w", err)
	}

	resultData := map[string]any{
		"terminal_id": terminalID,
		"truncated":   outputResp.Truncated,
	}
	result = &tool.ToolResult{
		Success: true,
		Output:  outputResp.Output,
		Data:    resultData,
	}
	if waitResp.ExitCode != nil {
		resultData["exit_code"] = *waitResp.ExitCode
		if *waitResp.ExitCode != 0 {
			result.Success = false
			return result, fmt.Errorf("command exited with code %d", *waitResp.ExitCode)
		}
	}
	if waitResp.Signal != nil && strings.TrimSpace(*waitResp.Signal) != "" {
		resultData["signal"] = *waitResp.Signal
	}
	return result, nil
}

func (t *acpReadTool) currentConnection() *acpproto.AgentSideConnection {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn()
}

func (t *acpWriteTool) currentConnection() *acpproto.AgentSideConnection {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn()
}

func (t *acpEditTool) currentConnection() *acpproto.AgentSideConnection {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn()
}

func (t *acpBashTool) currentConnection() *acpproto.AgentSideConnection {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn()
}

func shellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func stringParam(params map[string]interface{}, key string, required bool) (string, error) {
	if params == nil {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	switch v := raw.(type) {
	case string:
		if required && strings.TrimSpace(v) == "" {
			return "", fmt.Errorf("%s cannot be empty", key)
		}
		return strings.TrimSpace(v), nil
	default:
		return "", fmt.Errorf("%s must be a string", key)
	}
}

func optionalPositiveIntParam(params map[string]interface{}, key string) (*int, error) {
	if params == nil {
		return nil, nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return nil, nil
	}
	val, err := toInt(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be a number: %w", key, err)
	}
	if val <= 0 {
		return nil, fmt.Errorf("%s must be > 0", key)
	}
	return &val, nil
}

func optionalTimeoutParam(params map[string]interface{}, key string) (time.Duration, error) {
	if params == nil {
		return 0, nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return 0, nil
	}
	seconds, err := toFloat(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", key)
	}
	if seconds <= 0 {
		return 0, nil
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func optionalBoolParam(params map[string]interface{}, key string) (bool, error) {
	if params == nil {
		return false, nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return false, nil
	}
	switch v := raw.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y":
			return true, nil
		case "false", "0", "no", "n":
			return false, nil
		default:
			return false, fmt.Errorf("%s must be a boolean", key)
		}
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}

func applyEditReplacement(content, oldString, newString string, replaceAll bool) (string, int, int, error) {
	matches := strings.Count(content, oldString)
	if matches == 0 {
		return "", 0, 0, errors.New("old_string not found")
	}
	if !replaceAll && matches != 1 {
		return "", matches, 0, fmt.Errorf("old_string must be unique when replace_all is false (found %d matches)", matches)
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldString, newString), matches, matches, nil
	}
	return strings.Replace(content, oldString, newString, 1), matches, 1, nil
}

func toInt(value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case float32:
		return int(v), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(v))
	default:
		return 0, fmt.Errorf("unsupported int type %T", value)
	}
}

func toFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return 0, fmt.Errorf("unsupported float type %T", value)
	}
}

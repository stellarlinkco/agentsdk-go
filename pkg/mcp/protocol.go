package mcp

import (
	"encoding/json"
	"fmt"
)

const jsonRPCVersion = "2.0"

// Request models a JSON-RPC 2.0 request supported by MCP servers.
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// Response models a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error represents an MCP server error payload.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if len(e.Data) == 0 {
		return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("mcp error %d: %s (%s)", e.Code, e.Message, string(e.Data))
}

// ToolDescriptor describes an MCP tool announced by the server.
type ToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"input_schema"`
}

// ToolListResult models the payload of tools/list.
type ToolListResult struct {
	Tools []ToolDescriptor `json:"tools"`
}

// ToolCallParams drives tools/call requests.
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolCallResult mirrors the opaque result from tools/call.
type ToolCallResult struct {
	Content json.RawMessage `json:"content"`
}

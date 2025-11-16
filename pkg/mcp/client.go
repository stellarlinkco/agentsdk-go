package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
)

// Client exposes the MCP JSON-RPC surface independent of the transport.
type Client struct {
	transport Transport
	seq       atomic.Uint64
}

// NewClient wraps a transport with a higher-level JSON-RPC helper.
func NewClient(transport Transport) *Client {
	return &Client{transport: transport}
}

// Close tears down the underlying transport.
func (c *Client) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// Call issues a JSON-RPC request and decodes the result into dest when provided.
func (c *Client) Call(ctx context.Context, method string, params interface{}, dest interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req := &Request{
		JSONRPC: jsonRPCVersion,
		ID:      c.nextID(),
		Method:  method,
		Params:  params,
	}
	resp, err := c.transport.Call(ctx, req)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	if dest == nil || len(resp.Result) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Result, dest)
}

// ListTools fetches the server declared tool descriptors.
func (c *Client) ListTools(ctx context.Context) ([]ToolDescriptor, error) {
	var result ToolListResult
	if err := c.Call(ctx, "tools/list", map[string]interface{}{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// InvokeTool executes a remote MCP tool.
func (c *Client) InvokeTool(ctx context.Context, name string, args map[string]interface{}) (*ToolCallResult, error) {
	params := ToolCallParams{Name: name, Arguments: args}
	var out ToolCallResult
	if err := c.Call(ctx, "tools/call", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) nextID() string {
	id := c.seq.Add(1)
	if id == 0 {
		id = c.seq.Add(1)
	}
	return strconv.FormatUint(id, 10)
}

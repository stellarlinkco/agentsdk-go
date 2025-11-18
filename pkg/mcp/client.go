package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cexll/agentsdk-go/pkg/sandbox"
)

// preflightHook allows pluggable guards executed before each call.
type preflightHook func(context.Context, *Request) error

// Client exposes the MCP JSON-RPC surface independent of the transport.
type Client struct {
	transport Transport
	seq       atomic.Uint64
	preflight preflightHook
	cacheTTL  time.Duration
	now       func() time.Time

	cacheMu      sync.RWMutex
	cachedTools  []ToolDescriptor
	cacheExpires time.Time
}

// ClientOption customises a client instance.
type ClientOption func(*Client)

// NewClient wraps a transport with a higher-level JSON-RPC helper.
func NewClient(transport Transport, opts ...ClientOption) *Client {
	client := &Client{
		transport: transport,
		cacheTTL:  10 * time.Second,
		now:       time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}

// WithRetryPolicy wraps the client's transport in a retrying decorator.
func WithRetryPolicy(policy RetryPolicy) ClientOption {
	return func(c *Client) {
		if c == nil || c.transport == nil {
			return
		}
		c.transport = NewRetryTransport(c.transport, policy)
	}
}

// WithPreflight registers a hook executed before each transport call.
func WithPreflight(hook preflightHook) ClientOption {
	return func(c *Client) {
		if c == nil || hook == nil {
			return
		}
		if c.preflight == nil {
			c.preflight = hook
			return
		}
		prev := c.preflight
		c.preflight = func(ctx context.Context, req *Request) error {
			if err := prev(ctx, req); err != nil {
				return err
			}
			return hook(ctx, req)
		}
	}
}

// WithSandboxHostGuard ensures outbound calls stay within the sandbox network policy.
func WithSandboxHostGuard(manager *sandbox.Manager, host string) ClientOption {
	cleanHost := strings.TrimSpace(host)
	return WithPreflight(func(ctx context.Context, req *Request) error {
		if manager == nil || cleanHost == "" {
			return nil
		}
		return manager.CheckNetwork(cleanHost)
	})
}

// Close tears down the underlying transport.
func (c *Client) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	c.cacheMu.Lock()
	c.cachedTools = nil
	c.cacheExpires = time.Time{}
	c.cacheMu.Unlock()
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
	if c.preflight != nil {
		if err := c.preflight(ctx, req); err != nil {
			return err
		}
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
	if cached, ok := c.cachedToolSnapshot(); ok {
		return cached, nil
	}
	var result ToolListResult
	if err := c.Call(ctx, "tools/list", map[string]interface{}{}, &result); err != nil {
		return nil, err
	}
	c.storeTools(result.Tools)
	return copyToolDescriptors(result.Tools), nil
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

// WithToolCacheTTL overrides the cache expiration window for ListTools.
func WithToolCacheTTL(ttl time.Duration) ClientOption {
	if ttl < 0 {
		ttl = 0
	}
	return func(c *Client) {
		if c != nil {
			c.cacheTTL = ttl
		}
	}
}

// withClientClock overrides the clock for tests.
func withClientClock(clock func() time.Time) ClientOption {
	return func(c *Client) {
		if c != nil && clock != nil {
			c.now = clock
		}
	}
}

func (c *Client) cachedToolSnapshot() ([]ToolDescriptor, bool) {
	if c == nil || c.cacheTTL == 0 {
		return nil, false
	}
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	if len(c.cachedTools) == 0 {
		return nil, false
	}
	if c.cacheExpires.Before(c.now()) {
		return nil, false
	}
	return copyToolDescriptors(c.cachedTools), true
}

func (c *Client) storeTools(tools []ToolDescriptor) {
	if c == nil || c.cacheTTL == 0 {
		return
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cachedTools = copyToolDescriptors(tools)
	c.cacheExpires = c.now().Add(c.cacheTTL)
}

func copyToolDescriptors(in []ToolDescriptor) []ToolDescriptor {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolDescriptor, len(in))
	copy(out, in)
	return out
}

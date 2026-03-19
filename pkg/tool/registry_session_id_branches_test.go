package tool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stellarlinkco/agentsdk-go/pkg/mcp"
)

type stubTransportWithSessionID struct {
	server    *stubMCPServer
	sessionID string
	closeErr  error
}

func (t *stubTransportWithSessionID) Connect(context.Context) (mcp.Connection, error) {
	return newStubConnectionWithSessionID(t.server, t.sessionID, t.closeErr), nil
}

type stubConnectionWithSessionID struct {
	server    *stubMCPServer
	sessionID string
	closeErr  error

	incoming chan jsonrpc.Message
	outgoing chan jsonrpc.Message
	closed   chan struct{}
	once     sync.Once
}

func newStubConnectionWithSessionID(server *stubMCPServer, sessionID string, closeErr error) *stubConnectionWithSessionID {
	conn := &stubConnectionWithSessionID{
		server:    server,
		sessionID: sessionID,
		closeErr:  closeErr,
		incoming:  make(chan jsonrpc.Message, 16),
		outgoing:  make(chan jsonrpc.Message, 16),
		closed:    make(chan struct{}),
	}
	go server.serve(conn.incoming, conn.outgoing, conn.closed)
	return conn
}

func (c *stubConnectionWithSessionID) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, io.EOF
	case msg, ok := <-c.outgoing:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (c *stubConnectionWithSessionID) Write(ctx context.Context, msg jsonrpc.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errors.New("connection closed")
	case c.incoming <- msg:
		return nil
	}
}

func (c *stubConnectionWithSessionID) Close() error {
	c.once.Do(func() {
		close(c.closed)
		if c.server != nil {
			c.server.markClosed()
		}
	})
	return c.closeErr
}

func (c *stubConnectionWithSessionID) SessionID() string { return c.sessionID }

func TestFindMCPSessionLockedMatchesSessionIDViaSessionObject(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	client := mcp.NewClient(&mcp.Implementation{Name: "stub-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &stubTransportWithSessionID{server: server, sessionID: "sess-1"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	r := NewRegistry()
	r.mcpSessions = []*mcpSessionInfo{
		{serverID: "srv", sessionID: "", session: session},
	}

	r.mu.Lock()
	info := r.findMCPSessionLocked("", "sess-1")
	r.mu.Unlock()
	if info == nil || info.session != session {
		t.Fatalf("expected match by session.ID(), got %#v", info)
	}
}

func TestRegistryCloseLogsMCPSessionCloseError(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	client := mcp.NewClient(&mcp.Implementation{Name: "stub-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &stubTransportWithSessionID{
		server:    server,
		sessionID: "sess-err",
		closeErr:  errors.New("close failed"),
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	var buf bytes.Buffer
	prev := log.Default().Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	r := NewRegistry()
	r.mcpSessions = []*mcpSessionInfo{{serverID: "srv", session: session}}
	r.Close()

	if !strings.Contains(buf.String(), "close MCP session") {
		t.Fatalf("expected close error to be logged, got %q", buf.String())
	}
}

func TestRegisterMCPServerWithOptionsConnectContextErrorAfterDial(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	session, err := server.newSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	orig := newMCPClientWithOptions
	newMCPClientWithOptions = func(ctx context.Context, _ string, _ MCPServerOptions, _ mcpListChangedHandler) (*mcp.ClientSession, error) {
		<-ctx.Done()
		return session, nil
	}
	t.Cleanup(func() { newMCPClientWithOptions = orig })

	err = NewRegistry().RegisterMCPServerWithOptions(context.Background(), "fake", "svc", MCPServerOptions{Timeout: 5 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "connect context") {
		t.Fatalf("expected connect context error, got %v", err)
	}
	if !server.Closed() {
		t.Fatalf("expected session to be closed on failure")
	}
}

func TestRegisterMCPServerWithOptionsInitializeResultMissing(t *testing.T) {
	server := &stubMCPServer{tools: []*mcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}
	session, err := server.newSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	clearInitializeResult(session)
	t.Cleanup(func() { _ = session.Close() })

	orig := newMCPClientWithOptions
	newMCPClientWithOptions = func(context.Context, string, MCPServerOptions, mcpListChangedHandler) (*mcp.ClientSession, error) {
		return session, nil
	}
	t.Cleanup(func() { newMCPClientWithOptions = orig })

	err = NewRegistry().RegisterMCPServerWithOptions(context.Background(), "fake", "svc", MCPServerOptions{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "missing initialize result") {
		t.Fatalf("expected initialize error, got %v", err)
	}
}

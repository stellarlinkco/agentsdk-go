# ACP Integration Guide

This project provides an ACP adapter at [`pkg/acp`](../pkg/acp) using `github.com/coder/acp-go-sdk`.

Scope in this guide:

- Stable ACP methods only (`initialize`, `authenticate`, `session/new`, `session/load`, `session/prompt`, `session/cancel`, `session/set_mode`, `session/set_config_option`)
- CLI stdio mode and in-process library mode
- `unstable/*` ACP methods are intentionally out of scope

## 1) CLI Stdio Mode

Use the existing CLI entrypoint:

```bash
go run ./cmd/cli --acp=true
```

This starts an ACP agent server over stdin/stdout.

## 2) In-Process Go-to-Go Integration

When both sides are Go libraries, you can connect ACP client and agent in the same process without spawning CLI subprocesses.

```go
package main

import (
	"context"
	"net"

	acpadapter "github.com/cexll/agentsdk-go/pkg/acp"
	"github.com/cexll/agentsdk-go/pkg/api"
	acp "github.com/coder/acp-go-sdk"
)

type hostClient struct{}

func (hostClient) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{Content: "ok"}, nil
}
func (hostClient) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, nil
}
func (hostClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{OptionId: "allow_once"},
		},
	}, nil
}
func (hostClient) SessionUpdate(context.Context, acp.SessionNotification) error { return nil }
func (hostClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{TerminalId: "t-1"}, nil
}
func (hostClient) KillTerminalCommand(context.Context, acp.KillTerminalCommandRequest) (acp.KillTerminalCommandResponse, error) {
	return acp.KillTerminalCommandResponse{}, nil
}
func (hostClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{Output: "done", Truncated: false}, nil
}
func (hostClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, nil
}
func (hostClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	code := 0
	return acp.WaitForTerminalExitResponse{ExitCode: &code}, nil
}

func main() {
	agentIO, clientIO := net.Pipe()
	defer agentIO.Close()
	defer clientIO.Close()

	adapter := acpadapter.NewAdapter(api.Options{
		ProjectRoot: ".",
		// Model/ModelFactory setup omitted for brevity.
	})
	agentConn := acp.NewAgentSideConnection(adapter, agentIO, agentIO)
	adapter.SetConnection(agentConn)

	clientConn := acp.NewClientSideConnection(hostClient{}, clientIO, clientIO)

	ctx := context.Background()
	_, _ = clientConn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
	})
	sess, _ := clientConn.NewSession(ctx, acp.NewSessionRequest{Cwd: "D:/project", McpServers: []acp.McpServer{}})
	_, _ = clientConn.Prompt(ctx, acp.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock("hello")},
	})
}
```

## 3) Protocol Coverage Tests

`pkg/acp/e2e_test.go` contains in-process dual-end ACP tests with real `ClientSideConnection` and `AgentSideConnection`.

Coverage includes:

- Initialization and capability negotiation
- Session creation
- Prompt streaming updates
- Session cancellation
- Concurrent prompt rejection on same session
- Session load with persisted history replay
- Permission request round-trip
- Client capability bridges for `fs/*` and `terminal/*`

Run:

```bash
go test ./pkg/acp -v
go test ./...
```

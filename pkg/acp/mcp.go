package acp

import (
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/config"
	acpproto "github.com/coder/acp-go-sdk"
)

func mergeMCPServerSpecs(base []string, requested []acpproto.McpServer) ([]string, error) {
	specs := make([]string, 0, len(base)+len(requested))
	seen := make(map[string]struct{}, len(base)+len(requested))
	appendSpec := func(spec string) {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			return
		}
		if _, ok := seen[spec]; ok {
			return
		}
		seen[spec] = struct{}{}
		specs = append(specs, spec)
	}

	for _, spec := range base {
		appendSpec(spec)
	}
	if len(requested) == 0 {
		return specs, nil
	}

	for i, server := range requested {
		spec, err := acpMCPServerToSpec(server)
		if err != nil {
			return nil, fmt.Errorf("mcpServers[%d]: %w", i, err)
		}
		appendSpec(spec)
	}
	return specs, nil
}

func acpMCPServerToSpec(server acpproto.McpServer) (string, error) {
	switch {
	case server.Stdio != nil:
		command := strings.TrimSpace(server.Stdio.Command)
		if command == "" {
			return "", fmt.Errorf("stdio.command is required")
		}
		spec := "stdio://" + command
		if len(server.Stdio.Args) > 0 {
			spec += " " + strings.Join(server.Stdio.Args, " ")
		}
		return spec, nil

	case server.Http != nil:
		url := strings.TrimSpace(server.Http.Url)
		if url == "" {
			return "", fmt.Errorf("http.url is required")
		}
		return url, nil

	case server.Sse != nil:
		url := strings.TrimSpace(server.Sse.Url)
		if url == "" {
			return "", fmt.Errorf("sse.url is required")
		}
		return url, nil
	}

	return "", fmt.Errorf("one of stdio/http/sse transport must be provided")
}

func requestedMCPSettingsOverride(requested []acpproto.McpServer) (*config.Settings, error) {
	if len(requested) == 0 {
		return nil, nil
	}

	servers := make(map[string]config.MCPServerConfig, len(requested))
	for i, server := range requested {
		name, cfg, err := acpMCPServerToConfig(i, server)
		if err != nil {
			return nil, err
		}
		servers[name] = cfg
	}
	return &config.Settings{
		MCP: &config.MCPConfig{
			Servers: servers,
		},
	}, nil
}

func acpMCPServerToConfig(index int, server acpproto.McpServer) (string, config.MCPServerConfig, error) {
	switch {
	case server.Stdio != nil:
		command := strings.TrimSpace(server.Stdio.Command)
		if command == "" {
			return "", config.MCPServerConfig{}, fmt.Errorf("mcpServers[%d]: stdio.command is required", index)
		}
		name := normalizedRequestedMCPName(server.Stdio.Name, index)
		return name, config.MCPServerConfig{
			Type:    "stdio",
			Command: command,
			Args:    append([]string(nil), server.Stdio.Args...),
			Env:     envVarsToMap(server.Stdio.Env),
		}, nil

	case server.Http != nil:
		url := strings.TrimSpace(server.Http.Url)
		if url == "" {
			return "", config.MCPServerConfig{}, fmt.Errorf("mcpServers[%d]: http.url is required", index)
		}
		name := normalizedRequestedMCPName(server.Http.Name, index)
		return name, config.MCPServerConfig{
			Type:    "http",
			URL:     url,
			Headers: headersToMap(server.Http.Headers),
		}, nil

	case server.Sse != nil:
		url := strings.TrimSpace(server.Sse.Url)
		if url == "" {
			return "", config.MCPServerConfig{}, fmt.Errorf("mcpServers[%d]: sse.url is required", index)
		}
		name := normalizedRequestedMCPName(server.Sse.Name, index)
		return name, config.MCPServerConfig{
			Type:    "sse",
			URL:     url,
			Headers: headersToMap(server.Sse.Headers),
		}, nil
	}

	return "", config.MCPServerConfig{}, fmt.Errorf("mcpServers[%d]: one of stdio/http/sse transport must be provided", index)
}

func normalizedRequestedMCPName(raw string, index int) string {
	name := strings.TrimSpace(raw)
	if name != "" {
		return name
	}
	return fmt.Sprintf("acp-requested-%d", index+1)
}

func headersToMap(headers []acpproto.HttpHeader) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for _, header := range headers {
		name := strings.TrimSpace(header.Name)
		if name == "" {
			continue
		}
		out[name] = header.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func envVarsToMap(env []acpproto.EnvVariable) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for _, item := range env {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		out[name] = item.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

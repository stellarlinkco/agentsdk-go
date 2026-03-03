package acp

import (
	"fmt"
	"strings"

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

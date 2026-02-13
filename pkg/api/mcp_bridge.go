package api

import (
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/config"
)

type mcpServer struct {
	Name               string
	Spec               string
	URL                string
	Headers            map[string]string
	Env                map[string]string
	TimeoutSeconds     int
	EnabledTools       []string
	DisabledTools      []string
	ToolTimeoutSeconds int
}

// collectMCPServers merges explicit API inputs, settings.json entries, and
// managed allow/deny policies.
func collectMCPServers(settings *config.Settings, explicit []string) []mcpServer {
	seen := map[string]struct{}{}
	var servers []mcpServer
	allowRules := managedAllowRules(settings)
	denyRules := managedDenyRules(settings)

	add := func(server mcpServer) {
		server.Spec = strings.TrimSpace(server.Spec)
		if server.Spec == "" {
			return
		}
		if !allowedByManagedPolicies(server.Name, server.URL, allowRules, denyRules) {
			return
		}
		if _, ok := seen[server.Spec]; ok {
			return
		}
		seen[server.Spec] = struct{}{}
		server.Name = strings.TrimSpace(server.Name)
		server.URL = strings.TrimSpace(server.URL)
		servers = append(servers, server)
	}

	for _, spec := range explicit {
		add(mcpServer{Spec: spec, URL: spec})
	}

	if settings != nil && settings.MCP != nil {
		for name, cfg := range settings.MCP.Servers {
			// Convert MCPServerConfig to spec string
			spec := ""
			switch cfg.Type {
			case "http", "sse":
				spec = cfg.URL
			case "stdio":
				spec = fmt.Sprintf("stdio://%s %s", cfg.Command, strings.Join(cfg.Args, " "))
			default:
				if cfg.URL != "" {
					spec = cfg.URL
				}
			}
			add(mcpServer{
				Name:               name,
				Spec:               spec,
				URL:                cfg.URL,
				Headers:            cfg.Headers,
				Env:                cfg.Env,
				TimeoutSeconds:     cfg.TimeoutSeconds,
				EnabledTools:       cfg.EnabledTools,
				DisabledTools:      cfg.DisabledTools,
				ToolTimeoutSeconds: cfg.ToolTimeoutSeconds,
			})
		}
	}
	return servers
}

func managedAllowRules(s *config.Settings) []config.MCPServerRule {
	if s == nil {
		return nil
	}
	return s.AllowedMcpServers
}

func managedDenyRules(s *config.Settings) []config.MCPServerRule {
	if s == nil {
		return nil
	}
	return s.DeniedMcpServers
}

func allowedByManagedPolicies(name, target string, allow, deny []config.MCPServerRule) bool {
	if matchesRule(name, target, deny) {
		return false
	}
	if len(allow) == 0 {
		return true
	}
	return matchesRule(name, target, allow)
}

func matchesRule(name, target string, rules []config.MCPServerRule) bool {
	for _, rule := range rules {
		if rule.ServerName != "" && !strings.EqualFold(rule.ServerName, name) {
			continue
		}
		if rule.URL != "" && !strings.EqualFold(rule.URL, target) {
			continue
		}
		if strings.TrimSpace(rule.ServerName) == "" && strings.TrimSpace(rule.URL) == "" {
			continue
		}
		return true
	}
	return false
}

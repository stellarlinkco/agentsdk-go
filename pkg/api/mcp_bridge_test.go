package api

import (
	"testing"

	"github.com/cexll/agentsdk-go/pkg/config"
	"github.com/cexll/agentsdk-go/pkg/plugins"
)

func TestParseMCPEntryBuildsCommandSpec(t *testing.T) {
	spec, name, url := parseMCPEntry(map[string]any{
		"name":    "echoer",
		"command": "echo",
		"args":    []any{"hello", "world"},
	})
	if spec == "" || name != "echoer" || url != spec {
		t.Fatalf("unexpected entry parse: spec=%q name=%q url=%q", spec, name, url)
	}
	if spec[:8] != "stdio://" {
		t.Fatalf("expected stdio scheme, got %s", spec)
	}
}

func TestParseMCPEntryIgnoresInvalidTypes(t *testing.T) {
	if spec, _, _ := parseMCPEntry("invalid"); spec != "" {
		t.Fatalf("expected empty spec for invalid entry, got %q", spec)
	}
}

func TestAllowedByManagedPoliciesPrefersDeny(t *testing.T) {
	deny := []config.MCPServerRule{{ServerName: "svc"}, {URL: "http://svc"}}
	allow := []config.MCPServerRule{{ServerName: "svc"}}
	if allowed := allowedByManagedPolicies("svc", "http://svc", allow, deny); allowed {
		t.Fatal("deny rule should win over allow list")
	}
}

func TestCollectMCPServersMergesSourcesAndDedups(t *testing.T) {
	settings := &config.Settings{MCPServers: []string{"http://settings.example"}}
	plugin := &plugins.ClaudePlugin{
		Name: "plug",
		MCPConfig: &plugins.MCPConfig{
			Data: map[string]any{"servers": []any{
				map[string]any{"name": "plug", "url": "http://plugin.example"},
				map[string]any{"url": "http://settings.example"},
			}},
		},
	}
	servers := collectMCPServers(settings, []*plugins.ClaudePlugin{plugin}, []string{"http://settings.example"})
	if len(servers) != 2 {
		t.Fatalf("expected deduped two servers, got %d: %+v", len(servers), servers)
	}
}

func TestPluginMCPServersRespectsEnableDisable(t *testing.T) {
	enable := true
	settings := &config.Settings{
		EnableAllProjectMCPServers: &enable,
		EnabledMCPJSONServers:      []string{"keep"},
		DisabledMCPJSONServers:     []string{"drop"},
	}
	plugin := &plugins.ClaudePlugin{
		Name: "plug",
		MCPConfig: &plugins.MCPConfig{
			Data: map[string]any{"servers": []any{
				map[string]any{"name": "keep", "url": "http://ok"},
				map[string]any{"name": "drop", "url": "http://denied"},
			}},
		},
	}
	entries := pluginMCPServers(settings, []*plugins.ClaudePlugin{plugin})
	if len(entries) != 1 || entries[0].Name != "keep" {
		t.Fatalf("expected only enabled server, got %+v", entries)
	}
}

func TestDescribeRuleFormatsNicely(t *testing.T) {
	rule := config.MCPServerRule{ServerName: "svc", URL: "http://example"}
	if out := describeRule(rule); out == "" || out != "svc (http://example)" {
		t.Fatalf("unexpected describe rule output: %q", out)
	}
	onlyURL := config.MCPServerRule{URL: "http://only"}
	if out := describeRule(onlyURL); out != "http://only" {
		t.Fatalf("expected URL fallback, got %q", out)
	}
}

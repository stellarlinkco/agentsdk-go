package acp

import (
	"testing"

	acpproto "github.com/coder/acp-go-sdk"
)

func TestRequestedMCPSettingsOverrideMapsServerOptions(t *testing.T) {
	t.Parallel()

	settings, err := requestedMCPSettingsOverride([]acpproto.McpServer{
		{
			Http: &acpproto.McpServerHttpInline{
				Type: "http",
				Name: "team-http",
				Url:  "https://mcp.example",
				Headers: []acpproto.HttpHeader{
					{Name: "Authorization", Value: "Bearer token"},
				},
			},
		},
		{
			Stdio: &acpproto.McpServerStdio{
				Name:    "",
				Command: "npx",
				Args:    []string{"-y", "@acme/server"},
				Env: []acpproto.EnvVariable{
					{Name: "NODE_ENV", Value: "production"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("requestedMCPSettingsOverride failed: %v", err)
	}
	if settings == nil || settings.MCP == nil {
		t.Fatalf("expected MCP settings override, got %+v", settings)
	}
	if len(settings.MCP.Servers) != 2 {
		t.Fatalf("server count=%d, want 2", len(settings.MCP.Servers))
	}

	httpCfg, ok := settings.MCP.Servers["team-http"]
	if !ok {
		t.Fatalf("missing named http server config: %+v", settings.MCP.Servers)
	}
	if httpCfg.Type != "http" || httpCfg.URL != "https://mcp.example" {
		t.Fatalf("unexpected http cfg: %+v", httpCfg)
	}
	if got := httpCfg.Headers["Authorization"]; got != "Bearer token" {
		t.Fatalf("http header Authorization=%q, want %q", got, "Bearer token")
	}

	stdioCfg, ok := settings.MCP.Servers["acp-requested-2"]
	if !ok {
		t.Fatalf("missing generated stdio server name: %+v", settings.MCP.Servers)
	}
	if stdioCfg.Type != "stdio" || stdioCfg.Command != "npx" {
		t.Fatalf("unexpected stdio cfg: %+v", stdioCfg)
	}
	if got := stdioCfg.Env["NODE_ENV"]; got != "production" {
		t.Fatalf("stdio env NODE_ENV=%q, want %q", got, "production")
	}
}

func TestACPMCPServerToConfigVariants(t *testing.T) {
	t.Parallel()

	name, cfg, err := acpMCPServerToConfig(0, acpproto.McpServer{
		Stdio: &acpproto.McpServerStdio{
			Command: "node",
			Args:    []string{"server.js"},
			Env: []acpproto.EnvVariable{
				{Name: "A", Value: "1"},
				{Name: " ", Value: "ignored"},
			},
		},
	})
	if err != nil {
		t.Fatalf("stdio to config failed: %v", err)
	}
	if name != "acp-requested-1" {
		t.Fatalf("stdio name=%q, want %q", name, "acp-requested-1")
	}
	if cfg.Type != "stdio" || cfg.Command != "node" {
		t.Fatalf("unexpected stdio cfg: %+v", cfg)
	}
	if cfg.Env["A"] != "1" {
		t.Fatalf("unexpected stdio env map: %+v", cfg.Env)
	}

	name, cfg, err = acpMCPServerToConfig(1, acpproto.McpServer{
		Http: &acpproto.McpServerHttpInline{
			Name: "http-mcp",
			Url:  "https://mcp.example",
			Headers: []acpproto.HttpHeader{
				{Name: "Authorization", Value: "Bearer x"},
				{Name: " ", Value: "ignored"},
			},
		},
	})
	if err != nil {
		t.Fatalf("http to config failed: %v", err)
	}
	if name != "http-mcp" {
		t.Fatalf("http name=%q, want %q", name, "http-mcp")
	}
	if cfg.Type != "http" || cfg.URL != "https://mcp.example" {
		t.Fatalf("unexpected http cfg: %+v", cfg)
	}
	if cfg.Headers["Authorization"] != "Bearer x" {
		t.Fatalf("unexpected http headers: %+v", cfg.Headers)
	}

	name, cfg, err = acpMCPServerToConfig(2, acpproto.McpServer{
		Sse: &acpproto.McpServerSseInline{
			Url: "https://mcp.example/sse",
		},
	})
	if err != nil {
		t.Fatalf("sse to config failed: %v", err)
	}
	if name != "acp-requested-3" {
		t.Fatalf("sse name=%q, want %q", name, "acp-requested-3")
	}
	if cfg.Type != "sse" || cfg.URL != "https://mcp.example/sse" {
		t.Fatalf("unexpected sse cfg: %+v", cfg)
	}
}

func TestACPMCPServerToConfigRejectsInvalid(t *testing.T) {
	t.Parallel()

	if _, _, err := acpMCPServerToConfig(0, acpproto.McpServer{
		Stdio: &acpproto.McpServerStdio{Command: "   "},
	}); err == nil {
		t.Fatalf("expected invalid stdio command to fail")
	}
	if _, _, err := acpMCPServerToConfig(1, acpproto.McpServer{
		Http: &acpproto.McpServerHttpInline{Url: "  "},
	}); err == nil {
		t.Fatalf("expected invalid http url to fail")
	}
	if _, _, err := acpMCPServerToConfig(2, acpproto.McpServer{
		Sse: &acpproto.McpServerSseInline{Url: "  "},
	}); err == nil {
		t.Fatalf("expected invalid sse url to fail")
	}
	if _, _, err := acpMCPServerToConfig(3, acpproto.McpServer{}); err == nil {
		t.Fatalf("expected missing transport to fail")
	}
}

func TestMCPHeaderAndEnvMappingHelpers(t *testing.T) {
	t.Parallel()

	headers := headersToMap([]acpproto.HttpHeader{
		{Name: "Authorization", Value: "Bearer token"},
		{Name: " ", Value: "ignored"},
	})
	if len(headers) != 1 || headers["Authorization"] != "Bearer token" {
		t.Fatalf("unexpected headers map: %+v", headers)
	}
	if got := headersToMap([]acpproto.HttpHeader{{Name: " "}}); got != nil {
		t.Fatalf("expected nil map for empty header names, got %+v", got)
	}

	env := envVarsToMap([]acpproto.EnvVariable{
		{Name: "NODE_ENV", Value: "production"},
		{Name: " ", Value: "ignored"},
	})
	if len(env) != 1 || env["NODE_ENV"] != "production" {
		t.Fatalf("unexpected env map: %+v", env)
	}
	if got := envVarsToMap([]acpproto.EnvVariable{{Name: " "}}); got != nil {
		t.Fatalf("expected nil map for empty env names, got %+v", got)
	}
}

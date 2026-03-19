package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettingsMergeCoverageMore(t *testing.T) {
	t.Parallel()

	// MergeSettings lower==nil branch.
	require.Equal(t, "high", MergeSettings(nil, &Settings{APIKeyHelper: "high"}).APIKeyHelper)

	lower := &Settings{
		CompanyAnnouncements: []string{"a"},
		DisallowedTools:      []string{"bash"},
		LegacyMCPServers:     []string{"legacy"},
		AllowedMcpServers:    []MCPServerRule{{ServerName: "lower"}},
		DeniedMcpServers:     []MCPServerRule{{ServerName: "lower-deny"}},
		Permissions:          &PermissionsConfig{Allow: []string{"Read(**/*.md)"}, DefaultMode: "allow"},
		Sandbox: &SandboxConfig{
			Network: &SandboxNetworkConfig{
				AllowUnixSockets: []string{"/var/run/docker.sock"},
			},
		},
		BashOutput: &BashOutputConfig{AsyncThresholdBytes: intPtr(1)},
		ToolOutput: &ToolOutputConfig{PerToolThresholdBytes: map[string]int{"Bash": 1}},
	}
	higher := &Settings{
		IncludeCoAuthoredBy: boolPtr(true),
		DisableAllHooks:     boolPtr(true),
		OutputStyle:         "json",
		ForceLoginMethod:    "oidc",
		ForceLoginOrgUUID:   "org-uuid",
		AWSAuthRefresh:      "refresh",
		AWSCredentialExport: "export",
		Permissions: &PermissionsConfig{
			Ask:                          []string{"Read(**/draft.md)"},
			DisableBypassPermissionsMode: "on",
		},
		Sandbox: &SandboxConfig{
			Enabled:                   boolPtr(false),
			AutoAllowBashIfSandboxed:  boolPtr(true),
			AllowUnsandboxedCommands:  boolPtr(true),
			EnableWeakerNestedSandbox: boolPtr(true),
			ExcludedCommands:          []string{"rm"},
			Network: &SandboxNetworkConfig{
				AllowLocalBinding: boolPtr(true),
				HTTPProxyPort:     intPtr(8080),
				SocksProxyPort:    intPtr(1080),
			},
		},
		ToolOutput: &ToolOutputConfig{DefaultThresholdBytes: 10},
	}

	out := MergeSettings(lower, higher)
	require.True(t, *out.IncludeCoAuthoredBy)
	require.True(t, *out.DisableAllHooks)
	require.Equal(t, "json", out.OutputStyle)
	require.Equal(t, "oidc", out.ForceLoginMethod)
	require.Equal(t, "org-uuid", out.ForceLoginOrgUUID)
	require.Equal(t, "refresh", out.AWSAuthRefresh)
	require.Equal(t, "export", out.AWSCredentialExport)

	require.Equal(t, []string{"Read(**/*.md)"}, out.Permissions.Allow)
	require.Equal(t, []string{"Read(**/draft.md)"}, out.Permissions.Ask)
	require.Equal(t, "allow", out.Permissions.DefaultMode)
	require.Equal(t, "on", out.Permissions.DisableBypassPermissionsMode)

	require.NotNil(t, out.Sandbox)
	require.False(t, *out.Sandbox.Enabled)
	require.True(t, *out.Sandbox.AutoAllowBashIfSandboxed)
	require.True(t, *out.Sandbox.AllowUnsandboxedCommands)
	require.True(t, *out.Sandbox.EnableWeakerNestedSandbox)
	require.Equal(t, []string{"rm"}, out.Sandbox.ExcludedCommands)
	require.NotNil(t, out.Sandbox.Network)
	require.Equal(t, []string{"/var/run/docker.sock"}, out.Sandbox.Network.AllowUnixSockets)
	require.True(t, *out.Sandbox.Network.AllowLocalBinding)
	require.Equal(t, 8080, *out.Sandbox.Network.HTTPProxyPort)
	require.Equal(t, 1080, *out.Sandbox.Network.SocksProxyPort)

	require.Equal(t, 10, out.ToolOutput.DefaultThresholdBytes)
	require.Equal(t, map[string]int{"Bash": 1}, out.ToolOutput.PerToolThresholdBytes)

	// Hit helper nil branches directly.
	require.Nil(t, mergePermissions(nil, nil))
	require.Nil(t, mergeSandbox(nil, nil))
	require.Nil(t, mergeSandboxNetwork(nil, nil))
	require.Nil(t, mergeToolOutput(nil, nil))
	require.Nil(t, mergeMaps(nil, nil))
	require.Nil(t, mergeIntMap(nil, nil))

	// Exercise duplicate handling branches.
	require.Equal(t, []string{"x", "y"}, mergeStringSlices([]string{"x", "x"}, []string{"x", "y", "y"}))

	// Cover mergeStatusLine lower==nil and Command override.
	require.Equal(t, "echo hi", mergeStatusLine(nil, &StatusLineConfig{Command: "echo hi"}).Command)

	// Cover mergeMCPConfig lower==nil and higher==nil.
	require.Nil(t, mergeMCPConfig(nil, nil))
	require.NotNil(t, mergeMCPConfig(nil, &MCPConfig{Servers: map[string]MCPServerConfig{"x": {Type: "stdio"}}}))
	require.NotNil(t, mergeMCPConfig(&MCPConfig{Servers: map[string]MCPServerConfig{"x": {Type: "stdio"}}}, nil))

	// Cover clone helpers nil branches.
	require.Nil(t, cloneSettings(nil))
	require.Nil(t, cloneMCPConfig(nil))
	require.Nil(t, cloneStatusLine(nil))
	require.Nil(t, cloneBoolPtr(nil))
}

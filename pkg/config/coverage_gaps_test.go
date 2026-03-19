package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadJSONFile_Edges(t *testing.T) {
	t.Parallel()

	cfg, err := loadJSONFile("   ", nil)
	require.NoError(t, err)
	require.Nil(t, cfg)

	dir := t.TempDir()
	cfg, err = loadJSONFile(dir, nil)
	require.Error(t, err)
	require.Nil(t, cfg)
}

func TestApplySettingsLayer_SkipsEmptyPath(t *testing.T) {
	t.Parallel()

	var dst Settings
	require.NoError(t, applySettingsLayer(&dst, "project", "", nil))
}

func TestSettingsMerge_CoverageGaps(t *testing.T) {
	t.Parallel()

	require.NotNil(t, mergePermissions(nil, &PermissionsConfig{DefaultMode: "acceptEdits"}))
	require.NotNil(t, mergeSandbox(nil, &SandboxConfig{Enabled: boolPtr(true)}))
	require.NotNil(t, mergeSandboxNetwork(nil, &SandboxNetworkConfig{AllowUnixSockets: []string{"/tmp/x"}}))
	require.NotNil(t, mergeToolOutput(&ToolOutputConfig{DefaultThresholdBytes: 1}, nil))

	lowerStatus := &StatusLineConfig{Type: "command", Command: "low", IntervalSeconds: 1}
	higherStatus := &StatusLineConfig{Command: "high", IntervalSeconds: 2}
	outStatus := mergeStatusLine(lowerStatus, higherStatus)
	require.Equal(t, "high", outStatus.Command)
	require.Equal(t, 2, outStatus.IntervalSeconds)

	outMCP := mergeMCPConfig(&MCPConfig{}, &MCPConfig{Servers: map[string]MCPServerConfig{
		"remote": {Type: "http", URL: "https://example"},
	}})
	require.NotNil(t, outMCP.Servers)
	require.Contains(t, outMCP.Servers, "remote")

	port := 1080
	network := cloneSandboxNetwork(&SandboxNetworkConfig{SocksProxyPort: &port})
	require.NotNil(t, network)
	require.NotNil(t, network.SocksProxyPort)
	require.Equal(t, 1080, *network.SocksProxyPort)
	require.NotSame(t, &port, network.SocksProxyPort)
}

func TestAgentsMDLoader_CoverageGaps(t *testing.T) {
	t.Parallel()

	loader := includedMDLoader{root: t.TempDir(), visited: map[string]struct{}{}, label: "agents.md"}
	content, err := loader.load("   ", 0)
	require.NoError(t, err)
	require.Equal(t, "", content)

	content, err = loader.load("relative.md", 0)
	require.NoError(t, err)
	require.Equal(t, "", content)

	_, err = loader.load("missing.md", 1)
	require.Error(t, err)

	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o600))
	loader = includedMDLoader{root: root, visited: map[string]struct{}{}, isWindows: true, label: "agents.md"}
	_, err = loader.load(path, 0)
	require.NoError(t, err)

	_, err = readFileLimited(nil, root, includedMDMaxFileBytes, "agents.md")
	require.Error(t, err)
}

func TestValidateSettings_CoverageGaps(t *testing.T) {
	t.Parallel()

	require.Error(t, ValidateSettings(nil))
	require.NoError(t, validateToolPattern("*"))

	errs := validateToolOutputConfig(&ToolOutputConfig{DefaultThresholdBytes: -1})
	require.Len(t, errs, 1)

	mcpErrs := validateMCPConfig(&MCPConfig{Servers: map[string]MCPServerConfig{
		"bad": {Type: "nope"},
	}}, nil)
	joined := errors.Join(mcpErrs...)
	require.Error(t, joined)
	require.True(t, strings.Contains(joined.Error(), "not supported"))

	statusErrs := validateStatusLineConfig(&StatusLineConfig{})
	require.NotEmpty(t, statusErrs)
}

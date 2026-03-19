package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateToolPattern_Empty(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, validateToolPattern(""), "empty")
	require.ErrorContains(t, validateToolPattern("   "), "empty")
}

func TestValidateHookEntries_CoversBranches(t *testing.T) {
	t.Parallel()

	errs := validateHookEntries("hooks.PreToolUse", []HookMatcherEntry{
		{Matcher: "", Hooks: nil},                                                // empty hooks
		{Matcher: "bash", Hooks: []HookDefinition{{Type: "prompt"}}},             // missing prompt
		{Matcher: "bash", Hooks: []HookDefinition{{Type: "agent", Timeout: -1}}}, // missing prompt + negative timeout
		{Matcher: "bash", Hooks: []HookDefinition{{Type: "unknown"}}},            // unsupported type
	})
	require.GreaterOrEqual(t, len(errs), 4)
	msg := joinErrors(errs)
	require.Contains(t, msg, "hooks array is empty")
	require.Contains(t, msg, "prompt is required")
	require.Contains(t, msg, "timeout must be >= 0")
	require.Contains(t, msg, "unsupported type")
}

func TestValidateToolOutputConfig_KeyNormalization(t *testing.T) {
	t.Parallel()

	cfg := &ToolOutputConfig{
		DefaultThresholdBytes: 0,
		PerToolThresholdBytes: map[string]int{
			"":     1,
			" OK ": 1,
			"Bad":  1,
			"good": 0,
		},
	}
	errs := validateToolOutputConfig(cfg)
	require.GreaterOrEqual(t, len(errs), 4)
	msg := joinErrors(errs)
	require.Contains(t, msg, "empty tool name")
	require.Contains(t, msg, "leading/trailing whitespace")
	require.Contains(t, msg, "must be lowercase")
	require.Contains(t, msg, "must be >0")
}

func TestValidateMCPConfig_DefaultTypeAndEmptyServerName(t *testing.T) {
	t.Parallel()

	cfg := &MCPConfig{Servers: map[string]MCPServerConfig{
		" ":   {Type: "stdio", Command: "echo ok"},
		"ok":  {}, // type defaults to stdio; missing command should fail
		"sse": {Type: "sse", URL: "https://example"},
	}}

	errs := validateMCPConfig(cfg, nil)
	require.NotEmpty(t, errs)
	msg := joinErrors(errs)
	require.Contains(t, msg, "mcp.servers has an empty name")
	require.Contains(t, msg, "mcp.servers[ok].command is required")
	require.NotContains(t, msg, "mcp.servers[sse].url is required")
}

func TestValidateStatusLineConfig_NegativeFields(t *testing.T) {
	t.Parallel()

	errs := validateStatusLineConfig(&StatusLineConfig{
		Type:            "command",
		Command:         "echo ok",
		IntervalSeconds: -1,
		TimeoutSeconds:  -1,
	})
	require.Len(t, errs, 2)
	require.Contains(t, joinErrors(errs), "cannot be negative")
}

func joinErrors(errs []error) string {
	out := ""
	for _, err := range errs {
		if err == nil {
			continue
		}
		out += err.Error()
		out += "\n"
	}
	return out
}

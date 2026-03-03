package acp

import (
	"testing"

	"github.com/cexll/agentsdk-go/pkg/api"
	acpproto "github.com/coder/acp-go-sdk"
)

func TestBuildClientCapabilityTools(t *testing.T) {
	t.Parallel()

	noneTools, noneDisallowed := buildClientCapabilityTools("sess-1", nil, acpproto.ClientCapabilities{})
	if len(noneTools) != 0 {
		t.Fatalf("no-capability tool count=%d, want 0", len(noneTools))
	}
	if len(noneDisallowed) != 0 {
		t.Fatalf("no-capability disallowed count=%d, want 0", len(noneDisallowed))
	}

	caps := acpproto.ClientCapabilities{}
	caps.Fs.ReadTextFile = true
	caps.Fs.WriteTextFile = true
	caps.Terminal = true

	tools, disallowed := buildClientCapabilityTools("sess-2", nil, caps)
	if len(tools) != 3 {
		t.Fatalf("tool count=%d, want 3", len(tools))
	}
	if len(disallowed) != 3 {
		t.Fatalf("disallowed count=%d, want 3", len(disallowed))
	}

	gotNames := make(map[string]struct{}, len(tools))
	for _, tl := range tools {
		gotNames[tl.Name()] = struct{}{}
	}
	for _, name := range []string{"Read", "Write", "Bash"} {
		if _, ok := gotNames[name]; !ok {
			t.Fatalf("missing tool %q in %#v", name, gotNames)
		}
		if !containsString(disallowed, name) {
			t.Fatalf("missing disallowed entry %q in %#v", name, disallowed)
		}
	}
}

func TestFilterEnabledBuiltinsForBridge(t *testing.T) {
	t.Parallel()

	opts := api.Options{
		EntryPoint:          api.EntryPointCLI,
		EnabledBuiltinTools: nil, // default built-ins
	}
	filtered := filterEnabledBuiltinsForBridge(opts, []string{"Read", "Write", "Bash"})
	forbidden := map[string]struct{}{
		"bash":       {},
		"file_read":  {},
		"file_write": {},
	}
	for _, builtin := range filtered {
		if _, blocked := forbidden[canonicalBuiltinName(builtin)]; blocked {
			t.Fatalf("filtered builtins still contains shadowed builtin %q", builtin)
		}
	}
	if !containsString(filtered, "grep") {
		t.Fatalf("expected non-shadowed builtins to be preserved")
	}
	if !containsString(filtered, "glob") {
		t.Fatalf("expected non-shadowed builtins to be preserved")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

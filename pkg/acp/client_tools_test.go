package acp

import (
	"testing"

	"github.com/cexll/agentsdk-go/pkg/tool"
	acpproto "github.com/coder/acp-go-sdk"
)

func TestBuildClientCapabilityTools(t *testing.T) {
	t.Parallel()

	noneTools, noneShadowed := buildClientCapabilityTools("sess-1", nil, acpproto.ClientCapabilities{})
	if len(noneTools) != 0 {
		t.Fatalf("no-capability tool count=%d, want 0", len(noneTools))
	}
	if len(noneShadowed) != 0 {
		t.Fatalf("no-capability shadowed count=%d, want 0", len(noneShadowed))
	}

	caps := acpproto.ClientCapabilities{}
	caps.Fs.ReadTextFile = true
	caps.Fs.WriteTextFile = true
	caps.Terminal = true

	tools, shadowed := buildClientCapabilityTools("sess-2", nil, caps)
	if len(tools) != 4 {
		t.Fatalf("tool count=%d, want 4", len(tools))
	}
	if len(shadowed) != 4 {
		t.Fatalf("shadowed count=%d, want 4", len(shadowed))
	}

	gotNames := make(map[string]struct{}, len(tools))
	for _, tl := range tools {
		gotNames[tl.Name()] = struct{}{}
	}
	for _, name := range []string{"Read", "Write", "Edit", "Bash"} {
		if _, ok := gotNames[name]; !ok {
			t.Fatalf("missing tool %q in %#v", name, gotNames)
		}
	}
	for _, key := range []string{"file_read", "file_write", "file_edit", "bash"} {
		if !containsString(shadowed, key) {
			t.Fatalf("missing shadowed builtin %q in %#v", key, shadowed)
		}
	}

	writeOnlyCaps := acpproto.ClientCapabilities{}
	writeOnlyCaps.Fs.WriteTextFile = true
	writeOnlyTools, writeOnlyShadowed := buildClientCapabilityTools("sess-3", nil, writeOnlyCaps)
	if len(writeOnlyTools) != 1 || writeOnlyTools[0].Name() != "Write" {
		t.Fatalf("write-only tools=%v, want [Write]", toolNames(writeOnlyTools))
	}
	if !containsString(writeOnlyShadowed, "file_write") || !containsString(writeOnlyShadowed, "file_edit") {
		t.Fatalf("write-only shadowed=%v, want file_write+file_edit", writeOnlyShadowed)
	}
	if containsString(writeOnlyShadowed, "file_read") {
		t.Fatalf("write-only shadowed should not include file_read: %v", writeOnlyShadowed)
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

func toolNames(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Name())
	}
	return names
}

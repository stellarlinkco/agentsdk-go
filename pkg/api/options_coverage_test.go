package api

import (
	"reflect"
	"testing"
)

func TestRequestNormalizedPopulatesDefaults(t *testing.T) {
	defaultMode := ModeContext{
		EntryPoint: EntryPointPlatform,
	}

	req := Request{
		Prompt:        "hello",
		Mode:          ModeContext{},
		SessionID:     "",
		Channels:      []string{"c2", "c1", "c1"},
		Traits:        []string{"t2", "t1", "t2"},
		ToolWhitelist: []string{"Bash", "Bash"},
	}

	normalized := req.normalized(defaultMode, "  sess  ")

	if normalized.Mode.EntryPoint != defaultMode.EntryPoint {
		t.Fatalf("Mode.EntryPoint=%q, want %q", normalized.Mode.EntryPoint, defaultMode.EntryPoint)
	}
	if normalized.SessionID != "sess" {
		t.Fatalf("SessionID=%q, want %q", normalized.SessionID, "sess")
	}
	if normalized.Tags == nil || normalized.Metadata == nil {
		t.Fatalf("expected tags/metadata maps allocated, got tags=%v metadata=%v", normalized.Tags, normalized.Metadata)
	}

	if !reflect.DeepEqual(normalized.Channels, []string{"c1", "c2"}) {
		t.Fatalf("Channels=%v, want [c1 c2]", normalized.Channels)
	}
	if !reflect.DeepEqual(normalized.Traits, []string{"t1", "t2"}) {
		t.Fatalf("Traits=%v, want [t1 t2]", normalized.Traits)
	}
	if !reflect.DeepEqual(normalized.ToolWhitelist, []string{"Bash"}) {
		t.Fatalf("ToolWhitelist=%v, want [Bash]", normalized.ToolWhitelist)
	}
}

package api

import (
	"reflect"
	"testing"
)

func TestFreezeModeDeepCopy(t *testing.T) {
	mode := ModeContext{
		EntryPoint: EntryPointCLI,
		CLI: &CLIContext{
			Args:  []string{"a"},
			Flags: map[string]string{"k": "v"},
		},
		CI: &CIContext{
			Matrix:   map[string]string{"os": "linux"},
			Metadata: map[string]string{"build": "1"},
		},
		Platform: &PlatformContext{
			Labels: map[string]string{"org": "acme"},
		},
	}

	frozen := freezeMode(mode)
	if frozen.CLI == mode.CLI || frozen.CI == mode.CI || frozen.Platform == mode.Platform {
		t.Fatal("expected freezeMode to deep copy nested pointers")
	}

	mode.CLI.Args[0] = "b"
	mode.CLI.Flags["k"] = "changed"
	mode.CI.Matrix["os"] = "windows"
	mode.CI.Metadata["build"] = "2"
	mode.Platform.Labels["org"] = "other"

	if frozen.CLI.Args[0] != "a" {
		t.Fatalf("CLI.Args=%v, want %v", frozen.CLI.Args, []string{"a"})
	}
	if frozen.CLI.Flags["k"] != "v" {
		t.Fatalf("CLI.Flags=%v, want map[k:v]", frozen.CLI.Flags)
	}
	if frozen.CI.Matrix["os"] != "linux" || frozen.CI.Metadata["build"] != "1" {
		t.Fatalf("CI=%+v, want original values preserved", frozen.CI)
	}
	if frozen.Platform.Labels["org"] != "acme" {
		t.Fatalf("Platform.Labels=%v, want map[org:acme]", frozen.Platform.Labels)
	}
}

func TestRequestNormalizedPopulatesDefaults(t *testing.T) {
	defaultMode := ModeContext{
		EntryPoint: EntryPointPlatform,
		CLI:        &CLIContext{Args: []string{"--flag"}, Flags: map[string]string{"x": "y"}},
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

package api

import (
	"testing"

	"github.com/cexll/agentsdk-go/pkg/config"
	"github.com/cexll/agentsdk-go/pkg/plugins"
)

func TestDiscoverPluginsPropagatesScanError(t *testing.T) {
	if _, err := discoverPlugins("", nil); err == nil {
		t.Fatal("expected scan error for empty project root")
	}
}

func TestEnabledPluginMapTrimsKeys(t *testing.T) {
	settings := &config.Settings{EnabledPlugins: map[string]bool{" plugin ": true, "": true}}
	enabled := enabledPluginMap(settings)
	if len(enabled) != 1 || !enabled["plugin"] {
		t.Fatalf("unexpected enabled map: %+v", enabled)
	}
}

func TestSnapshotPluginsNormalizesHooksAndSorting(t *testing.T) {
	plugin := &plugins.ClaudePlugin{
		Name:        "demo",
		Version:     "1.0.0",
		Description: "desc",
		Commands:    []string{"b", "a"},
		Agents:      []string{"z"},
		Skills:      []string{"s"},
		Hooks: map[string][]string{
			" pre ": {"b", "a"},
			" ":     {"ignored"},
		},
	}
	snaps := snapshotPlugins([]*plugins.ClaudePlugin{plugin, nil})
	if len(snaps) != 1 {
		t.Fatalf("expected single snapshot, got %d", len(snaps))
	}
	if snaps[0].Commands[0] != "a" || snaps[0].Commands[1] != "b" {
		t.Fatalf("commands not sorted: %+v", snaps[0].Commands)
	}
	if snaps[0].Hooks["pre"][0] != "a" {
		t.Fatalf("hook commands not normalized: %+v", snaps[0].Hooks)
	}
}

func TestNormalizePluginHooksDropsEmpty(t *testing.T) {
	if hooks := normalizePluginHooks(map[string][]string{"": {"a"}, "k": {}}); hooks != nil {
		t.Fatalf("expected nil hooks, got %+v", hooks)
	}
}

func TestEnabledPluginsFiltering(t *testing.T) {
	settings := &config.Settings{EnabledPlugins: map[string]bool{"keep": true, "drop": false}}
	enabled := enabledPluginMap(settings)
	pluginsIn := []*plugins.ClaudePlugin{{Name: "keep"}, {Name: "drop"}}
	filtered := plugins.FilterEnabledPlugins(pluginsIn, enabled)
	if len(filtered) != 1 || filtered[0].Name != "keep" {
		t.Fatalf("unexpected filter result: %+v", filtered)
	}
}

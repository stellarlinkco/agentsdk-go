package api

import (
	"sort"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/config"
	"github.com/cexll/agentsdk-go/pkg/plugins"
)

// discoverPlugins scans the project root for .claude-plugin manifests and
// filters them according to settings.EnabledPlugins.
func discoverPlugins(projectRoot string, settings *config.Settings) ([]*plugins.ClaudePlugin, error) {
	pluginsInProject, err := plugins.ScanPluginsInProject(projectRoot)
	if err != nil {
		return nil, err
	}
	return plugins.FilterEnabledPlugins(pluginsInProject, enabledPluginMap(settings)), nil
}

func enabledPluginMap(settings *config.Settings) map[string]bool {
	if settings == nil || len(settings.EnabledPlugins) == 0 {
		return nil
	}
	out := make(map[string]bool, len(settings.EnabledPlugins))
	for name, enabled := range settings.EnabledPlugins {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out[trimmed] = enabled
	}
	return out
}

// snapshotPlugins converts loaded plugins into a response-friendly shape.
func snapshotPlugins(plugs []*plugins.ClaudePlugin) []PluginSnapshot {
	if len(plugs) == 0 {
		return nil
	}
	out := make([]PluginSnapshot, 0, len(plugs))
	for _, plug := range plugs {
		if plug == nil {
			continue
		}
		snap := PluginSnapshot{
			Name:        plug.Name,
			Version:     plug.Version,
			Description: plug.Description,
			Commands:    sortedCopy(plug.Commands),
			Agents:      sortedCopy(plug.Agents),
			Skills:      sortedCopy(plug.Skills),
			Hooks:       normalizePluginHooks(plug.Hooks),
		}
		out = append(out, snap)
	}
	return out
}

func normalizePluginHooks(hooks map[string][]string) map[string][]string {
	if len(hooks) == 0 {
		return nil
	}
	out := make(map[string][]string, len(hooks))
	for k, v := range hooks {
		trimmed := strings.TrimSpace(k)
		if trimmed == "" || len(v) == 0 {
			continue
		}
		out[trimmed] = sortedCopy(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cp := append([]string(nil), values...)
	sort.Strings(cp)
	return cp
}

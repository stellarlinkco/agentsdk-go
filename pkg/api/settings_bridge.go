package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/config"
)

// loadSettings resolves settings.json using the new layered SettingsLoader and
// applies an optional explicit settings path on top. Runtime overrides from
// api.Options always win.
func loadSettings(opts Options) (*config.Settings, error) {
	loader := opts.SettingsLoader
	if loader == nil {
		loader = &config.SettingsLoader{ProjectRoot: opts.ProjectRoot}
	} else {
		clone := *loader // avoid mutating caller-provided loader
		loader = &clone
	}

	if opts.SettingsOverrides != nil {
		loader.RuntimeOverrides = config.MergeSettings(loader.RuntimeOverrides, opts.SettingsOverrides)
	}

	settings, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("api: load settings: %w", err)
	}
	if settings == nil {
		return nil, errors.New("api: settings loader returned nil")
	}

	if path := strings.TrimSpace(opts.SettingsPath); path != "" {
		overlay, err := loadSettingsFile(path)
		if err != nil {
			return nil, err
		}
		if overlay != nil {
			if merged := config.MergeSettings(settings, overlay); merged != nil {
				settings = merged
			}
		}
	}

	if opts.SettingsOverrides != nil {
		if merged := config.MergeSettings(settings, opts.SettingsOverrides); merged != nil {
			settings = merged
		}
	}

	if settings.Env == nil {
		settings.Env = map[string]string{}
	}
	return settings, nil
}

// loadSettingsFile decodes a single settings.json file. Missing files are
// treated as errors because an explicit path signals caller intent.
func loadSettingsFile(path string) (*config.Settings, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("api: read settings file %s: %w", path, err)
	}
	var settings config.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("api: decode settings %s: %w", path, err)
	}
	return &settings, nil
}

// projectConfigFromSettings preserves the legacy Config() surface by
// returning a defensive snapshot of the merged settings. It ensures callers
// never observe a nil config even if settings loading failed earlier.
func projectConfigFromSettings(settings *config.Settings) *config.Settings {
	snapshot := config.MergeSettings(nil, settings)
	if snapshot == nil {
		snapshot = &config.Settings{}
	}
	if snapshot.Env == nil {
		snapshot.Env = map[string]string{}
	}
	if snapshot.Permissions == nil {
		snapshot.Permissions = &config.PermissionsConfig{}
	}
	return snapshot
}

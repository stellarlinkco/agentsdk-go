package config

import (
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var filepathAbs = filepath.Abs

// SettingsLoader composes settings using the simplified precedence model.
// Higher-priority layers override lower ones while preserving unspecified fields.
// Order (low -> high): defaults < project < local < runtime overrides.
type SettingsLoader struct {
	ProjectRoot      string
	RuntimeOverrides *Settings
	FS               *FS
}

// Load resolves and merges settings across all layers.
func (l *SettingsLoader) Load() (*Settings, error) {
	if strings.TrimSpace(l.ProjectRoot) == "" {
		return nil, errors.New("project root is required for settings loading")
	}

	root := l.ProjectRoot
	abs, err := filepathAbs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	root = abs

	merged := GetDefaultSettings()

	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		var homeErr error
		home, homeErr = os.UserHomeDir()
		if homeErr != nil {
			home = ""
		}
	}

	layers := []struct {
		name  string
		paths []string
	}{
		{
			name:  "global",
			paths: []string{getGlobalSettingsPath(home)},
		},
		{
			name:  "global-local",
			paths: []string{getGlobalLocalSettingsPath(home)},
		},
		{
			name:  "project",
			paths: []string{getProjectSettingsPath(root)},
		},
		{
			name:  "local",
			paths: []string{getLocalSettingsPath(root)},
		},
	}

	for _, layer := range layers {
		if err := applySettingsLayerCandidates(&merged, layer.name, layer.paths, l.FS); err != nil {
			return nil, err
		}
	}

	if l.RuntimeOverrides != nil {
		log.Printf("settings: applying runtime overrides")
		if next := MergeSettings(&merged, l.RuntimeOverrides); next != nil {
			merged = *next
		}
	}

	return &merged, nil
}

// getProjectSettingsPath returns the tracked project settings path.
func getProjectSettingsPath(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return filepath.Join(root, ".agents", "settings.json")
}

// getLocalSettingsPath returns the untracked project-local settings path.
func getLocalSettingsPath(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return filepath.Join(root, ".agents", "settings.local.json")
}

func getGlobalSettingsPath(home string) string {
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".agents", "settings.json")
}

func getGlobalLocalSettingsPath(home string) string {
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".agents", "settings.local.json")
}

// loadJSONFile decodes a settings JSON file. Missing files return (nil, nil).
func loadJSONFile(path string, filesystem *FS) (*Settings, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	var (
		data []byte
		err  error
	)
	if filesystem != nil {
		data, err = filesystem.ReadFile(path)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &s, nil
}

func applySettingsLayerCandidates(dst *Settings, name string, paths []string, filesystem *FS) error {
	nonEmpty := 0
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		nonEmpty++
		cfg, err := loadJSONFile(path, filesystem)
		if err != nil {
			return fmt.Errorf("load %s settings: %w", name, err)
		}
		if cfg == nil {
			continue
		}

		log.Printf("settings: applying %s layer from %s", name, path)
		if next := MergeSettings(dst, cfg); next != nil {
			*dst = *next
		}
		return nil
	}

	if nonEmpty == 0 {
		log.Printf("settings: %s layer skipped (no path)", name)
		return nil
	}

	log.Printf("settings: %s layer not found", name)
	return nil
}

// applySettingsLayer is kept for internal tests that target edge branches.
func applySettingsLayer(dst *Settings, name, path string, filesystem *FS) error {
	return applySettingsLayerCandidates(dst, name, []string{path}, filesystem)
}

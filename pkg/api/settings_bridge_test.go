package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
)

func TestLoadSettingsMergesOverridesAndInitialisesEnv(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	overlayPath := filepath.Join(root, "custom_settings.json")
	if err := os.WriteFile(overlayPath, []byte(`{"env":{"A":"B"},"permissions":{"additionalDirectories":["/tmp/data"]}}`), 0o600); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	overrideEnv := map[string]string{"C": "D"}
	override := &config.Settings{Model: "override-model", Env: overrideEnv}
	opts := Options{ProjectRoot: root, SettingsPath: overlayPath, SettingsOverrides: override}

	settings, err := loadSettings(opts)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if settings.Env["A"] != "B" || settings.Env["C"] != "D" {
		t.Fatalf("env merge failed: %+v", settings.Env)
	}
	if settings.Model != "override-model" {
		t.Fatalf("override model lost, got %s", settings.Model)
	}
	if settings.Permissions == nil || len(settings.Permissions.AdditionalDirectories) != 1 {
		t.Fatalf("permissions mapping missing: %+v", settings.Permissions)
	}
}

func TestLoadSettingsUsesDefaultsWhenProjectConfigMissing(t *testing.T) {
	root := t.TempDir()

	settings, err := loadSettings(Options{ProjectRoot: root})
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if settings == nil {
		t.Fatal("expected defaults, got nil settings")
	}
	if settings.CleanupPeriodDays == nil || *settings.CleanupPeriodDays != 30 {
		got := 0
		if settings.CleanupPeriodDays != nil {
			got = *settings.CleanupPeriodDays
		}
		t.Fatalf("expected default cleanup period 30, got %d", got)
	}
	if settings.Permissions == nil || settings.Permissions.DefaultMode != "askBeforeRunningTools" {
		t.Fatalf("default permissions not applied: %+v", settings.Permissions)
	}
	if settings.Sandbox == nil || settings.Sandbox.Enabled == nil || *settings.Sandbox.Enabled {
		t.Fatalf("expected sandbox disabled by default, got %+v", settings.Sandbox)
	}
	if settings.Env == nil || len(settings.Env) != 0 {
		t.Fatalf("expected empty env map, got %+v", settings.Env)
	}
}

func TestLoadSettingsClonesCustomLoader(t *testing.T) {
	root := t.TempDir()
	originalOverrides := &config.Settings{Model: "custom"}
	custom := &config.SettingsLoader{ProjectRoot: root, RuntimeOverrides: originalOverrides}

	settings, err := loadSettings(Options{
		ProjectRoot:       root,
		SettingsLoader:    custom,
		SettingsOverrides: &config.Settings{Model: "override"},
	})
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if settings.Model != "override" {
		t.Fatalf("expected override model, got %s", settings.Model)
	}
	if custom.RuntimeOverrides != originalOverrides {
		t.Fatalf("expected caller-provided loader to remain unchanged")
	}
}

func TestProjectConfigFromSettingsNilInput(t *testing.T) {
	cfg := projectConfigFromSettings(nil)
	if cfg == nil {
		t.Fatal("expected defensive config")
	}
	if cfg.Env == nil || cfg.Permissions == nil {
		t.Fatalf("expected defaulted fields, got env=%+v perms=%+v", cfg.Env, cfg.Permissions)
	}
}

func TestLoadSettingsFileIgnoresEmptyPath(t *testing.T) {
	settings, err := loadSettingsFile("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil settings for empty path, got %+v", settings)
	}
}

func TestLoadSettingsFileMissingPathErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := loadSettingsFile(path); err == nil {
		t.Fatal("expected error for missing explicit path")
	}
}

func TestLoadSettingsErrorsOnMissingExplicitOverlay(t *testing.T) {
	root := t.TempDir()
	opts := Options{ProjectRoot: root, SettingsPath: filepath.Join(root, "absent.json")}
	if _, err := loadSettings(opts); err == nil {
		t.Fatal("expected loadSettings to fail for missing overlay")
	}
}

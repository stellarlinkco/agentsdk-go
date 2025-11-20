package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/config"
)

func TestLoadSettingsMergesOverridesAndInitialisesEnv(t *testing.T) {
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude: %v", err)
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

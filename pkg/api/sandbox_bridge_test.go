package api

import (
	"path/filepath"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/config"
)

func TestAdditionalSandboxPathsHandlesNilAndDedup(t *testing.T) {
	if extra := additionalSandboxPaths(nil); extra != nil {
		t.Fatalf("expected nil extras for nil settings, got %+v", extra)
	}

	settings := &config.Settings{Permissions: &config.PermissionsConfig{AdditionalDirectories: []string{" /tmp ", "/tmp"}}}
	extras := additionalSandboxPaths(settings)
	if len(extras) != 1 || filepath.Clean(extras[0]) != "/tmp" {
		t.Fatalf("expected deduped absolute path, got %+v", extras)
	}
}

func TestBuildSandboxManagerAppliesDefaultNetworkAllow(t *testing.T) {
	root := t.TempDir()
	opts := Options{ProjectRoot: root}
	mgr, sbRoot := buildSandboxManager(opts, nil)
	if want, err := filepath.EvalSymlinks(root); err != nil {
		t.Fatalf("eval symlink: %v", err)
	} else if want != "" && sbRoot != want {
		t.Fatalf("unexpected sandbox root %s, want %s", sbRoot, want)
	}
	if err := mgr.CheckNetwork("localhost"); err != nil {
		t.Fatalf("expected localhost allowed by default: %v", err)
	}
	if err := mgr.CheckNetwork("example.com"); err == nil {
		t.Fatal("expected example.com to be denied by default allow list")
	}
}

func TestAdditionalSandboxPathsSkipsInvalidEntries(t *testing.T) {
	settings := &config.Settings{Permissions: &config.PermissionsConfig{AdditionalDirectories: []string{"", "../relative"}}}
	extras := additionalSandboxPaths(settings)
	if len(extras) != 1 || !filepath.IsAbs(extras[0]) {
		t.Fatalf("expected relative path to be resolved absolute, got %+v", extras)
	}
}

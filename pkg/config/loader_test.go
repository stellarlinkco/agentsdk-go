package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/plugins"
	"github.com/stretchr/testify/require"
)

func TestLoaderResolvesClaudeDirAndPlugins(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	require.NoError(t, os.MkdirAll(claude, 0o755))

	ts, signer := makeTrustStore(t)
	manifestPath := writePlugin(t, claude, "hello", signer)

	cfgBody := []byte(`version: "1.0.0"
plugins:
  - name: hello
    path: plugins/hello
`)
	require.NoError(t, os.WriteFile(filepath.Join(claude, "config.yaml"), cfgBody, 0o644))

	loader, err := NewLoader(root, WithTrustStore(ts))
	require.NoError(t, err)

	cfg, err := loader.Load()
	require.NoError(t, err)
	require.Equal(t, "1.0.0", cfg.Version)
	require.Equal(t, claude, cfg.ClaudeDir)
	require.Len(t, cfg.Manifests, 1)
	require.Equal(t, manifestPath, cfg.Manifests[0].ManifestPath)
	require.NotEmpty(t, cfg.SourceHash)
}

func TestLoaderAutoDiscoversPluginsWhenListMissing(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	ts, signer := makeTrustStore(t)
	writePlugin(t, claude, "auto", signer)

	cfgBody := []byte(`version: 2.1.3`)
	require.NoError(t, os.WriteFile(filepath.Join(claude, "config.yaml"), cfgBody, 0o644))

	loader, err := NewLoader(root, WithTrustStore(ts))
	require.NoError(t, err)

	cfg, err := loader.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Plugins, 1, "auto discovery should synthesise plugin refs")
	require.Equal(t, "auto", cfg.Plugins[0].Name)
	require.Len(t, cfg.Manifests, 1)
}

func TestLoaderRollbackOnInvalidReload(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	ts, signer := makeTrustStore(t)
	writePlugin(t, claude, "bad", signer)

	cfgPath := filepath.Join(claude, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: 0.1.0
plugins:
  - name: bad
`), 0o644))

	loader, err := NewLoader(root, WithTrustStore(ts))
	require.NoError(t, err)
	cfg, err := loader.Load()
	require.NoError(t, err)
	require.Equal(t, "0.1.0", cfg.Version)

	require.NoError(t, os.WriteFile(cfgPath, []byte(""), 0o644))
	_, err = loader.Load()
	require.Error(t, err)
	last, ok := loader.Last()
	require.True(t, ok)
	require.Equal(t, "0.1.0", last.Version)
}

func TestValidatorRejectsEvilPaths(t *testing.T) {
	v := NewDefaultValidator("/tmp/project")
	cfg := &ProjectConfig{
		Version: "1.0.0",
		Plugins: []PluginRef{{Name: "evil", Path: "../escape"}},
		Sandbox: SandboxBlock{AllowedPaths: []string{"../etc"}},
		Manifests: []*plugins.Manifest{
			{Name: "evil", Version: "1.0.0", EntrypointAbs: "/tmp/project/.claude/plugins/evil/main"},
		},
	}
	err := v.Validate(cfg)
	require.Error(t, err)
}

package config

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/plugins"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func makeTrustStore(t *testing.T) (*plugins.TrustStore, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	ts := plugins.NewTrustStore()
	ts.Register("dev", pub)
	ts.AllowUnsigned(false)
	return ts, priv
}

func writePlugin(t *testing.T, claudeDir, name string, signer ed25519.PrivateKey) string {
	t.Helper()
	pluginDir := filepath.Join(claudeDir, "plugins", name)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	entryPath := filepath.Join(pluginDir, "main.js")
	require.NoError(t, os.WriteFile(entryPath, []byte("console.log('hi')"), 0o644))
	digest := sha256.Sum256([]byte("console.log('hi')"))

	mf := &plugins.Manifest{
		Name:       name,
		Version:    "1.2.3",
		Entrypoint: "main.js",
		Digest:     hex.EncodeToString(digest[:]),
		Signer:     "dev",
		Metadata:   map[string]string{"purpose": "test"},
	}
	sig, err := plugins.SignManifest(mf, signer)
	require.NoError(t, err)
	mf.Signature = sig

	manifestBytes, err := yaml.Marshal(mf)
	require.NoError(t, err)
	manifestPath := filepath.Join(pluginDir, "manifest.yaml")
	require.NoError(t, os.WriteFile(manifestPath, manifestBytes, 0o644))
	return manifestPath
}

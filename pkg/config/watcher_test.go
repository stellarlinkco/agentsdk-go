package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWatcherHotReload(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	ts, signer := makeTrustStore(t)
	writePlugin(t, claude, "watch", signer)

	cfgPath := filepath.Join(claude, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: 1.0.0
plugins:
  - name: watch
`), 0o644))

	loader, err := NewLoader(root, WithTrustStore(ts))
	require.NoError(t, err)

	_, err = loader.Load()
	require.NoError(t, err)

	changes := make(chan string, 4)
	watcher, err := NewWatcher(loader, WithDebounce(10*time.Millisecond), OnChange(func(cfg *ProjectConfig) {
		changes <- cfg.Version
	}))
	require.NoError(t, err)
	defer watcher.Close()

	_, err = watcher.Start()
	require.NoError(t, err)
	require.Equal(t, "1.0.0", <-changes)

	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: 1.1.0
plugins:
  - name: watch
`), 0o644))

	select {
	case version := <-changes:
		require.Equal(t, "1.1.0", version)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watcher reload")
	}
}

func TestWatcherEmitsErrorsOnInvalidConfig(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	ts, signer := makeTrustStore(t)
	writePlugin(t, claude, "bad", signer)

	cfgPath := filepath.Join(claude, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: 1.0.0
plugins:
  - name: bad
`), 0o644))

	loader, err := NewLoader(root, WithTrustStore(ts))
	require.NoError(t, err)

	errs := make(chan error, 2)
	watcher, err := NewWatcher(loader, OnError(func(err error) { errs <- err }))
	require.NoError(t, err)
	defer watcher.Close()

	_, err = watcher.Start()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: not-a-semver`), 0o644))

	select {
	case err := <-errs:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("expected watcher to emit validation error")
	}
}

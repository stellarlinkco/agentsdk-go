package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
)

func TestRulesLoader_LoadRulesErrorBranches(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics vary on windows")
	}

	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents")
	rulesDir := filepath.Join(agentsDir, "rules")
	require.NoError(t, os.MkdirAll(rulesDir, 0o755))

	loader := NewRulesLoader(root)

	// os.ReadDir error (permission denied).
	require.NoError(t, os.Chmod(rulesDir, 0o000))
	_, err := loader.LoadRules()
	require.Error(t, err)
	require.NoError(t, os.Chmod(rulesDir, 0o755))

	// os.ReadFile error (permission denied).
	md := filepath.Join(rulesDir, "01-x.md")
	require.NoError(t, os.WriteFile(md, []byte("x"), 0o000))
	_, err = loader.LoadRules()
	require.Error(t, err)
	require.NoError(t, os.Chmod(md, 0o600))
}

func TestRulesLoader_WatchChangesEventFilteringAndErrorChannel(t *testing.T) {
	t.Parallel()

	root, dir := makeRulesRoot(t)
	loader := NewRulesLoader(root)
	_, err := loader.LoadRules()
	require.NoError(t, err)

	updates := make(chan []Rule, 4)
	require.NoError(t, loader.WatchChanges(func(r []Rule) {
		select {
		case updates <- r:
		default:
		}
	}))
	require.NotNil(t, loader.watcher)

	// Non-blocking watcher error path.
	loader.watcher.Errors <- errors.New("watcher boom")

	// Chmod event should be ignored (op filter branch).
	loader.watcher.Events <- fsnotify.Event{Name: filepath.Join(dir, "ignored.md"), Op: fsnotify.Chmod}

	// Non-md should be ignored (ext filter branch).
	loader.watcher.Events <- fsnotify.Event{Name: filepath.Join(dir, "note.txt"), Op: fsnotify.Create}

	// md event that triggers reload error branch.
	bad := writeRuleFile(t, dir, "02-bad.md", "nope")
	require.NoError(t, os.Chmod(bad, 0o000))
	loader.watcher.Events <- fsnotify.Event{Name: bad, Op: fsnotify.Write}

	// Fix and send another md event to exercise happy callback path.
	require.NoError(t, os.Chmod(bad, 0o600))
	require.NoError(t, os.WriteFile(bad, []byte("ok"), 0o600))
	loader.watcher.Events <- fsnotify.Event{Name: bad, Op: fsnotify.Write}

	select {
	case got := <-updates:
		require.NotEmpty(t, got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for rules reload")
	}

	require.NoError(t, loader.Close())
}

func TestRulesLoader_WatchChangesStatError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics vary on windows")
	}

	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.Chmod(agentsDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(agentsDir, 0o755) })

	loader := NewRulesLoader(root)
	err := loader.WatchChanges(nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, os.ErrPermission) || !errors.Is(err, os.ErrNotExist))
}

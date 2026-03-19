package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestRulesLoaderWatchChanges_LogsLoadErrorsAndStopsOnClosedErrorChannel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits and fsnotify semantics vary on windows")
	}

	root := t.TempDir()
	rulesDir := filepath.Join(root, ".agents", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("mkdir rules: %v", err)
	}

	// Unreadable .md file forces LoadRules() to return an error when watcher fires.
	bad := filepath.Join(rulesDir, "01-bad.md")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatalf("write bad rule: %v", err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatalf("chmod bad rule: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(bad, 0o600); err != nil {
			t.Errorf("chmod cleanup: %v", err)
		}
	})

	loader := NewRulesLoader(root)

	var watcher *fsnotify.Watcher
	newWatcherFn := func() (*fsnotify.Watcher, error) {
		watcher = &fsnotify.Watcher{
			Events: make(chan fsnotify.Event, 1),
			Errors: make(chan error, 1),
		}
		return watcher, nil
	}

	if err := loader.watchChangesWith(nil, os.Stat, newWatcherFn, func(*fsnotify.Watcher, string) error { return nil }); err != nil {
		t.Fatalf("watchChangesWith: %v", err)
	}

	watcher.Events <- fsnotify.Event{Name: bad, Op: fsnotify.Write}
	time.Sleep(25 * time.Millisecond)

	close(watcher.Errors)
	time.Sleep(25 * time.Millisecond)
}

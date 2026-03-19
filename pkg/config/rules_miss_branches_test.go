package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

type fakeRulesFileInfo struct {
	isDir bool
}

func (f fakeRulesFileInfo) Name() string       { return "fake" }
func (f fakeRulesFileInfo) Size() int64        { return 0 }
func (f fakeRulesFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeRulesFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeRulesFileInfo) IsDir() bool        { return f.isDir }
func (f fakeRulesFileInfo) Sys() any           { return nil }

func TestRulesLoader_LoadRules_StatAndReadDirBranches(t *testing.T) {
	t.Parallel()

	loader := NewRulesLoader("root")

	_, err := loader.loadRulesWith(
		func(string) (os.FileInfo, error) { return nil, fs.ErrPermission },
		func(string) ([]os.DirEntry, error) { return nil, nil },
		func(string) ([]byte, error) { return nil, nil },
	)
	if err == nil {
		t.Fatalf("expected stat permission error")
	}

	rules, err := loader.loadRulesWith(
		func(string) (os.FileInfo, error) { return fakeRulesFileInfo{isDir: true}, nil },
		func(string) ([]os.DirEntry, error) { return nil, fs.ErrNotExist },
		func(string) ([]byte, error) { return nil, nil },
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rules != nil {
		t.Fatalf("expected nil rules on missing directory race, got %v", rules)
	}
	if got := loader.GetContent(); got != "" {
		t.Fatalf("expected empty content after missing dir race, got %q", got)
	}
}

func TestRulesLoader_WatchChanges_NewWatcherAndAddErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	loader := NewRulesLoader(root)

	rulesDir := filepath.Join(root, ".agents", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("mkdir rules dir: %v", err)
	}

	if err := loader.watchChangesWith(
		nil,
		os.Stat,
		func() (*fsnotify.Watcher, error) { return nil, errors.New("new watcher failed") },
		func(*fsnotify.Watcher, string) error { return nil },
	); err == nil {
		t.Fatalf("expected new watcher error")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer watcher.Close()

	if err := loader.watchChangesWith(
		nil,
		os.Stat,
		func() (*fsnotify.Watcher, error) { return watcher, nil },
		func(*fsnotify.Watcher, string) error { return errors.New("add failed") },
	); err == nil {
		t.Fatalf("expected add error")
	}
}

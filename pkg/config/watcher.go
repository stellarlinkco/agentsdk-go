package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher listens for config changes under .claude and hot-reloads safely.
type Watcher struct {
	loader   *Loader
	debounce time.Duration

	fsw *fsnotify.Watcher

	stop chan struct{}
	done chan struct{}

	mu       sync.Mutex
	watched  map[string]struct{}
	lastHash string

	onChange func(*ProjectConfig)
	onError  func(error)
}

// WatcherOption configures the hot reloader.
type WatcherOption func(*Watcher)

// WithDebounce overrides the default debounce window.
func WithDebounce(d time.Duration) WatcherOption {
	return func(w *Watcher) { w.debounce = d }
}

// OnChange registers a callback fired after successful reload.
func OnChange(fn func(*ProjectConfig)) WatcherOption {
	return func(w *Watcher) { w.onChange = fn }
}

// OnError registers a callback for reload failures.
func OnError(fn func(error)) WatcherOption {
	return func(w *Watcher) { w.onError = fn }
}

// NewWatcher wires a file watcher around the provided loader.
func NewWatcher(loader *Loader, opts ...WatcherOption) (*Watcher, error) {
	if loader == nil {
		return nil, errors.New("loader is nil")
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}
	w := &Watcher{
		loader:   loader,
		debounce: 150 * time.Millisecond,
		fsw:      fsw,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		watched:  map[string]struct{}{},
	}
	for _, opt := range opts {
		opt(w)
	}
	if w.debounce <= 0 {
		w.debounce = 150 * time.Millisecond
	}
	return w, nil
}

// Start loads the initial config and begins watching .claude.
func (w *Watcher) Start() (*ProjectConfig, error) {
	cfg, err := w.loader.Load()
	if err != nil {
		return nil, err
	}
	if err := w.refreshTargets(cfg); err != nil {
		return nil, err
	}
	w.lastHash = cfg.SourceHash
	if w.onChange != nil {
		w.onChange(cfg)
	}
	go w.loop()
	return cfg, nil
}

// Close stops file watching.
func (w *Watcher) Close() error {
	close(w.stop)
	<-w.done
	return w.fsw.Close()
}

func (w *Watcher) refreshTargets(cfg *ProjectConfig) error {
	desired := map[string]struct{}{w.loader.Root(): {}}
	if cfg != nil && cfg.ClaudeDir != "" {
		desired[cfg.ClaudeDir] = struct{}{}
		desired[filepath.Join(cfg.ClaudeDir, pluginsDirName)] = struct{}{}
		for _, mf := range cfg.Manifests {
			if mf != nil {
				desired[mf.PluginDir] = struct{}{}
			}
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for path := range desired {
		if _, ok := w.watched[path]; ok {
			continue
		}
		if err := w.addWatch(path); err != nil {
			return err
		}
	}
	for path := range w.watched {
		if _, ok := desired[path]; !ok {
			_ = w.fsw.Remove(path)
			delete(w.watched, path)
		}
	}
	return nil
}

func (w *Watcher) addWatch(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := w.fsw.Add(path); err != nil {
		return err
	}
	w.watched[path] = struct{}{}
	return nil
}

func (w *Watcher) loop() {
	defer close(w.done)
	var timer *time.Timer
	schedule := func() {
		if timer == nil {
			timer = time.AfterFunc(w.debounce, func() {
				w.reload()
			})
			return
		}
		timer.Reset(w.debounce)
	}

	for {
		select {
		case <-w.stop:
			if timer != nil {
				timer.Stop()
			}
			return
		case err := <-w.fsw.Errors:
			if err != nil && w.onError != nil {
				w.onError(err)
			}
		case evt := <-w.fsw.Events:
			if evt.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				schedule()
			}
		}
	}
}

func (w *Watcher) reload() {
	cfg, err := w.loader.Reload()
	if err != nil {
		if w.onError != nil {
			w.onError(err)
		}
		return
	}
	if cfg.SourceHash == w.lastHash {
		return
	}
	if err := w.refreshTargets(cfg); err != nil {
		if w.onError != nil {
			w.onError(err)
		}
		return
	}
	w.lastHash = cfg.SourceHash
	if w.onChange != nil {
		w.onChange(cfg)
	}
}

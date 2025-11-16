package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNoBackendRoute reports attempts to read/write paths without a registered backend.
var ErrNoBackendRoute = errors.New("session: no backend route for path")

// Backend defines the minimal persistence operations required by sessions.
type Backend interface {
	Read(path string) ([]byte, error)
	Write(path string, data []byte) error
	List(prefix string) ([]string, error)
	Delete(path string) error
}

// CompositeBackend routes operations to backends based on the longest matching prefix.
type CompositeBackend struct {
	mu     sync.RWMutex
	routes map[string]Backend
}

// NewCompositeBackend creates an empty CompositeBackend.
func NewCompositeBackend() *CompositeBackend {
	return &CompositeBackend{
		routes: make(map[string]Backend),
	}
}

// AddRoute registers a backend for the specified prefix, overwriting existing entries.
func (b *CompositeBackend) AddRoute(prefix string, backend Backend) error {
	if backend == nil {
		return errors.New("session: backend cannot be nil")
	}
	norm := normalizePrefix(prefix)
	if norm == "" {
		return errors.New("session: prefix cannot be empty")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.routes[norm] = backend
	return nil
}

// Route returns the backend registered for the provided path (longest prefix match).
func (b *CompositeBackend) Route(p string) Backend {
	if b == nil {
		return nil
	}
	path := normalizePath(p)

	b.mu.RLock()
	defer b.mu.RUnlock()

	var matched Backend
	var maxLen int
	for prefix, backend := range b.routes {
		if strings.HasPrefix(path, prefix) && len(prefix) > maxLen {
			matched = backend
			maxLen = len(prefix)
		}
	}
	return matched
}

// Read resolves the backend for path and delegates the read.
func (b *CompositeBackend) Read(path string) ([]byte, error) {
	backend, err := b.routeOrErr(path)
	if err != nil {
		return nil, err
	}
	return backend.Read(normalizePath(path))
}

// Write resolves the backend for path and delegates the write.
func (b *CompositeBackend) Write(path string, data []byte) error {
	backend, err := b.routeOrErr(path)
	if err != nil {
		return err
	}
	return backend.Write(normalizePath(path), data)
}

// List resolves the backend for prefix and delegates the list operation.
func (b *CompositeBackend) List(prefix string) ([]string, error) {
	backend, err := b.routeOrErr(prefix)
	if err != nil {
		return nil, err
	}
	return backend.List(normalizePath(prefix))
}

// Delete resolves the backend for path and delegates the delete.
func (b *CompositeBackend) Delete(path string) error {
	backend, err := b.routeOrErr(path)
	if err != nil {
		return err
	}
	return backend.Delete(normalizePath(path))
}

func (b *CompositeBackend) routeOrErr(path string) (Backend, error) {
	backend := b.Route(path)
	if backend == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoBackendRoute, normalizePath(path))
	}
	return backend, nil
}

// FileBackend stores session data on the local filesystem under root.
type FileBackend struct {
	root     string
	fileMode os.FileMode
}

// NewFileBackend creates a filesystem-backed Backend.
func NewFileBackend(root string) (*FileBackend, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("session: backend root required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &FileBackend{
		root:     abs,
		fileMode: 0o600,
	}, nil
}

// Read loads file contents from disk.
func (f *FileBackend) Read(p string) ([]byte, error) {
	full, err := f.fullPath(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// Write persists bytes to disk, creating parent directories as needed.
func (f *FileBackend) Write(p string, data []byte) error {
	full, err := f.fullPath(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, f.fileMode)
}

// List enumerates files contained within prefix.
func (f *FileBackend) List(prefix string) ([]string, error) {
	full, err := f.fullPath(prefix)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{normalizePath(prefix)}, nil
	}
	var paths []string
	err = filepath.WalkDir(full, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(f.root, path)
		if err != nil {
			return err
		}
		paths = append(paths, normalizePath("/"+filepath.ToSlash(rel)))
		return nil
	})
	return paths, err
}

// Delete removes the file or directory at the provided path.
func (f *FileBackend) Delete(p string) error {
	full, err := f.fullPath(p)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (f *FileBackend) fullPath(p string) (string, error) {
	norm := strings.TrimPrefix(normalizePath(p), "/")
	full := filepath.Join(f.root, filepath.FromSlash(norm))
	full = filepath.Clean(full)
	if !strings.HasPrefix(full, f.root) {
		return "", fmt.Errorf("session: path %s escapes backend root", p)
	}
	return full, nil
}

func normalizePrefix(prefix string) string {
	p := normalizePath(prefix)
	if p == "/" {
		return p
	}
	if !strings.HasSuffix(p, "/") {
		return p
	}
	// normalizePath already removes trailing slashes; just return.
	return p
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	clean := path.Clean(p)
	if clean == "." {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}

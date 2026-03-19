package api

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	embeddedAgentHooksDir = ".agents/hooks"
)

func materializeEmbeddedClaudeHooks(projectRoot string, embedFS fs.FS) error {
	if embedFS == nil {
		return nil
	}

	root := strings.TrimSpace(projectRoot)
	if root == "" {
		root = "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}

	return materializeEmbeddedHooksDir(root, embedFS, embeddedAgentHooksDir)
}

func materializeEmbeddedHooksDir(projectRoot string, embedFS fs.FS, embedDir string) error {
	info, err := fs.Stat(embedFS, embedDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat embedded %s: %w", embedDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("embedded %s is not a directory", embedDir)
	}

	return fs.WalkDir(embedFS, embedDir, func(embedPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel := path.Clean(embedPath)
		prefix := embedDir + "/"
		if rel != embedDir && !strings.HasPrefix(rel, prefix) {
			return nil
		}

		dest := filepath.Join(projectRoot, filepath.FromSlash(rel))
		if _, err := os.Stat(dest); err == nil {
			return nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", dest, err)
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}

		data, err := fs.ReadFile(embedFS, embedPath)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", embedPath, err)
		}

		tmp := dest + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, dest); err != nil {
			_ = os.Remove(tmp)
			if _, statErr := os.Stat(dest); statErr == nil {
				return nil
			}
			return fmt.Errorf("rename %s: %w", dest, err)
		}

		return nil
	})
}

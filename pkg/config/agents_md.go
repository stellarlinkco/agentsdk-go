package config

import (
	"bytes"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

const (
	agentsMDFileName = "AGENTS.md"

	includedMDMaxDepth     = 8
	includedMDMaxFileBytes = 1 << 20 // 1 MiB
	includedMDMaxTotal     = 4 << 20 // 4 MiB
)

// LoadAgentsMD loads ./AGENTS.md and expands @include directives.
//
// AGENTS.md supports including additional files by placing "@path/to/file"
// lines inside the file. This loader replaces those lines with the referenced
// file content (recursively), skipping include directives inside code blocks.
//
// Missing AGENTS.md returns ("", nil).
func LoadAgentsMD(projectRoot string, filesystem *FS) (string, error) {
	return loadIncludedMD(projectRoot, filesystem, agentsMDFileName, "agents.md")
}

func loadIncludedMD(projectRoot string, filesystem *FS, filename, label string) (string, error) {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		root = "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}

	loader := includedMDLoader{
		root:      root,
		fs:        filesystem,
		visited:   map[string]struct{}{},
		isWindows: runtime.GOOS == "windows",
		label:     strings.TrimSpace(label),
	}
	content, err := loader.load(filepath.Join(root, strings.TrimSpace(filename)), 0)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

type includedMDLoader struct {
	root      string
	fs        *FS
	visited   map[string]struct{}
	total     int64
	isWindows bool
	label     string
}

func (l *includedMDLoader) load(path string, depth int) (string, error) {
	label := strings.TrimSpace(l.label)
	if label == "" {
		label = "included.md"
	}
	if depth > includedMDMaxDepth {
		return "", fmt.Errorf("%s: include depth exceeds %d", label, includedMDMaxDepth)
	}

	absPath := strings.TrimSpace(path)
	if absPath == "" {
		return "", nil
	}
	if !filepath.IsAbs(absPath) && !isWindowsAbs(absPath) {
		absPath = filepath.Join(l.root, absPath)
	}
	absPath = filepath.Clean(absPath)
	if abs, err := filepath.Abs(absPath); err == nil {
		absPath = abs
	}

	if strings.TrimSpace(l.root) != "" {
		root := filepath.Clean(l.root)
		prefix := root
		if sep := string(filepath.Separator); root != sep && !strings.HasSuffix(root, sep) {
			prefix = root + sep
		}
		if absPath != root && !strings.HasPrefix(absPath, prefix) {
			return "", fmt.Errorf("%s: include path escapes project root: %s", label, path)
		}
	}

	visitKey := absPath
	if l.isWindows {
		visitKey = strings.ToLower(visitKey)
	}
	if _, ok := l.visited[visitKey]; ok {
		return "", nil
	}
	l.visited[visitKey] = struct{}{}

	data, err := readFileLimited(l.fs, absPath, includedMDMaxFileBytes, label)
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) && depth == 0 {
			return "", nil
		}
		return "", err
	}
	if l.total+int64(len(data)) > includedMDMaxTotal {
		return "", fmt.Errorf("%s: total included content exceeds %d bytes limit", label, includedMDMaxTotal)
	}
	l.total += int64(len(data))

	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s: %s appears to be binary", label, absPath)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%s: %s is not valid UTF-8", label, absPath)
	}

	dir := filepath.Dir(absPath)
	lines := strings.Split(string(data), "\n")

	var b strings.Builder
	inCodeBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}

		if !inCodeBlock && strings.HasPrefix(trimmed, "@") {
			target := strings.TrimSpace(strings.TrimPrefix(trimmed, "@"))
			if target == "" {
				continue
			}
			includePath := target
			if !filepath.IsAbs(includePath) && !isWindowsAbs(includePath) {
				includePath = filepath.Join(dir, includePath)
			}
			included, err := l.load(includePath, depth+1)
			if err != nil {
				return "", err
			}
			included = strings.TrimRight(included, "\n")
			if included != "" {
				b.WriteString(included)
				b.WriteByte('\n')
			}
			continue
		}

		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func readFileLimited(filesystem *FS, path string, maxBytes int64, label string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		if strings.TrimSpace(label) == "" {
			label = "included.md"
		}
		return nil, fmt.Errorf("%s: path is empty", label)
	}
	if maxBytes <= 0 {
		maxBytes = includedMDMaxFileBytes
	}

	stat := func() (iofs.FileInfo, error) { return os.Stat(path) }
	read := func() ([]byte, error) { return os.ReadFile(path) }
	if filesystem != nil {
		stat = func() (iofs.FileInfo, error) { return filesystem.Stat(path) }
		read = func() ([]byte, error) { return filesystem.ReadFile(path) }
	}

	info, err := stat()
	if err != nil {
		return nil, err
	}
	if info != nil && info.Size() > maxBytes {
		return nil, fmt.Errorf("%s: %s exceeds %d bytes limit", strings.TrimSpace(label), path, maxBytes)
	}
	data, err := read()
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s: %s exceeds %d bytes limit", strings.TrimSpace(label), path, maxBytes)
	}
	return data, nil
}

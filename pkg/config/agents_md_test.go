package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAgentsMDWithIncludes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("hello\n@extra.md\n```\\n@ignored.md\\n```"), 0o600); err != nil {
		t.Fatalf("write agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.md"), []byte("extra"), 0o600); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	content, err := LoadAgentsMD(root, nil)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if !strings.Contains(content, "extra") || !strings.Contains(content, "@ignored.md") {
		t.Fatalf("unexpected content %q", content)
	}
}

func TestLoadAgentsMDErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("@../outside.md"), 0o600); err != nil {
		t.Fatalf("write agents: %v", err)
	}
	if _, err := LoadAgentsMD(root, nil); err == nil {
		t.Fatalf("expected include escape error")
	}
	if content, err := LoadAgentsMD(filepath.Join(root, "missing"), nil); err != nil || content != "" {
		t.Fatalf("expected missing agents md empty, got %q err=%v", content, err)
	}
}

func TestReadFileLimited(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.md")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := readFileLimited(nil, "", 10, "agents.md"); err == nil {
		t.Fatalf("expected empty path error")
	}

	data, err := readFileLimited(nil, path, 10, "agents.md")
	if err != nil || string(data) != "hello" {
		t.Fatalf("unexpected read %q err=%v", data, err)
	}

	if _, err := readFileLimited(nil, path, 1, "agents.md"); err == nil {
		t.Fatalf("expected size limit error")
	}

	if _, err := readFileLimited(nil, path, 0, "agents.md"); err != nil {
		t.Fatalf("expected default max bytes to allow read: %v", err)
	}

	fsLayer := NewFS(dir, nil)
	data2, err := readFileLimited(fsLayer, path, 10, "agents.md")
	if err != nil || string(data2) != "hello" {
		t.Fatalf("unexpected fs read %q err=%v", data2, err)
	}
}

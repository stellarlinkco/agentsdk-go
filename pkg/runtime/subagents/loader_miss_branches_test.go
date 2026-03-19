package subagents

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
)

func TestLoadSubagentDirNilFSAndSkipsNonMarkdown(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "skip.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write skip: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "ok.md"), []byte(strings.Join([]string{
		"---",
		"name: ok",
		"description: d",
		"---",
		"body",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write ok: %v", err)
	}

	files, errs := loadSubagentDir(agentsDir, nil)
	if len(errs) != 0 {
		t.Fatalf("errs=%v, want none", errs)
	}
	if _, ok := files["ok"]; !ok || len(files) != 1 {
		t.Fatalf("files=%v, want only ok", files)
	}
}

func TestLoadSubagentDirDetectsDuplicateNames(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := strings.Join([]string{
		"---",
		"name: dup",
		"description: d",
		"---",
		"body",
	}, "\n")
	if err := os.WriteFile(filepath.Join(agentsDir, "a.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "b.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}

	_, errs := loadSubagentDir(agentsDir, config.NewFS("", nil))
	if len(errs) == 0 || !hasError(errs, "duplicate subagent") {
		t.Fatalf("errs=%v, want duplicate subagent", errs)
	}
}

func TestParseSubagentFileNilFSAndReadError(t *testing.T) {
	root := t.TempDir()
	path := mustWrite(t, root, ".agents/agents/ok.md", strings.Join([]string{
		"---",
		"name: ok",
		"description: d",
		"---",
		"body",
	}, "\n"))

	file, err := parseSubagentFile(path, "fallback", nil)
	if err != nil || file.Metadata.Name != "ok" {
		t.Fatalf("file=%v err=%v, want ok", file, err)
	}

	_, err = parseSubagentFile(string([]byte{0}), "fallback", nil)
	if err == nil || !strings.Contains(err.Error(), "subagents: read") {
		t.Fatalf("err=%v, want read error", err)
	}
}

func TestBuildMetadataMapEmptyReturnsNil(t *testing.T) {
	if got := buildMetadataMap(SubagentFile{}, nil, nil, ""); got != nil {
		t.Fatalf("got=%v, want nil", got)
	}
}

type failingReadDirFile struct{}

type staticDirInfo struct {
	name string
}

func (i staticDirInfo) Name() string       { return i.name }
func (i staticDirInfo) Size() int64        { return 0 }
func (i staticDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (i staticDirInfo) ModTime() time.Time { return time.Time{} }
func (i staticDirInfo) IsDir() bool        { return true }
func (i staticDirInfo) Sys() any           { return nil }

func (failingReadDirFile) Stat() (fs.FileInfo, error) { return staticDirInfo{name: "agents"}, nil }
func (failingReadDirFile) Read([]byte) (int, error)   { return 0, io.EOF }
func (failingReadDirFile) Close() error               { return nil }
func (failingReadDirFile) ReadDir(int) ([]fs.DirEntry, error) {
	return nil, errors.New("readdir boom")
}

type failingWalkFS struct {
	root string
}

func (f failingWalkFS) Open(name string) (fs.File, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.root) {
		return failingReadDirFile{}, nil
	}
	return nil, fs.ErrNotExist
}

func (f failingWalkFS) Stat(name string) (fs.FileInfo, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.root) {
		return staticDirInfo{name: "agents"}, nil
	}
	return nil, fs.ErrNotExist
}

func TestLoadSubagentDirCollectsWalkReturnError(t *testing.T) {
	root := "/does-not-exist-" + t.Name()
	agentsDir := filepath.Join(root, ".agents", "agents")

	embedRoot := strings.TrimPrefix(filepath.ToSlash(agentsDir), "/")
	fsLayer := config.NewFS("", failingWalkFS{root: embedRoot})

	_, errs := loadSubagentDir(agentsDir, fsLayer)
	if len(errs) == 0 || !hasError(errs, "readdir boom") {
		t.Fatalf("errs=%v, want readdir boom", errs)
	}
}

func TestLoadFromFSRejectsInvalidModelInMetadata(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, ".agents/agents/bad.md", strings.Join([]string{
		"---",
		"name: bad",
		"description: d",
		"model: nope",
		"---",
		"body",
	}, "\n"))

	_, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(errs) == 0 || !hasError(errs, "invalid model") {
		t.Fatalf("errs=%v, want invalid model", errs)
	}
}

func TestBuildHandlerCopiesMetadataWhenNonEmpty(t *testing.T) {
	file := SubagentFile{Metadata: SubagentMetadata{Name: "x"}, Body: "body"}
	meta := map[string]any{"k": "v"}
	h := buildHandler(file, meta)
	res, err := h.Handle(context.Background(), Context{}, Request{Instruction: "run"})
	if err != nil || res.Metadata["k"] != "v" {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

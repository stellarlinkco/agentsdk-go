package api

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMaterializeEmbeddedClaudeHooks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := fstest.MapFS{
		".agents/hooks/pre.sh":      {Data: []byte("echo pre")},
		".agents/hooks/sub/post.sh": {Data: []byte("echo post")},
	}
	if err := materializeEmbeddedClaudeHooks(root, fs); err != nil {
		t.Fatalf("materialize failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".agents/hooks/pre.sh"))
	if err != nil || !strings.Contains(string(data), "pre") {
		t.Fatalf("unexpected hook content %q err=%v", data, err)
	}

	// existing file should not be overwritten
	dest := filepath.Join(root, ".agents/hooks/pre.sh")
	if err := os.WriteFile(dest, []byte("local"), 0o600); err != nil {
		t.Fatalf("write dest: %v", err)
	}
	if err := materializeEmbeddedClaudeHooks(root, fs); err != nil {
		t.Fatalf("materialize second failed: %v", err)
	}
	data, err = os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "local" {
		t.Fatalf("expected local file preserved")
	}
}

func TestMaterializeEmbeddedClaudeHooksErrors(t *testing.T) {
	t.Parallel()

	if err := materializeEmbeddedClaudeHooks(" ", nil); err != nil {
		t.Fatalf("expected nil embedfs ok, got %v", err)
	}
	if err := materializeEmbeddedClaudeHooks(t.TempDir(), fstest.MapFS{}); err != nil {
		t.Fatalf("expected missing dir to be ignored, got %v", err)
	}
	fs := fstest.MapFS{
		".agents/hooks": {Data: []byte("not dir")},
	}
	if err := materializeEmbeddedClaudeHooks(t.TempDir(), fs); err == nil {
		t.Fatalf("expected not-a-dir error")
	}

	errFS := statErrFS{err: errors.New("stat failed")}
	if err := materializeEmbeddedClaudeHooks(t.TempDir(), errFS); err == nil || !strings.Contains(err.Error(), "stat embedded") {
		t.Fatalf("expected stat error, got %v", err)
	}

	readFS := readErrFS{FS: fstest.MapFS{".agents/hooks/pre.sh": {Data: []byte("hi")}}, err: errors.New("read failed")}
	if err := materializeEmbeddedClaudeHooks(t.TempDir(), readFS); err == nil || !strings.Contains(err.Error(), "read embedded") {
		t.Fatalf("expected read error, got %v", err)
	}
}

type statErrFS struct{ err error }

func (s statErrFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
func (s statErrFS) Stat(string) (fs.FileInfo, error) {
	return nil, s.err
}

type readErrFS struct {
	fs.FS
	err error
}

func (r readErrFS) ReadFile(string) ([]byte, error) {
	return nil, r.err
}

type sideEffectReadFS struct {
	fstest.MapFS
	root            string
	makeDestDir     bool
	chmodParentTo   os.FileMode
	chmodParentPath string
}

func (s *sideEffectReadFS) ReadFile(name string) ([]byte, error) {
	if s.makeDestDir || s.chmodParentTo != 0 {
		dest := filepath.Join(s.root, filepath.FromSlash(path.Clean(name)))
		if s.makeDestDir {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return nil, err
			}
		}
		if s.chmodParentTo != 0 && s.chmodParentPath == "" {
			parent := filepath.Dir(dest)
			if err := os.Chmod(parent, s.chmodParentTo); err == nil {
				s.chmodParentPath = parent
			}
		}
	}

	entry, ok := s.MapFS[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return entry.Data, nil
}

func TestMaterializeEmbeddedClaudeHooksRenameErrorIgnoredWhenDestAppears(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := &sideEffectReadFS{
		MapFS: fstest.MapFS{
			".agents/hooks/pre.sh": {Data: []byte("echo pre")},
		},
		root:        root,
		makeDestDir: true,
	}

	if err := materializeEmbeddedClaudeHooks(root, fs); err != nil {
		t.Fatalf("materialize failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, ".agents/hooks/pre.sh"))
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected dest to be a directory (rename should have failed)")
	}
}

func TestMaterializeEmbeddedClaudeHooksWriteTempError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions behave differently on windows")
	}

	root := t.TempDir()
	fs := &sideEffectReadFS{
		MapFS: fstest.MapFS{
			".agents/hooks/pre.sh": {Data: []byte("echo pre")},
		},
		root:          root,
		chmodParentTo: 0o555,
	}

	err := materializeEmbeddedClaudeHooks(root, fs)
	if fs.chmodParentPath != "" {
		if chmodErr := os.Chmod(fs.chmodParentPath, 0o755); chmodErr != nil {
			t.Errorf("chmod cleanup: %v", chmodErr)
		}
	}
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("expected write error, got %v", err)
	}
}

package toolbuiltin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
)

func TestFileSandboxResolveReadWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	fs := newFileSandboxWithSandbox(root, sandbox.NewFileSystemAllowList(root))
	if _, err := fs.resolvePath(nil); err == nil {
		t.Fatalf("expected nil path error")
	}
	if _, err := fs.resolvePath(1); err == nil {
		t.Fatalf("expected non-string error")
	}
	if _, err := fs.resolvePath(" "); err == nil {
		t.Fatalf("expected empty path error")
	}

	path, err := fs.resolvePath("file.txt")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if err := fs.writeFile(path, "hello"); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	read, err := fs.readFile(path)
	if err != nil || read != "hello" {
		t.Fatalf("read failed: %v content=%q", err, read)
	}
}

func TestFileSandboxReadLimits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	fs := newFileSandboxWithSandbox(root, sandbox.NewFileSystemAllowList(root))
	fs.maxBytes = 3

	path := filepath.Join(root, "big.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := fs.readFile(path); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size error, got %v", err)
	}

	bin := filepath.Join(root, "bin.dat")
	if err := os.WriteFile(bin, []byte{'a', 0, 'b'}, 0o600); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	if _, err := fs.readFile(bin); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("expected binary error, got %v", err)
	}
}

func TestFileSandboxWriteLimits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	fs := newFileSandboxWithSandbox(root, sandbox.NewFileSystemAllowList(root))
	fs.maxBytes = 3

	path := filepath.Join(root, "tiny.txt")
	if err := fs.writeFile(path, "toolong"); err == nil {
		t.Fatalf("expected size error")
	}
}

func TestFileSandboxNilAndDirErrors(t *testing.T) {
	if _, err := (*fileSandbox)(nil).resolvePath("x"); err == nil {
		t.Fatalf("expected nil sandbox error")
	}
	if _, err := (*fileSandbox)(nil).readFile("x"); err == nil {
		t.Fatalf("expected nil sandbox read error")
	}
	if err := (*fileSandbox)(nil).writeFile("x", "y"); err == nil {
		t.Fatalf("expected nil sandbox write error")
	}

	root := t.TempDir()
	fs := newFileSandboxWithSandbox(root, sandbox.NewFileSystemAllowList(root))
	dirPath := filepath.Join(root, "dir")
	if err := os.MkdirAll(dirPath, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := fs.readFile(dirPath); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected directory error, got %v", err)
	}
}

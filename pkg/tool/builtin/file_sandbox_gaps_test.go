package toolbuiltin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
)

func TestFileSandboxReadFile_StatAndReadErrors(t *testing.T) {
	skipIfWindows(t)

	root := cleanTempDir(t)
	fs := newFileSandboxWithSandbox(root, sandbox.NewFileSystemAllowList(root))

	if _, err := fs.readFile(filepath.Join(root, "missing.txt")); err == nil || !strings.Contains(err.Error(), "stat file") {
		t.Fatalf("expected stat error, got %v", err)
	}

	path := filepath.Join(root, "nope.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if _, err := fs.readFile(path); err == nil || !strings.Contains(err.Error(), "read file") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestFileSandboxWriteFile_MkdirAndWriteErrors(t *testing.T) {
	skipIfWindows(t)

	root := cleanTempDir(t)
	fs := newFileSandboxWithSandbox(root, sandbox.NewFileSystemAllowList(root))

	blocker := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := fs.writeFile(filepath.Join(blocker, "child.txt"), "hello"); err == nil || !strings.Contains(err.Error(), "ensure directory") {
		t.Fatalf("expected mkdir error, got %v", err)
	}

	noPerm := filepath.Join(root, "no-perm")
	if err := os.MkdirAll(noPerm, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(noPerm, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(noPerm, 0o700) })

	if err := fs.writeFile(filepath.Join(noPerm, "x.txt"), "hi"); err == nil || !strings.Contains(err.Error(), "write file") {
		t.Fatalf("expected write error, got %v", err)
	}
}

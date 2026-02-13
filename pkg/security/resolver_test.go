package security

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestPathResolverRejectsDangerousPaths(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		wantErr   string
		configure func(r *PathResolver)
	}{
		{
			name: "symlink loop detection",
			setup: func(t *testing.T) string {
				root := tempDirClean(t)
				loopA := filepath.Join(root, "loopA")
				loopB := filepath.Join(root, "loopB")
				mustSymlink(t, loopB, loopA)
				mustSymlink(t, loopA, loopB)
				return loopA
			},
			wantErr: "symlink",
		},
		{
			name: "nested symlink escape",
			setup: func(t *testing.T) string {
				root := tempDirClean(t)
				outside := filepath.Join(tempDirClean(t), "loot.txt")
				if err := os.WriteFile(outside, []byte("loot"), 0o600); err != nil {
					t.Fatalf("write loot: %v", err)
				}
				deep := filepath.Join(root, "deep", "nested")
				if err := os.MkdirAll(deep, 0o755); err != nil {
					t.Fatalf("mk deep: %v", err)
				}
				link := filepath.Join(deep, "pivot")
				mustSymlink(t, outside, link)
				return filepath.Join(link, "data.txt")
			},
			wantErr: "symlink",
		},
		{
			name: "permission denied surfaces error",
			setup: func(t *testing.T) string {
				if runtime.GOOS == "windows" {
					t.Skip("permission bits unsupported on windows")
				}
				root := tempDirClean(t)
				sealed := filepath.Join(root, "sealed")
				if err := os.MkdirAll(sealed, 0o700); err != nil {
					t.Fatalf("mkdir sealed: %v", err)
				}
				if err := os.Chmod(sealed, 0o000); err != nil {
					t.Fatalf("chmod sealed: %v", err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(sealed, 0o755); err != nil {
						t.Fatalf("restore perms: %v", err)
					}
				})
				return filepath.Join(sealed, "payload.txt")
			},
			wantErr: "permission denied",
		},
		{
			name: "path depth exceeds limit",
			setup: func(t *testing.T) string {
				root := tempDirClean(t)
				return filepath.Join(root, "a", "b", "c")
			},
			configure: func(r *PathResolver) { r.maxDepth = 2 },
			wantErr:   "max depth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewPathResolver()
			if tt.configure != nil {
				tt.configure(resolver)
			}
			path := tt.setup(t)
			if _, err := resolver.Resolve(path); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q got %v", tt.wantErr, err)
			}
		})
	}
}

func TestPathResolverHandlesSafePaths(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr string
	}{
		{
			name: "hard link treated as regular file",
			setup: func(t *testing.T) string {
				dir := tempDirClean(t)
				original := filepath.Join(dir, "original.txt")
				if err := os.WriteFile(original, []byte("data"), 0o600); err != nil {
					t.Fatalf("write original: %v", err)
				}
				hard := filepath.Join(dir, "hard.txt")
				mustHardlink(t, original, hard)
				return hard
			},
		},
		{
			name: "nonexistent path resolves cleanly",
			setup: func(t *testing.T) string {
				dir := tempDirClean(t)
				return filepath.Join(dir, "missing", "file.txt")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewPathResolver()
			path := tt.setup(t)
			resolved, err := resolver.Resolve(path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resolved == "" || !filepath.IsAbs(resolved) {
					t.Fatalf("expected absolute resolved path, got %q", resolved)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q got %v", tt.wantErr, err)
			}
		})
	}
}

func mustHardlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Link(oldname, newname); err != nil {
		if os.IsPermission(err) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EXDEV) {
			t.Skipf("hard link unsupported: %v", err)
		}
		t.Fatalf("link: %v", err)
	}
}

func TestOpenNoFollowMissingPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O_NOFOLLOW unsupported on this platform")
	}
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if err := openNoFollow(missing); err == nil || !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("expected missing path error, got %v", err)
	}
}

func TestResolveRootPath(t *testing.T) {
	resolver := NewPathResolver()
	root, err := resolver.Resolve(string(filepath.Separator))
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	if root != string(filepath.Separator) {
		t.Fatalf("expected root to resolve to %q, got %q", string(filepath.Separator), root)
	}
}

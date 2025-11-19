package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSandboxValidatePath(t *testing.T) {
	root := tempDirClean(t)
	inside := filepath.Join(root, "dir", "file.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatalf("mk inside: %v", err)
	}
	if err := os.WriteFile(inside, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	outsideRoot := tempDirClean(t)
	outside := filepath.Join(outsideRoot, "escape.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		allow   string
		wantErr string
	}{
		{"inside root allowed", inside, "", ""},
		{"outside root blocked", outside, "", ErrPathNotAllowed.Error()},
		{"additional allowlist enables path", outside, outsideRoot, ""},
		{"empty path rejected", "   ", "", "empty path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox := NewSandbox(root)
			if tt.allow != "" {
				sandbox.Allow(tt.allow)
			}
			err := sandbox.ValidatePath(tt.path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSandboxValidatePathScenarios(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		setup   func(t *testing.T) (*Sandbox, string)
	}{
		{
			name: "allows absolute path in allowlist",
			setup: func(t *testing.T) (*Sandbox, string) {
				root := tempDirClean(t)
				safe := filepath.Join(root, "dir", "safe.txt")
				if err := os.MkdirAll(filepath.Dir(safe), 0o755); err != nil {
					t.Fatalf("mk safe: %v", err)
				}
				if err := os.WriteFile(safe, []byte("ok"), 0o600); err != nil {
					t.Fatalf("write safe: %v", err)
				}
				return NewSandbox(root), safe
			},
		},
		{
			name:    "rejects path outside allowlist",
			wantErr: ErrPathNotAllowed.Error(),
			setup: func(t *testing.T) (*Sandbox, string) {
				root := tempDirClean(t)
				outsideRoot := tempDirClean(t)
				target := filepath.Join(outsideRoot, "escape.txt")
				if err := os.WriteFile(target, []byte("blocked"), 0o600); err != nil {
					t.Fatalf("write outside: %v", err)
				}
				return NewSandbox(root), target
			},
		},
		{
			name: "additional allowlist permits extra root",
			setup: func(t *testing.T) (*Sandbox, string) {
				root := tempDirClean(t)
				outsideRoot := tempDirClean(t)
				target := filepath.Join(outsideRoot, "shared.txt")
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatalf("mk shared dir: %v", err)
				}
				if err := os.WriteFile(target, []byte("allowed"), 0o600); err != nil {
					t.Fatalf("write shared: %v", err)
				}
				sb := NewSandbox(root)
				sb.Allow(outsideRoot)
				return sb, target
			},
		},
		{
			name:    "parent traversal attack blocked",
			wantErr: ErrPathNotAllowed.Error(),
			setup: func(t *testing.T) (*Sandbox, string) {
				sb := NewSandbox(tempDirClean(t))
				return sb, filepath.Join("..", "..", "..", "etc", "passwd")
			},
		},
		{
			name: "relative path inside workdir allowed",
			setup: func(t *testing.T) (*Sandbox, string) {
				root := tempDirClean(t)
				safe := filepath.Join(root, "rel", "data.txt")
				if err := os.MkdirAll(filepath.Dir(safe), 0o755); err != nil {
					t.Fatalf("mk rel dir: %v", err)
				}
				if err := os.WriteFile(safe, []byte("rel"), 0o600); err != nil {
					t.Fatalf("write rel: %v", err)
				}
				orig := mustGetwd(t)
				if err := os.Chdir(root); err != nil {
					t.Fatalf("chdir root: %v", err)
				}
				t.Cleanup(func() {
					if err := os.Chdir(orig); err != nil {
						t.Fatalf("restore cwd: %v", err)
					}
				})
				rel, err := filepath.Rel(root, safe)
				if err != nil {
					t.Fatalf("rel path: %v", err)
				}
				return NewSandbox(root), rel
			},
		},
		{
			name:    "working directory restriction blocks sibling",
			wantErr: ErrPathNotAllowed.Error(),
			setup: func(t *testing.T) (*Sandbox, string) {
				parent := tempDirClean(t)
				work := filepath.Join(parent, "workspace")
				if err := os.MkdirAll(work, 0o755); err != nil {
					t.Fatalf("mk workspace: %v", err)
				}
				sibling := filepath.Join(parent, "other", "note.txt")
				if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
					t.Fatalf("mk sibling dir: %v", err)
				}
				if err := os.WriteFile(sibling, []byte("blocked"), 0o600); err != nil {
					t.Fatalf("write sibling: %v", err)
				}
				return NewSandbox(work), sibling
			},
		},
		{
			name:    "working directory cannot escape via absolute",
			wantErr: ErrPathNotAllowed.Error(),
			setup: func(t *testing.T) (*Sandbox, string) {
				work := tempDirClean(t)
				sb := NewSandbox(work)
				return sb, filepath.Join(work, "..", "..", "secret.txt")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb, path := tt.setup(t)
			err := sb.ValidatePath(path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSandboxRejectsSymlinkEscape(t *testing.T) {
	tests := []struct {
		name     string
		wantErr  string
		makePath func(t *testing.T, root string) string
	}{
		{
			name:    "symlink outside root rejected",
			wantErr: "symlink",
			makePath: func(t *testing.T, root string) string {
				t.Helper()
				target := filepath.Join(t.TempDir(), "target.txt")
				if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
					t.Fatalf("write target: %v", err)
				}
				symlink := filepath.Join(root, "link.txt")
				mustSymlink(t, target, symlink)
				return symlink
			},
		},
		{
			name:    "symlink chain escapes root",
			wantErr: "symlink",
			makePath: func(t *testing.T, root string) string {
				t.Helper()
				outside := filepath.Join(t.TempDir(), "target.txt")
				if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
					t.Fatalf("write target: %v", err)
				}
				link2 := filepath.Join(root, "link-2")
				mustSymlink(t, outside, link2)
				link1 := filepath.Join(root, "link-1")
				mustSymlink(t, link2, link1)
				return link1
			},
		},
		{
			name:    "relative symlink climbs outside sandbox",
			wantErr: "symlink",
			makePath: func(t *testing.T, root string) string {
				t.Helper()
				outsideDir := t.TempDir()
				target := filepath.Join(outsideDir, "secret.txt")
				if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
					t.Fatalf("write secret: %v", err)
				}
				nested := filepath.Join(root, "nested")
				if err := os.MkdirAll(nested, 0o755); err != nil {
					t.Fatalf("mk nested: %v", err)
				}
				relTarget, err := filepath.Rel(nested, target)
				if err != nil {
					t.Fatalf("rel target: %v", err)
				}
				link := filepath.Join(nested, "rel-link")
				mustSymlink(t, relTarget, link)
				return link
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := tt.makePath(t, root)
			sb := NewSandbox(root)
			if err := sb.ValidatePath(path); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSandboxValidateCommand(t *testing.T) {
	t.Parallel()
	sb := NewSandbox(t.TempDir())

	tests := []struct {
		name    string
		cmd     string
		wantErr string
	}{
		{name: "banned command", cmd: "rm -rf /", wantErr: "fragment"},
		{name: "blocked metacharacter", cmd: "ls | rm -rf /", wantErr: "metacharacters"},
		{name: "forbidden argument", cmd: "cat ../etc/passwd", wantErr: "argument"},
		{name: "safe command passes", cmd: `echo "hello world"`, wantErr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sb.ValidateCommand(tt.cmd)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSandboxAllowIgnoresInvalidEntries(t *testing.T) {
	t.Parallel()
	root := tempDirClean(t)
	sb := NewSandbox(root)
	initial := len(sb.allowList)

	sb.Allow("")              // ignored empty
	sb.Allow(sb.allowList[0]) // duplicate
	if len(sb.allowList) != initial {
		t.Fatalf("allow list changed for invalid inputs: %v", sb.allowList)
	}

	additional := filepath.Join(root, "extra")
	sb.Allow(additional)
	if len(sb.allowList) != initial+1 {
		t.Fatalf("expected exactly one new entry, got %d entries", len(sb.allowList))
	}
	if got, want := sb.allowList[len(sb.allowList)-1], normalizePath(additional); got != want {
		t.Fatalf("expected normalized path %q got %q", want, got)
	}
}

func TestSandboxValidatePathResolverError(t *testing.T) {
	t.Parallel()
	root := tempDirClean(t)
	target := filepath.Join(root, "nested", "deep", "file.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	sb := NewSandbox(root)
	sb.resolver.maxDepth = 1

	err := sb.ValidatePath(target)
	if err == nil {
		t.Fatalf("expected resolver error")
	}
	if !strings.Contains(err.Error(), "resolve failed") || !strings.Contains(err.Error(), "max depth") {
		t.Fatalf("expected resolver depth error, got %v", err)
	}
}

func TestNewSandboxDefaultsToRoot(t *testing.T) {
	t.Parallel()
	sb := NewSandbox("")
	if len(sb.allowList) != 1 || sb.allowList[0] != string(filepath.Separator) {
		t.Fatalf("unexpected allow list: %#v", sb.allowList)
	}
}

func TestWithinSandboxScenarios(t *testing.T) {
	t.Parallel()
	root := tempDirClean(t)
	child := filepath.Join(root, "child")
	if !withinSandbox(child, root) {
		t.Fatalf("expected child inside sandbox")
	}
	outside := filepath.Join(root, "..", "outside")
	if withinSandbox(outside, root) {
		t.Fatalf("expected outside path to be rejected")
	}
	if withinSandbox(child, "") {
		t.Fatalf("empty prefix should never allow access")
	}
	if !withinSandbox(filepath.Join(string(filepath.Separator), "tmp"), string(filepath.Separator)) {
		t.Fatalf("root prefix should allow everything")
	}
	same := filepath.Join(root, "same")
	if !withinSandbox(same, same) {
		t.Fatalf("path equal to prefix must be allowed")
	}
	if !withinSandbox(filepath.Join(same, "nested", "leaf"), same) {
		t.Fatalf("nested path should inherit prefix even without trailing slash")
	}
}

func tempDirClean(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err == nil {
		return realDir
	}
	return dir
}

func mustSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if os.IsPermission(err) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS) {
			t.Skipf("symlink unsupported: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/security"
)

// TestPathResolverValidation validates canonical resolution, traversal detection, and symlink rejection.
func TestPathResolverValidation(t *testing.T) {
	root := tempRoot(t)
	safePath := filepath.Join(root, "reports", "weekly.txt")
	escapePath := filepath.Join(root, "..", "rogue", "passwd")
	symlinkBase := filepath.Join(root, "link-out")

	resolver := security.NewPathResolver()
	sandbox := security.NewSandbox(root)

	cases := []struct {
		name             string
		path             string
		setup            func(t *testing.T)
		expectResolveErr bool
		expectSandboxErr bool
	}{
		{
			name:             "safePathStaysInside",
			path:             safePath,
			expectResolveErr: false,
			expectSandboxErr: false,
		},
		{
			name:             "pathEscapeBlockedBySandbox",
			path:             escapePath,
			expectResolveErr: false,
			expectSandboxErr: true,
		},
		{
			name: "symlinkBlocked",
			path: filepath.Join(symlinkBase, "child.txt"),
			setup: func(t *testing.T) {
				if runtime.GOOS == "windows" {
					t.Skip("Windows symlink creation requires privileges")
				}
				ensureDir(t, filepath.Dir(symlinkBase))
				if err := os.Symlink(root, symlinkBase); err != nil {
					t.Skipf("symlink creation failed: %v", err)
				}
			},
			expectResolveErr: true,
			expectSandboxErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			_, err := resolver.Resolve(tc.path)
			if (err != nil) != tc.expectResolveErr {
				t.Fatalf("resolver error=%v wantErr=%v", err, tc.expectResolveErr)
			}
			if err == nil {
				if err := sandbox.ValidatePath(tc.path); (err != nil) != tc.expectSandboxErr {
					t.Fatalf("sandbox error=%v wantErr=%v", err, tc.expectSandboxErr)
				}
			}
		})
	}
}

// TestValidatorCommands checks the command validator against benign and malicious inputs.
func TestValidatorCommands(t *testing.T) {
	validator := security.NewValidator()

	cases := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		{
			name:    "simpleCatCommand",
			cmd:     `cat "/etc/hosts"`,
			wantErr: false,
		},
		{
			name:    "commandWithQuotesValid",
			cmd:     `grep "hello world" /tmp/log.txt`,
			wantErr: false,
		},
		{
			name:    "bannedBinary",
			cmd:     "rm -rf /",
			wantErr: true,
		},
		{
			name:    "pipeUsageBlocked",
			cmd:     "ls | cat",
			wantErr: true,
		},
		{
			name:    "bannedArgumentDetected",
			cmd:     "echo --no-preserve-root",
			wantErr: true,
		},
		{
			name:    "emptyCommandRejected",
			cmd:     "   ",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.Validate(tc.cmd)
			if tc.wantErr && err == nil {
				t.Fatalf("expected failure for %q", tc.cmd)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.cmd, err)
			}
		})
	}
}

// TestSandboxValidation combines path and command decisions to mimic the example flow.
func TestSandboxValidation(t *testing.T) {
	root := tempRoot(t)
	sandbox := security.NewSandbox(root)

	insidePath := filepath.Join(root, "reports", "weekly.txt")
	ensureDir(t, filepath.Dir(insidePath))

	cases := []struct {
		name        string
		path        string
		cmd         string
		wantPathErr bool
		wantCmdErr  bool
	}{
		{
			name:        "allGreen",
			path:        insidePath,
			cmd:         "echo safe",
			wantPathErr: false,
			wantCmdErr:  false,
		},
		{
			name:        "pathEscapeFails",
			path:        filepath.Join(root, "..", "rogue", "file.txt"),
			cmd:         "echo still-safe",
			wantPathErr: true,
			wantCmdErr:  false,
		},
		{
			name:        "commandBlocked",
			path:        insidePath,
			cmd:         "rm --no-preserve-root /",
			wantPathErr: false,
			wantCmdErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := sandbox.ValidatePath(tc.path); (err != nil) != tc.wantPathErr {
				t.Fatalf("ValidatePath(%s) error=%v wantErr=%v", tc.name, err, tc.wantPathErr)
			}
			if err := sandbox.ValidateCommand(tc.cmd); (err != nil) != tc.wantCmdErr {
				t.Fatalf("ValidateCommand(%s) error=%v wantErr=%v", tc.name, err, tc.wantCmdErr)
			}
		})
	}
}

// TestWhitelistConfiguration ensures Allow augments sandbox roots predictably.
func TestWhitelistConfiguration(t *testing.T) {
	root := tempRoot(t)
	sandbox := security.NewSandbox(root)
	outside := tempRoot(t)

	cases := []struct {
		name    string
		allow   bool
		wantErr bool
	}{
		{
			name:    "blockedBeforeAllow",
			allow:   false,
			wantErr: true,
		},
		{
			name:    "allowedAfterRegister",
			allow:   true,
			wantErr: false,
		},
		{
			name:    "idempotentAllow",
			allow:   true,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.allow {
				sandbox.Allow(outside)
			}
			err := sandbox.ValidatePath(outside)
			if tc.wantErr && err == nil {
				t.Fatalf("expected path rejection for %s", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("path should be allowed for %s: %v", tc.name, err)
			}
		})
	}
}

// tempRoot returns a symlink-free temporary directory suitable for strict resolution.
func tempRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dir, err)
	}
	return resolved
}

func ensureDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

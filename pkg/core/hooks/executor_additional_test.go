package hooks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/core/events"
)

func TestExecutorWithWorkDirAndClose(t *testing.T) {
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil && resolved != "" {
		dir = resolved
	}
	exec := NewExecutor(WithWorkDir(dir))
	// Use stderr for output since exit 0 stdout is parsed as JSON
	exec.Register(ShellHook{Event: events.Notification, Command: "pwd >&2"})

	results, err := exec.Execute(context.Background(), events.Event{Type: events.Notification})
	if err != nil || len(results) == 0 {
		t.Fatalf("execute failed: %v", err)
	}
	if got := strings.TrimSpace(results[0].Stderr); !sameWorkDirPath(dir, got) {
		t.Fatalf("expected workdir %q, got %q", dir, got)
	}

	exec.Close()
}

func TestNewShellCommandUnixNormalCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only assertion")
	}

	cmd := newShellCommand(context.Background(), "echo hello")
	if cmd.Path != "/bin/sh" {
		t.Fatalf("expected /bin/sh, got %q", cmd.Path)
	}
	if len(cmd.Args) != 3 {
		t.Fatalf("expected 3 args, got %v", cmd.Args)
	}
	if cmd.Args[1] != "-c" {
		t.Fatalf("expected -c, got %q", cmd.Args[1])
	}
	if cmd.Args[2] != "echo hello" {
		t.Fatalf("expected command %q, got %q", "echo hello", cmd.Args[2])
	}
}

func TestNewShellCommandUnixEmptyCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only assertion")
	}

	cmd := newShellCommand(context.Background(), "")
	if cmd.Path != "/bin/sh" {
		t.Fatalf("expected /bin/sh, got %q", cmd.Path)
	}
	if len(cmd.Args) != 3 {
		t.Fatalf("expected 3 args, got %v", cmd.Args)
	}
	if cmd.Args[2] != "" {
		t.Fatalf("expected empty command, got %q", cmd.Args[2])
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected empty shell command to run, got %v", err)
	}
}

func TestNewShellCommandUnixWhitespaceCommandTrimmed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only assertion")
	}

	cmd := newShellCommand(context.Background(), " \n\t ")
	if cmd.Path != "/bin/sh" {
		t.Fatalf("expected /bin/sh, got %q", cmd.Path)
	}
	if len(cmd.Args) != 3 {
		t.Fatalf("expected 3 args, got %v", cmd.Args)
	}
	if cmd.Args[2] != "" {
		t.Fatalf("expected trimmed empty command, got %q", cmd.Args[2])
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected whitespace shell command to run, got %v", err)
	}
}

func sameWorkDirPath(expected, got string) bool {
	expected = filepath.Clean(expected)
	got = strings.TrimSpace(got)
	if got == "" {
		return false
	}
	if filepath.Clean(got) == expected {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	// Git Bash commonly reports Temp as /tmp/<rel> while Windows APIs return
	// C:\Users\...\AppData\Local\Temp\<rel>.
	tempRoot := filepath.Clean(os.TempDir())
	rel, err := filepath.Rel(tempRoot, expected)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	wantMSYS := "/tmp/" + filepath.ToSlash(rel)
	return filepath.Clean(got) == filepath.Clean(wantMSYS)
}

//go:build windows

package hooks

import (
	"context"
	"os/exec"
	"strings"
)

func newShellCommand(ctx context.Context, command string) *exec.Cmd {
	trimmed := strings.TrimSpace(command)
	return exec.CommandContext(ctx, "cmd", "/C", trimmed)
}

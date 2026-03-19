package hooks

import (
	"errors"
	"fmt"
	"strings"
)

var ErrSafetyHookDenied = errors.New("hooks: tool execution blocked by safety hook")

// SafetyCheck blocks catastrophic tool usage in YOLO-default mode.
//
// It is intentionally lightweight: string checks only, no I/O, and should run
// before any user-configured shell hooks.
func SafetyCheck(toolName string, params map[string]any) error {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return nil
	}
	switch name {
	case "bash":
		return safetyCheckBash(params)
	default:
		return nil
	}
}

func safetyCheckBash(params map[string]any) error {
	if len(params) == 0 {
		return nil
	}
	raw, ok := params["command"]
	if !ok || raw == nil {
		return nil
	}
	cmd, ok := raw.(string)
	if !ok {
		return nil
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	lower := strings.ToLower(cmd)

	// Fragment bans: cheap guard against obviously destructive intent.
	for _, fragment := range []string{
		"rm -rf /",
		"rm -fr /",
		"rm --recursive /",
		"--no-preserve-root",
		"--preserve-root=false",
	} {
		if strings.Contains(lower, fragment) {
			return fmt.Errorf("%w: bash command contains banned fragment %q", ErrSafetyHookDenied, fragment)
		}
	}

	// Base-command bans: power management, disk formatting, and privilege escalation.
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return nil
	}
	base := fields[0]
	switch base {
	case "dd", "mkfs", "mkfs.ext4", "fdisk", "parted", "format",
		"shutdown", "reboot", "halt", "poweroff",
		"sudo":
		return fmt.Errorf("%w: bash command %q is not allowed", ErrSafetyHookDenied, base)
	default:
		return nil
	}
}

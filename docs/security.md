# Security (v2)

agentsdk-go v2 uses a YOLO-default execution model with two enforcement layers:

1. **Sandbox** (`pkg/sandbox/`): filesystem/network/resource isolation (no approval workflow).
2. **Safety hook** (`pkg/hooks/safety.go`): Go-native `PreToolUse` check that blocks catastrophic `bash` commands before user shell hooks run.

## Safety Hook

- Runs before user-configured shell hooks.
- Blocks a small, explicit blocklist of destructive patterns (e.g. `rm -rf /`, `dd`, `mkfs`, `fdisk`, `shutdown`, `reboot`, `sudo`).
- Disable with `api.Options{DisableSafetyHook: true}`.

## Sandbox

Sandbox is about isolation, not permission prompts:

- Filesystem roots and path traversal controls
- Network restrictions (when enabled/configured)
- Resource limits

The sandbox manager is owned by tool execution (`pkg/tool/`) and is configured via `.agents/settings.json` and/or `api.Options`.

## Settings Notes

`.agents/settings.json` still accepts a `permissions` object for compatibility, but v2 core does not implement an approval/ask workflow.

- Use `permissions.additionalDirectories` to widen filesystem roots.
- Use `disallowedTools` to disable built-in tools by name.

Example:

```json
{
  "permissions": {
    "additionalDirectories": ["/data"]
  },
  "disallowedTools": ["bash"],
  "sandbox": {
    "enabled": true
  }
}
```

# Migration Guide: In-Process Hooks ➜ ShellHook (v0.4.0)

This guide walks existing users through the breaking Hooks API change introduced in v0.4.0. The in-process Go hook interfaces have been removed; hooks now run as shell commands that receive JSON on stdin and signal decisions via exit codes.

## What Changed

| Area | Old (≤ v0.3.x) | New (v0.4.0) |
| --- | --- | --- |
| Hook shape | Go interfaces: `PreToolUse(context.Context, events.ToolUsePayload) error`, `PostToolUse(...)`, `UserPromptSubmit(...)`, `Stop(...)`, `Notification(...)` | Shell commands executed via `/bin/sh -c` with JSON stdin; modeled as `hooks.ShellHook` |
| Decision channel | Return `error` to veto; no structured decision for PreToolUse | Exit codes: `0=allow`, `1=deny`, `2=ask`, others = failure. PreToolUse may emit JSON permission map on stdout |
| Registration | Pass structs implementing the interfaces into API options (e.g., the `demoHooks` in `examples/04-advanced/hooks.go`) | Provide `[]hooks.ShellHook` through `api.Options.TypedHooks` or declarative `.agents/settings.json` (`Hooks.PreToolUse` / `Hooks.PostToolUse`) |
| Payload | Go structs delivered directly | JSON envelope on stdin: `{"hook_event_name", "session_id"?, payload block}` |

## Migration Checklist

1) **Remove in-process hook structs** that implement the old interfaces (e.g., `demoHooks` in `examples/04-advanced/hooks.go`). They are no longer invoked by the runtime.

2) **Author shell commands/scripts** that accept JSON on stdin and exit with the correct code. Scripts run under `/bin/sh -c` and inherit environment variables plus `.agents/settings.json` `env`.

3) **Register ShellHooks** either programmatically or via settings:
- Programmatic: set `api.Options.TypedHooks` and optional `HookMiddleware` / `HookTimeout`.
- Declarative: add `Hooks.PreToolUse` / `Hooks.PostToolUse` command maps in `.agents/settings.json`. Tool names are matched using regex selectors.

4) **Validate selectors**: `hooks.NewSelector(toolPattern, payloadPattern)` compiles regex filters. Tool names must match for a hook to fire; leave blank for wildcard.

## Before/After Code

**Before (removed in v0.4.0): in-process hook implementation**

```go
// Implemented the typed interfaces and ran inside the agent process.
type demoHooks struct{}

func (h *demoHooks) PreToolUse(ctx context.Context, payload events.ToolUsePayload) error {
    // veto by returning an error
    return ctx.Err()
}

func (h *demoHooks) PostToolUse(ctx context.Context, payload events.ToolResultPayload) error {
    return ctx.Err()
}
// ... UserPromptSubmit / Stop / Notification similar ...
```

**After: shell-based hooks**

```go
import (
    "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
)

sel, _ := hooks.NewSelector("^Bash$", "") // limit to Bash tool

rt, err := api.New(ctx, api.Options{
    // ... other options ...
    TypedHooks: []hooks.ShellHook{
        {
            Event:    hooks.PreToolUse,
            Command:  "./scripts/pre_bash.sh",
            Selector: sel,
            Timeout:  2 * time.Second,
            Env:      map[string]string{"HOOK_ENV": "demo"},
        },
    },
    HookTimeout:    5 * time.Second,
    HookMiddleware: []hooks.Middleware{auditHook},
})
```

**Shell script example (`scripts/pre_bash.sh`)**

```sh
#!/bin/sh
# Reads JSON from stdin and denies dangerous params
payload=$(cat)

echo "$payload" | python - <<'PY'
import json, sys

data = json.load(sys.stdin)
cmd = (data.get("tool_input") or {}).get("params", {}).get("cmd", "")
if "rm -rf" in cmd:
    print('{"reason":"rm blocked"}')
    sys.exit(1)  # deny
sys.exit(0)       # allow
PY
```

> Exit codes: `0` allow, `1` deny, `2` ask/needs confirmation, any other value causes the run to fail. For PreToolUse, stdout may include a JSON object (permission hints) consumed by the runtime.

## Declarative Configuration Example (`.agents/settings.json`)

```json
{
  "env": {"AUDIT_OWNER": "platform"},
  "hooks": {
    "PreToolUse": {
      "^Bash$": "./scripts/pre_bash.sh"
    },
    "PostToolUse": {
      "^Bash$": "./scripts/post_bash.sh"
    }
  }
}
```

- Keys under `PreToolUse`/`PostToolUse` are regex patterns matched against the tool name.
- Commands run under `/bin/sh -c` with the JSON payload on stdin and inherit `env`.

## Payload Reference

The executor sends a compact envelope per event:

```json
{
  "hook_event_name": "PreToolUse",
  "session_id": "abc-123",             // omitted if empty
  "tool_input": {
    "name": "Bash",
    "params": {"cmd": "ls"}
  }
}
```

Other events populate `tool_response`, `user_prompt`, `notification`, or `stop` fields instead of `tool_input`.

## Migration Verification

- Run your hook scripts manually with sample JSON to confirm exit codes.
- Start the runtime with `HOOK_LOG=debug` (or your own logging in `HookMiddleware`) to ensure selectors match and timeouts are respected.
- CI smoke test: `grep -q 'Breaking Changes' README.md && test -f docs/migration-guide.md`.

If a hook returns a non-zero code other than `1` or `2`, the executor treats it as an error and aborts the agent iteration—keep scripts minimal and predictable (KISS/YAGNI).

# Migration Guide: MCP `mcpServers` ➜ `mcp.servers` (v0.4.0)

This section covers the breaking change that replaces the flat `mcpServers` list with a typed `mcp.servers` map in `.agents/settings.json` (effective v0.4.0).

## What Changed

| Area | Old (≤ v0.3.x) | New (v0.4.0) |
| --- | --- | --- |
| Config key | Top-level `mcpServers: []string` holding URLs or `stdio://cmd args` specs | Nested `mcp.servers: {name: MCPServerConfig}` map; names are required |
| Transport selection | Inferred from string prefix (`http[s]://` = SSE, otherwise stdio); missing fields silently passed through | Explicit `type` (`stdio` defaulted when empty, `http`, or `sse`) plus required fields: `command` for stdio, `url` for http/sse |
| Per-server options | None (no headers/env/timeout) | Optional `args`, `env`, `headers`, `timeoutSeconds` per server; env is sorted deterministically before spawning stdio transports |
| Validation | Legacy key allowed; weak checks | Presence of `mcpServers` now fails validation; empty server names, missing command/url, negative `timeoutSeconds`, or empty header keys all raise errors |

## Migration Checklist

1) Remove any `mcpServers` arrays from `.agents/settings.json`; keep CLI `--mcp` overrides only for ad-hoc use.
2) Create `mcp.servers` and assign stable, non-empty names for each server.
3) For stdio servers: set `type: "stdio"` (or leave blank), move the binary into `command`, and split flags into `args`.
4) For HTTP/SSE servers: set `type: "http"` or `"sse"`, move the endpoint into `url`, and port any auth into `headers`; add `timeoutSeconds` if you previously relied on global timeouts.
5) Run config validation (`go test ./pkg/config -run MCP`) or start the runtime to ensure no `mcp.servers[*]` validation errors fire.

## Before/After Configuration

**Before (deprecated)**

```json
{
  "mcpServers": [
    "stdio://./bin/mcp-server --flag",
    "https://mcp.example/api"
  ]
}
```

**After: structured, per-server options**

```json
{
  "mcp": {
    "servers": {
      "local-tools": {
        "type": "stdio",
        "command": "./bin/mcp-server",
        "args": ["--flag"],
        "env": {"FOO": "bar"}
      },
      "remote-api": {
        "type": "sse",
        "url": "https://mcp.example/api",
        "headers": {"Authorization": "Bearer ${TOKEN}"},
        "timeoutSeconds": 15
      }
    }
  }
}
```

## Transport Examples (new format)

- **stdio**

```json
{
  "mcp": {
    "servers": {
      "tooling": {
        "type": "stdio",
        "command": "node",
        "args": ["server.js"],
        "env": {"NODE_ENV": "production"}
      }
    }
  }
}
```

- **http**

```json
{
  "mcp": {
    "servers": {
      "http-api": {
        "type": "http",
        "url": "https://api.example/mcp",
        "headers": {"X-Api-Key": "${API_KEY}"},
        "timeoutSeconds": 10
      }
    }
  }
}
```

- **sse**

```json
{
  "mcp": {
    "servers": {
      "streaming": {
        "type": "sse",
        "url": "https://stream.example/mcp",
        "headers": {"Authorization": "Bearer ${TOKEN}"},
        "timeoutSeconds": 30
      }
    }
  }
}
```

## Common Migration Errors & Fixes

- Validation still mentions `mcpServers`: remove the legacy key entirely; only `mcp.servers` is accepted.
- `mcp.servers` entry name is empty: use non-blank map keys (e.g., `"default"`).
- `type` is stdio (default) but `command` is missing: fill `command` and, if needed, `args`/`env`.
- `type` is `http`/`sse` but `url` is blank: supply the full endpoint (including scheme).
- `timeoutSeconds` < 0 or headers contain empty keys: fix the values; validation refuses negative timeouts or blank header names.

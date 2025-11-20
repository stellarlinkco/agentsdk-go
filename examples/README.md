[中文](README_zh.md) | English

# agentsdk-go Examples

All examples can be run from the repository root.

**Hardening notes**
- Sandbox resource limits (CPU/memory/disk) are enabled by default to keep tools from over-consuming.
- historyStore uses LRU; `MaxSessions` defaults to 500 to prevent memory leaks during long runs.

## settings.json
All examples read the official `.claude/settings.json`. A minimal starter (also stored at `examples/middleware/.claude/settings.json`):
```json
{
  "permissions": {
    "allow": [
      "Bash(ls:*)",
      "Bash(pwd:*)",
      "Read(**/*.go)"
    ]
  },
  "env": {
    "EXAMPLE_VAR": "value"
  },
  "sandbox": {
    "enabled": false
  }
}
```

## cli
Minimal CLI flow that calls the Anthropic provider directly. The SDK now reads `.claude/settings.json` (official Claude format); point `AGENTSDK_PROJECT_ROOT` to a directory that already has one or create a minimal file using the snippet in `examples/middleware/.claude/settings.json`.

```bash
export ANTHROPIC_API_KEY=sk-...
go run ./examples/cli
```

Environment variables:
- Required: `ANTHROPIC_API_KEY`
- Optional: `AGENTSDK_PROJECT_ROOT` (skip temp config when pointing to an existing `.claude` directory), `ANTHROPIC_BASE_URL` (proxy/mirror), `HTTP_PROXY`/`HTTPS_PROXY`

## http
HTTP API implemented with the standard library (`/v1/run`, `/v1/run/stream`, `/v1/tools/execute`); relies on the sandbox and LRU session limits by default.

```bash
export ANTHROPIC_API_KEY=sk-...
export AGENTSDK_HTTP_ADDR=":8080"           # optional
export AGENTSDK_MAX_SESSIONS=500            # LRU cap to prevent memory leaks
curl -s http://localhost:8080/health || true
```

Key environment variables:
- Model: `ANTHROPIC_API_KEY` (required), `ANTHROPIC_BASE_URL` (proxy/mirror optional)
- Base: `AGENTSDK_HTTP_ADDR`, `AGENTSDK_PROJECT_ROOT`, `AGENTSDK_SANDBOX_ROOT`, `AGENTSDK_MODEL` (ensure the target root contains `.claude/settings.json`)
- Network: `AGENTSDK_NETWORK_ALLOW` (comma-separated allowlist, default `api.anthropic.com`)
- Timeout: `AGENTSDK_DEFAULT_TIMEOUT`, `AGENTSDK_MAX_TIMEOUT`
- Resources: `AGENTSDK_RESOURCE_CPU_PERCENT`, `AGENTSDK_RESOURCE_MEMORY_MB`, `AGENTSDK_RESOURCE_DISK_MB`, `AGENTSDK_MAX_BODY_BYTES`, `AGENTSDK_MAX_SESSIONS`

## mcp
Demonstrates connecting to `mcp-server-time` via stdio and invoking MCP tools.

```bash
uv tool install mcp-server-time  # install if missing
uvx mcp-server-time --help       # verify availability
go run ./examples/mcp
```

Requirements: `uv`/`uvx` in PATH; no API key needed.

## hooks
Pure local demo of the hooks executor covering 7 lifecycle callbacks and middleware logs, with deduplication and timeout control; no API key required.

```bash
go run ./examples/hooks -prompt "自检沙箱配置" -session hooks-demo -tool log_scan
```

Tunable flags: `-hook-timeout`, `-dedup-window`, `-tool-latency`.

## sandbox
Exercises the filesystem/network/resource triple sandbox strategy: first run through allowed paths then trigger limits; creates a temporary workspace by default; no API key required.

```bash
go run ./examples/sandbox
```

Optional flags: `--root`, `--allow-host`/`--deny-host`, `--cpu-limit`/`--mem-mb`/`--disk-mb`.

## skills
Shows runtime/skills registration, auto activation (matcher + priority/mutual exclusion) and manual execution.

```bash
go run ./examples/skills \
  -prompt "分析生产日志发现异常 SSH 尝试" \
  -env prod -severity high \
  -channels cli,slack -traits sre,security
```

Key flags: `-manual-skill`, `-timeout`.

## subagents
Minimal runtime/subagents usage: auto-select subagents via matchers or force dispatch; no API key required.

```bash
go run ./examples/subagents                      # auto selection
go run ./examples/subagents -target plan         # force dispatch
go run ./examples/subagents -prompt "scan logs"  # trigger explore path
```

## commands
Example of parsing/executing slash commands; supports quotes, escapes, `--flag` and `--flag=value`; built-in input script; no API key required.

```bash
go run ./examples/commands
```

## plugins
Demonstrates pkg/plugins TrustStore, manifest loading, and directory scan; validates signatures then prints plugin info.

```bash
go run ./examples/plugins                 # signature verification by default
go run ./examples/plugins -allow-unsigned # allow unsigned manifests
```

Default scan path `examples/plugins`; no API key required.

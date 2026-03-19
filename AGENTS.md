# AGENTS.md

Guidance for agents working in this repository.

> NOTE (v2): Treat `docs/refactor/PRD.md` and `docs/refactor/ARCHITECTURE-v2.md` as the ground truth.

## Ground Truth

- `docs/refactor/PRD.md` — Stories / FR / Acceptance Matrix
- `docs/refactor/ARCHITECTURE-v2.md` — package graph, flows, invariants

## Code Map (v2)

- `pkg/api/` — Runtime + agent loop + compaction + streaming (`pkg/api/agent.go`)
- `pkg/model/` — single `model.Model` interface + providers (`pkg/model/interface.go`)
- `pkg/tool/` — registry/executor + built-ins (`pkg/tool/builtin/`)
- `pkg/middleware/` — 4 stages: `BeforeAgent`, `BeforeTool`, `AfterTool`, `AfterAgent` (`pkg/middleware/types.go`)
- `pkg/hooks/` — 7 events + safety hook (`pkg/hooks/types.go`, `pkg/hooks/safety.go`)
- `pkg/sandbox/` — filesystem/network/resource isolation only (no approval workflow)
- `pkg/config/` — `.agents/` config + rules loaders (`pkg/config/settings_loader.go`, `pkg/config/rules.go`)
- `pkg/message/` — history, converter, trimmer (message processing utilities)
- `pkg/mcp/` — MCP server integration (`pkg/mcp/mcp.go`)
- `pkg/gitignore/` — gitignore matcher used by grep/glob built-ins
- `pkg/runtime/` — skills registry (`pkg/runtime/skills/`) + subagents manager (`pkg/runtime/subagents/`)

## Memory

- Runtime loads `./AGENTS.md` (with `@include` support) at startup and appends it under `## Memory` to the system prompt (`pkg/api/agent.go`).

## Build & Test

```bash
go test ./...
go build ./...
```

## Examples

Examples require API keys (no offline mode).

```bash
go run ./examples/01-basic
go run ./examples/02-cli --session-id demo --prompt "你好"
go run ./examples/03-http
go run ./examples/04-advanced --prompt "安全巡检" --enable-mcp=false
go run ./examples/08-safety-hook
go run ./examples/09-compaction
```

Online (Anthropic-compatible) endpoints can be configured via env vars:

```bash
export ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic
export ANTHROPIC_API_KEY=...
go run ./examples/03-http --serve --model deepseek-chat
```

## Configuration

```
.agents/
├── settings.json         # project config
├── settings.local.json   # local overrides (gitignored)
├── rules/                # rules (markdown)
├── skills/               # skills
└── agents/               # subagents
```

Notes:
- Settings are loaded from `~/.agents/` and `./.agents/` (no legacy config-dir fallback).
- Use `settings.permissions.additionalDirectories` to widen filesystem roots for sandboxed tools.
- Use `settings.disallowedTools` to disable built-in tools by name.

## Safety Model (v2)

- `pkg/hooks/safety.go` blocks catastrophic `bash` patterns before user shell hooks.
- Disable via `api.Options{DisableSafetyHook: true}`.
- Sandbox (`pkg/sandbox`) is isolation only; it must not become a permission/approval system.

## Docs

- Ground truth: `docs/refactor/PRD.md`, `docs/refactor/ARCHITECTURE-v2.md`
- Legacy (v1, historical only): `docs/architecture.md`, `docs/api-reference.md`

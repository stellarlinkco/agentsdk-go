# API Reference (v2)

This repository recently underwent a v2 simplification refactor. The canonical technical spec is:

- `docs/refactor/PRD.md`
- `docs/refactor/ARCHITECTURE-v2.md`

For API details, prefer reading the Go types directly:

- Runtime: `pkg/api/agent.go`
- Options / Request / Response: `pkg/api/options.go`
- Model interface: `pkg/model/interface.go`
- Tools and built-ins: `pkg/tool/`, `pkg/tool/builtin/`
- Middleware (4 stages): `pkg/middleware/types.go`
- Hooks (7 events) + safety hook: `pkg/hooks/types.go`, `pkg/hooks/safety.go`

Legacy v1 behavior and packages (commands/tasks/security/dual-model) are intentionally removed from v2 core.

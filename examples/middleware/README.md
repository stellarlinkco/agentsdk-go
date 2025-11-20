# Middleware Chain Example

This example builds a runtime with `api.New`, wires four production-style middleware components, and includes a small settings middleware that seeds the request context with values from `.claude/settings.json`. Each middleware uses the six interception points defined in `pkg/middleware/types.go`, demonstrates `middleware.State` handoffs, and mirrors the project bootstrap pattern from `examples/http` so you can run it without additional scaffolding.

## Quick Start

1. Go 1.23+ with the repo checked out.
2. From the repo root run:
   ```bash
   go run ./examples/middleware --prompt "扫描 access.log 并生成摘要"
   ```
3. The program generates a temporary `.claude/settings.json` (official format), passes it into `api.New` via `ProjectRoot`/`SettingsOverrides`, and then prints the final response plus middleware/tool metrics.

`flag -h` lists knobs for the token bucket, concurrency limit, middleware timeout, and simulated tool latency.

## Middleware Modules

| File | Purpose |
| --- | --- |
| `logging.go` | Structured request/response logs, latency tracking, cross-middleware request IDs stored in `State.Values["request_id"]`. |
| `ratelimit.go` | Pure-Go token bucket with concurrency guard; protects `before_agent` and surfaces wait time through shared state. |
| `security.go` | Prompt validation, sensitive term blocking, tool parameter checks, and output review; writes audit notes to `security.flags`. |
| `monitoring.go` | Collects latency metrics, flags slow iterations/tools, and exposes snapshots to `main.go` for reporting. |

The chain order (`logging → ratelimit → security → monitoring`) ensures every middleware can see data written by the previous one. For example, `logging` generates the `request_id`, `security` appends review flags, and `monitoring` reads both values when printing slow-query warnings.

## Custom Middleware

1. Implement the `middleware.Middleware` interface defined in `pkg/middleware/types.go`. It is idiomatic to no-op methods you do not need.
2. Use the provided `middleware.State` to share context. Store simple types (`string`, `time.Time`, counters) under stable keys so downstream middleware can read them safely.
3. Register your middleware when constructing the chain:
   ```go
   chain := middleware.NewChain([]middleware.Middleware{
       newLoggingMiddleware(logger),
       myCustomMiddleware{},
   }, middleware.WithTimeout(2*time.Second))
   ```
4. Prefer short, defensive logic that fails fast—middleware errors bubble out of the agent loop immediately, so clear messages are essential.

## Frequently Asked Questions

**Q: Do I need a real model provider?**  
No. `main.go` ships with a deterministic `demoModel` and `demoToolbox` so the pipeline runs offline. Swap in `pkg/model` providers when you are ready to connect to Anthropic or other backends.

**Q: How do I share data between middleware safely?**  
Stick to immutable values or simple structs inside `middleware.State.Values`. This example stores `request_id`, security flags, and timing markers so later middleware can report enriched metrics.

**Q: Can I stream metrics elsewhere?**  
Yes. `monitoring.go` keeps aggregate counters in `metricsRegistry`; replace the in-memory snapshot with a Prometheus exporter or OTEL span if you need external observability.

**Q: What happens if a middleware blocks?**  
`middleware.WithTimeout` enforces a per-hook timeout (set via the `--middleware-timeout` flag). When a middleware exceeds the deadline, the agent surfaces a wrapped error and stops execution, mirroring the behavior in `pkg/middleware/chain.go`.

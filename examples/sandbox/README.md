# Sandbox Policy Example

This example wires the sandbox primitives from `pkg/sandbox` into a small, self-contained program. It exercises the filesystem allowlist, outbound domain filter, and resource limiter, then runs a couple of positive/negative checks so you can see each guard firing.

## Quick Start

1. From the repo root run:
   ```bash
   go run ./examples/sandbox
   ```
   The demo creates a temporary workspace plus an extra shared directory and cleans them up on exit.
2. Override defaults with flags when needed:
   - `--root` – point the filesystem allowlist at an existing directory (temp dir by default)
   - `--allow-host` / `--deny-host` – tweak the domain allow/deny probe targets
   - `--cpu-limit` / `--mem-mb` / `--disk-mb` – resource ceilings enforced by `ResourceLimiter`

No API key or external services are required; the program never dials the network.

## What It Demonstrates

- **FileSystemPolicy**: `FileSystemAllowList` is seeded with the workspace root and an extra shared directory; paths outside those roots (e.g., `../etc/passwd`) are rejected.
- **NetworkPolicy**: `DomainAllowList` permits the configured `--allow-host` (plus `*.svc.local` for wildcard coverage) and blocks the `--deny-host` probe.
- **ResourceLimiter**: `Manager.Enforce` is called twice—once with a steady workload that fits the CPU/memory/disk budgets, and once with an intentional spike that triggers `ErrResourceExceeded`.

The log output is intentionally small so you can see each policy succeed or fail without digging through verbose traces.

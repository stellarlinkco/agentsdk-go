# MCP stdio example

This example shows how to connect the registry to a local `mcp-server-time` process via the MCP stdio transport.

## Prerequisites

- [uv](https://github.com/astral-sh/uv) must be installed and available on `$PATH`.
- Install the reference time server once: `uv tool install mcp-server-time` (or rely on `uvx` to auto-install on first use).

## Quick test

Verify the time server starts correctly:

```bash
uvx mcp-server-time --help
```

If you see the usage output, the binary is ready.

## Run the example

```bash
go run ./examples/mcp
```

The program will:

1. Launch `mcp-server-time` via `uvx` using the stdio transport.
2. Register its MCP tools (e.g., `get_current_time`, `convert_time`).
3. Invoke `get_current_time` for the UTC timezone and pretty-print the JSON response.

Example output:

```
Current time response:
[
  {
    "type": "text",
    "text": "{\n  \"timezone\": \"UTC\",\n  \"datetime\": \"2025-11-17T05:59:51+00:00\",\n  \"day_of_week\": \"Monday\",\n  \"is_dst\": false\n}"
  }
]
```

Use CTRL+C to stop the Go program; it terminates the time server subprocess automatically.

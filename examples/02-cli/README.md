# 02-cli: Interactive REPL

Run a minimal CLI loop that keeps session history and optionally enables MCP servers.

## Requirements

- `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` must be set (e.g., `export ANTHROPIC_API_KEY=sk-...`).
- Optional: `SESSION_ID` seeds `--session-id`; defaults to `demo-session`.

## Basic Usage

```bash
go run ./examples/02-cli --session-id my-session --interactive
```

## Command-line Flags

- `--session-id`: Session identifier to keep chat history (default: `demo-session`)
- `--project-root`: Project root directory (default: `.`)
- `--enable-mcp`: Enable MCP server auto-load from `.agents/settings.json` (default: `false`)
- `--interactive`: Run in interactive REPL mode (default: `false`)

## MCP Behavior

- When `--enable-mcp=true`, the SDK automatically loads MCP servers from `.agents/settings.json` under `--project-root`; no manual wiring is needed.
- To disable MCP for this example, run `go run ./examples/02-cli --enable-mcp=false`.
- To add servers, edit `.agents/settings.json` under `mcp.servers`; the SDK handles spec conversion and registration.

## Tips

- Type `exit` to quit
- Only assistant replies are printed
- MCP servers are loaded from `.agents/settings.json` under the project root
- Use `--project-root` to point at a different project directory (with its own `.agents/`)

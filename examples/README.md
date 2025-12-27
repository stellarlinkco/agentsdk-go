[中文](README_zh.md) | English

# agentsdk-go Examples

Five progressively richer examples. Run everything from the repo root.

**Environment Setup**

1. Copy `.env.example` to `.env` and set your API key:
```bash
cp .env.example .env
# Edit .env and set ANTHROPIC_API_KEY=sk-ant-your-key-here
```

2. Load environment variables:
```bash
source .env
```

Alternatively, export directly:
```bash
export ANTHROPIC_API_KEY=sk-ant-your-key-here
```

**Learning path**
- `01-basic` (32 lines): single API call, minimal surface, prints one response.
- `02-cli` (73 lines): CLI REPL with session history and optional config loading.
- `03-http` (~300 lines): REST + SSE server on `:8080`, production-ready wiring.
- `04-advanced` (~1400 lines): full stack with middleware, hooks, MCP, sandbox, skills, subagents.
- `05-custom-tools` (~150 lines): selective built-in tools and custom tool registration.

## 01-basic — minimal entry
- Purpose: fastest way to see the SDK loop in action with one request/response.
- Run:
```bash
source .env
go run ./examples/01-basic
```

## 02-cli — interactive REPL
- Key features: interactive prompt, per-session history, optional `.claude/settings.json` load.
- Run:
```bash
source .env
go run ./examples/02-cli --session-id demo --settings-path .claude/settings.json
```

## 03-http — REST + SSE
- Key features: `/health`, `/v1/run` (blocking), `/v1/run/stream` (SSE, 15s heartbeat); defaults to `:8080`. Fully thread-safe runtime handles concurrent requests automatically.
- Run:
```bash
source .env
go run ./examples/03-http
```

## 04-advanced — full integration
- Key features: end-to-end pipeline with middleware chain, hooks, MCP client, sandbox controls, skills, subagents, streaming output.
- Run:
```bash
source .env
go run ./examples/04-advanced --prompt "安全巡检" --enable-mcp=false
```

## 05-custom-tools — custom tool registration
- Key features: selective built-in tools (`EnabledBuiltinTools`), custom tool implementation (`CustomTools`), demonstrates tool filtering and registration.
- Run:
```bash
source .env
go run ./examples/05-custom-tools
```
- See [05-custom-tools/README.md](05-custom-tools/README.md) for detailed usage and custom tool implementation guide.

## 05-multimodel — multi-model support
- Key features: model pool configuration, tier-based model routing (low/mid/high), subagent-model mapping, cost optimization.
- Run:
```bash
source .env
go run ./examples/05-multimodel
```
- See [05-multimodel/README.md](05-multimodel/README.md) for configuration examples and best practices.

## 08-askuserquestion — AskUserQuestion tool
- Key features: three independent demo programs showing different aspects of the AskUserQuestion tool.
- Run:
```bash
# Demo 1: Tool-only test (no API key needed)
go run ./examples/08-askuserquestion/demo_simple.go

# Demo 2: LLM integration test (requires API key)
source .env
go run ./examples/08-askuserquestion/demo_llm.go

# Demo 3: Full agent scenarios (requires API key)
source .env
go run ./examples/08-askuserquestion/main.go
```
- **Note**: This directory contains 3 independent programs with their own `main()` functions. Run each file separately, not with `go run .`
- See [08-askuserquestion/README.md](08-askuserquestion/README.md) for detailed usage and implementation patterns.

# Hooks Lifecycle Demo

Minimal end-to-end example showing how to drive the `pkg/core/hooks` executor, attach middleware, and implement all seven lifecycle callbacks (SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Notification, SubagentStop, Stop). The example emits synthetic payloads only—no API keys or external services are needed.

## Run
From the repository root:

```bash
go run ./examples/hooks \
  -prompt "自检沙箱配置" \
  -session hooks-demo \
  -tool log_scan
```

Useful flags:
- `-prompt` — text fed into UserPrompt and tool params (default: "分析日志并生成摘要")
- `-session` — session ID used in SessionStart/Notification payloads
- `-tool` — tool name used for PreToolUse/PostToolUse
- `-tool-latency` — simulated duration injected into PostToolUse
- `-hook-timeout` — per-hook timeout enforced by the executor
- `-dedup-window` — recent event window for de-duplication (second `notify-once` notification is intentionally suppressed)

## What to watch for
- Middleware logs before each hook (`middleware dispatch` / `middleware timing`).
- Hook logs for all seven lifecycle callbacks with their payload fields.
- Final counts map confirming each hook ran once while the duplicate notification was dropped.

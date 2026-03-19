# Safety Hook 示例

本示例演示 v2 的 **Go-native safety hook** 与 `DisableSafetyHook`：
- 默认情况下：在 `PreToolUse` 对 `bash` 做轻量级灾难命令拦截（例如 `rm -rf /`）。
- 可通过 `api.Options.DisableSafetyHook=true` 禁用该拦截。

本示例注册了一个 **伪造的 `bash` 工具**（不会调用系统 `bash`，只回显 `command`），确保不会执行真实危险命令。

注意：为与其它 examples 保持一致，本示例也要求设置 `ANTHROPIC_API_KEY`（或 `ANTHROPIC_AUTH_TOKEN`）。

## 运行

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/08-safety-hook
```

预期输出包含两段：
- Safety hook enabled：输出包含 `hooks: tool execution blocked by safety hook`，且工具不会被执行。
- Safety hook disabled：工具会被执行一次，并回显 `executed: rm -rf /`。

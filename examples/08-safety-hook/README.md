# Safety Hook 示例（离线）

本示例演示 v2 的 **Go-native safety hook** 与 `DisableSafetyHook`：
- 默认情况下：在 `PreToolUse` 对 `bash` 做轻量级灾难命令拦截（例如 `rm -rf /`）。
- 可通过 `api.Options.DisableSafetyHook=true` 禁用该拦截。

为确保离线可运行、且不会执行真实危险命令，本示例注册了一个 **伪造的 `bash` 工具**（不会调用系统 `bash`，只回显 `command`）。

## 运行

```bash
go run ./examples/08-safety-hook
```

预期输出包含两段：
- Safety hook enabled：输出包含 `hooks: tool execution blocked by safety hook`，且工具不会被执行。
- Safety hook disabled：工具会被执行一次，并回显 `executed: rm -rf /`。

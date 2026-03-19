# Refactor ARCHITECTURE (Guardrails + Test Seams)

## 1. 边界与依赖方向（可验证规则）

### 1.1 目录边界

- `cmd/**`：可执行入口（CLI 等）。
- `pkg/**`：SDK 库代码（可被 `cmd/**`、`examples/**` 使用）。
- `examples/**`：演示代码（只能依赖 `pkg/**`，不应被任何生产代码依赖）。
- `test/**`：结构性/闭环测试与测试辅助。

### 1.2 允许/禁止依赖边（阻断规则）

**禁止（必须 fail）：**
- `pkg/**` -> `cmd/**`
- `pkg/**` -> `examples/**`
- `cmd/**` -> `examples/**`

**允许：**
- `cmd/**` -> `pkg/**`
- `examples/**` -> `pkg/**`

> 备注：这是“最小可防御”的 guardrail；不在这里声明的更细粒度分层不作为阻断条件。

## 2. 可测试性 Seams（为 99% 覆盖率服务）

- 对外部边界（HTTP、进程执行、时钟、随机数、环境变量）一律通过**最小 seam**替换：
  - 允许使用 `var` 注入（package-level）或小接口（单方法优先）
  - 禁止为了测试引入大而全的抽象层
- `cmd/cli` 的核心逻辑必须可在测试中以函数方式调用（不通过 `os.Exit` 直接终止测试进程）。

## 3. 闭环测试 slice（默认）

### Slice: `sdk_closed_loop_v1`

入口：
- 直接调用 SDK API（不走真实外部模型网络）

强制检查（blocking）：
- 一次完整 run 成功返回（或可预期失败）
- 至少一次 tool call 被执行且结果被记录/可断言
- 上下文取消/超时路径可覆盖并可断言

证据包：
- 使用 `t.TempDir()` 作为默认落盘目录；若设置 `CLOSED_LOOP_ARTIFACT_DIR` 则写入该目录（便于 CI 收集）。


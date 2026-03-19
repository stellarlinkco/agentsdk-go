# Prompt Compression Compaction 示例

本示例演示 v2 的 compaction 机制：
- 触发条件：当 `history.TokenCount()/TokenLimit >= Threshold`。
- 行为：保留最新 `PreserveCount` 条消息不变；将更早的消息通过一次 **LLM prompt compression** 压缩成 summary。
- 关键点：传给压缩模型的输入会 **剔除 tool-call/tool-result 内容**（不包含 role=tool 消息，也不包含 ToolCalls/结果）。

本示例使用：
- 一个 stub `model.Model`（既响应正常 agent loop，又响应压缩请求）
- 一个伪造的 `bash` 工具（不会执行系统命令）

注意：为与其它 examples 保持一致，本示例也要求设置 `ANTHROPIC_API_KEY`（或 `ANTHROPIC_AUTH_TOKEN`）。

## 运行

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/09-compaction
```

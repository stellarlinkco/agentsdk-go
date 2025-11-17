# agentsdk-go Examples Guide

`examples/` 目录按功能递增组织：先掌握最小化的 `agent.Run()`，再引入 streaming、工具调用，最后扩展到 HTTP Server、checkpoint、MCP 与 workflow。所有示例都可以通过 `go run ./examples/<name>` 直接运行，并共用 `ANTHROPIC_*` 环境变量来初始化模型提供商。

## Quick Start

1. 安装 Go 1.23+ 并进入仓库根目录。
2. 设置最少的 API 凭证（Kimi、Claude 兼容），可按需覆盖 HTTP 端口：

```bash
go env -w GO111MODULE=on
export ANTHROPIC_API_KEY="sk-your-key"
export ANTHROPIC_BASE_URL="https://api.anthropic.com"   # 可选，自定义代理/镜像
export HTTP_SERVER_ADDR=":8080"                         # 可选，HTTP 示例默认 :8080
```

> 其余示例沿用相同环境变量，仅 `examples/mcp` 需要本地或远端 MCP 服务可用（默认假设 `http://localhost:8080`）。

## Learning Path

**基础路线**

1. **basic** → 验证 API 密钥与最小 `agent.Run()`。
2. **simple-stream** → 理解 `RunStream` 事件循环，无工具依赖。
3. **tool-basic** → 学会向 Agent 注册 Bash/File 工具并做同步任务。
4. **tool-stream** → 结合工具与流式事件，观察工具执行过程。
5. **http-simple** → 暴露非流式 HTTP API (`POST /run` + `/health`)。
6. **http-stream** → 在 HTTP 上启用 SSE（`GET /run/stream`）。
7. **http-full** → 生产级 HTTP server（所有内建工具 + 超时 + 优雅退出）。
8. **mcp** → 调用 MCP 远程工具。
9. **stream (deprecated)** → 旧版“一个示例囊括所有”实现，仅供升级参考。

**高级路线（完成基础示例后继续）**

10. **checkpoint (Session 管理)** → 演示 Memory/File Session 的 `Checkpoint/Resume/Fork`、`Filter` 查询与 WAL 回放。
11. **workflow (工作流编排)** → 使用 `workflow.StateGraph` + Action/Decision/Parallel `Node` + `Executor` 表达条件与并行流程。
12. **approval (审批队列)** → 演练 `approval.Queue` 的审批流程、Session 白名单以及 Record/Store 审计，再衔接 `security.ApprovalQueue` 的 TTL 机制。
13. **telemetry (遥测)** → `Telemetry Manager` 统一管理 Tracer/Meter、指标记录与敏感信息 Filter。
14. **model-openai (OpenAI 模型)** → 用 `openai.Provider` + `OPENAI_*` 环境变量替换 Anthropic，并演示流式响应与工具调用。
15. **security (安全沙箱)** → 聚焦 Sandbox、Validator、路径验证与 Allow list 配置，确保工具命令受控。
16. **wal (Write-Ahead Log)** → 展示 WAL 创建、Entry 写入/读取、`Replay` 恢复与 `Truncate` 清理流程。

## Example Matrix

| 顺序 | 目录 | 核心主题 | 快速命令 |
| --- | --- | --- | --- |
| 1 | `basic` | 最小化 `agent.Run()`，附带 Bash/File 工具与 token 统计 | `go run ./examples/basic` |
| 2 | `simple-stream` | `RunStream` 事件迭代，无工具依赖 | `go run ./examples/simple-stream` |
| 3 | `tool-basic` | 同步工具调用（Bash + File） | `go run ./examples/tool-basic` |
| 4 | `tool-stream` | 工具 + 流式事件，观察执行轨迹 | `go run ./examples/tool-stream` |
| 5 | `http-simple` | HTTP `POST /run` + `/health` | `go run ./examples/http-simple` |
| 6 | `http-stream` | HTTP + SSE (`/run/stream`) | `go run ./examples/http-stream` |
| 7 | `http-full` | 完整 HTTP Server（Bash/File/Glob/Grep + 优雅关闭） | `go run ./examples/http-full` |
| 8 | `mcp` | MCP 远端工具调用 | `go run ./examples/mcp` |
| 9 | `stream` | **Deprecated**：旧式流式 + HTTP 混合 | `go run ./examples/stream` |
| 10 | `checkpoint` | Memory/File Session 的 Checkpoint/Resume/Fork + Filter 查询 | `go run ./examples/checkpoint` |
| 11 | `workflow` | StateGraph + Action/Decision/Parallel + Executor | `go run ./examples/workflow` |
| 12 | `approval` | 审批队列、白名单与 Record/Store 审计 + 安全队列 | `go run ./examples/approval` |
| 13 | `telemetry` | Telemetry Manager、指标记录与敏感信息 Filter | `go run ./examples/telemetry` |
| 14 | `model-openai` | OpenAI Provider、流式输出与 Tool Call | `go run ./examples/model-openai` |
| 15 | `security` | Sandbox、Validator、路径校验与 Allow list | `go run ./examples/security` |
| 16 | `wal` | WAL Append/Sync/Replay 与 Truncate 恢复 | `go run ./examples/wal` |

---

### basic
最简单的 `agent.Run()` 例子，用来验证 `ANTHROPIC_API_KEY`、默认模型 `claude-3-5-sonnet-20241022` 以及 Bash/File 工具链。

**主要特性**
- 开箱拉起 Anthropic 模型，并打印基地址 / 模型信息。
- `agent.New` 设置最小运行上下文 (`SessionID`, `WorkDir`, `MaxIterations=1`)。
- 注册 Bash/File 工具后执行 echo 命令，输出 token 使用情况。

**运行命令**
```bash
go run ./examples/basic
```

**适用场景**
- API 密钥连通性与 SDK wiring 自检。
- 为后续示例准备运行时缓存（模型工厂、工具集合）。

### simple-stream
Streaming 入门，不加载任何工具，仅通过 `RunStream` 读取事件并逐条打印。

**主要特性**
- 仍复用 Anthropic 初始化逻辑，保证模型先可用。
- `agent.RunStream` 返回的 channel 逐事件写日志，方便理解事件生命周期。
- 通过 `MaxIterations=1` 控制为一次性流式推理。

**运行命令**
```bash
go run ./examples/simple-stream
```

**适用场景**
- 熟悉事件类型（`type`, `data`）及处理顺序。
- 验证非工具场景下的 streaming 通路。

### tool-basic
在同步 `agent.Run` 中引入 Bash/File 工具，展示多轮（最多 4 次迭代）问题求解。

**主要特性**
- CLI 无 streaming，便于把控工具调用与最终答案。
- 打印 `ToolCalls` 结果与 token 使用，帮助排查开销。
- 典型任务：列出目录 + 读取 `go.mod` 前几行。

**运行命令**
```bash
go run ./examples/tool-basic
```

**适用场景**
- 构建最小“工具+LLM”同步执行链。
- 在 IDE/CI 中演示如何捕获 `agent.Run` 的最终结果。

### tool-stream
将 Bash/File 工具与 `RunStream` 结合，可以实时观察工具调用事件。

**主要特性**
- 同步新增工具注册，但运行路径改为 streaming。
- 日志输出中包含工具计划/完成事件，便于调试长任务。
- `MaxIterations=4` 防止无限循环。

**运行命令**
```bash
go run ./examples/tool-stream
```

**适用场景**
- 调试工具调用卡顿或错误；通过实时事件锁定阶段。
- 构建需要边执行边反馈的 CLI／TUI。

### http-simple
最基础 HTTP Server，只暴露同步 `POST /run` 与健康检查。

**主要特性**
- 使用 `server.New(ag)` 装配 Router，并手写 `/health`。
- 仍注册 Bash/File 工具，方便远程执行简单命令。
- 监听 `:8080`（可用 `HTTP_SERVER_ADDR` 覆盖）。

**运行命令**
```bash
go run ./examples/http-simple &
curl -s -X POST http://localhost:8080/run \
  -H 'Content-Type: application/json' \
  -d '{"input":"hello"}'
```

**适用场景**
- 演示如何在自研后端里嵌入 agentsdk-go。
- 实现最小的健康检查 / 远程触发接口。

### http-stream
在 HTTP Server 上同时提供同步与 SSE streaming 能力。

**主要特性**
- `server.New` 默认挂载 `POST /run` + `GET /run/stream`。
- 额外注册 Glob 工具，便于列目录模式匹配。
- README 中提供 `curl -N` 示例以观察 SSE。

**运行命令**
```bash
go run ./examples/http-stream &
curl -s -X POST http://localhost:8080/run -d '{"input":"demo"}'
curl -N http://localhost:8080/run/stream?input=hello
```

**适用场景**
- Web IDE 或前端需要实时 token feed。
- 比较同步响应与 SSE 的负载差异。

### http-full
生产级 HTTP 例子：注册所有内建工具并启用超时、优雅关闭与可配置端口。

**主要特性**
- 通过 `HTTP_SERVER_ADDR` 自定义监听地址，默认 `:8080`。
- 注册 Bash/File/Glob/Grep，全量工具能力。
- 配置 `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout` 等服务器参数。
- 捕获 `SIGINT/SIGTERM`，使用 `http.Server.Shutdown` 优雅退出。

**运行命令**
```bash
export HTTP_SERVER_ADDR=":9090"
go run ./examples/http-full &
curl -s -X POST http://localhost:9090/run -d '{"input":"ls"}'
curl -N http://localhost:9090/run/stream?input=plan
curl -s http://localhost:9090/health
```

**适用场景**
- 生产环境或需要 SLA 的 PoC。
- 研究如何挂载更多工具、配置 server 超时、实现 graceful shutdown。

### checkpoint
MemorySession 与 FileSession 组合展示完整的 Session 管理闭环，涵盖 `Checkpoint/Resume/Fork`、`Filter` 查询以及 WAL 同步，让你能在 CLI 中复现实际会话分叉与恢复。

**主要特性**
- `session.NewMemorySession` 依次 `Append` 三段对话后创建命名 checkpoint，持续输出日志便于追踪。
- 主线继续写入并 `Fork` 子分支，随后使用 `Filter{Role:"user"}`、`List`/`printTranscript` 观察不同分支视图。
- `Resume` 回滚到 checkpoint 后再次打印时间线，验证恢复行为是否符合预期。
- `session.NewFileSession` 借助临时目录 WAL 将内存转录 `replayTranscript` 到磁盘，并重复 `Checkpoint/Resume/Fork`，覆盖落盘、恢复和子会话复制。

**运行命令**
```bash
go run ./examples/checkpoint
```

**适用场景**
- 为 Memory/File Session 扩展持久化或回溯功能前先做沙箱试验。
- 定位 `Checkpoint/Resume/Fork` 时序问题，利用详细日志复现现场。
- 构建面向运营的“分支/重来”界面前，验证底层 API 组合。

### mcp
展示如何使用 `tool.Registry` 调用远程 MCP 服务器提供的工具。

**主要特性**
- `tool.NewRegistry()` 动态注册远端 MCP server（默认 `http://localhost:8080`）。
- 通过 `Registry.Execute` 直接调用远端 `echo` 工具，并在 5s 上下文超时内获取输出。
- 与 Agent 解耦，单测/调试 MCP 工具时很实用。

**运行命令**
```bash
# 先确保 MCP server 运行在 http://localhost:8080
go run ./examples/mcp
```

**适用场景**
- 在没有 Agent 的场合单独验证 MCP 工具 schema/权限。
- 将外部系统暴露为工具后，先用本示例冒烟。

### workflow
以采购审批为例构建完整的 `workflow.StateGraph`，通过 Action/Decision/Parallel 节点表达条件分支，并用 `workflow.Executor` 与自定义 trace 还原执行路径。

**主要特性**
- `workflow.NewGraph` 注册 collect → route → manual/auto → notify_all → summarize 节点，并使用 `AddTransition` 明确定义跳转关系。
- `workflow.ExecutionContext` 存储 `ctxKeyRequest` 与 `ctxKeyScore`，Decision 节点依据风险分数或优先级决定走人工或自动路径。
- `workflow.NewParallel` fan-out 到多个通知节点，闭包 `makeNotifier` 让重复逻辑得以复用。
- 自定义 trace 结构记录步骤、resolution 与备注，在 `summarize` 节点统一输出执行摘要。

**运行命令**
```bash
go run ./examples/workflow
```

**适用场景**
- 在接入 Agent 前独立验证状态图、条件与并发逻辑是否正确。
- 为审批/运营链路设计多角色协同、通知 fan-out 与执行摘要。
- 编写 workflow 节点单测或 benchmark，提前发现长链路瓶颈。

### approval
围绕 `pkg/approval` 与 `pkg/security` 搭建审批队列演示，涵盖 Record/Store 持久化、白名单自动放行以及 TTL 审批窗口。

**主要特性**
- `approval.NewQueue` + `approval.NewMemoryStore` 生成 `Record`，`Request`/`Pending`/`Approve`/`Reject` 全流程都有日志可追踪。
- `approval.NewWhitelist` 依据 session + tool + params 记忆历史，重复触发时 `auto=true` 表示命中白名单。
- `store.Query` 使用 `approval.Filter` 导出完整的 Record/Store 审计轨迹（时间戳、决策、评论）。
- `security.NewApprovalQueue` 将审批持久化到 JSON，支持 TTL 白名单、`ListPending`、`IsWhitelisted` 与 `Deny` 等安全场景。

**运行命令**
```bash
go run ./examples/approval
```

**适用场景**
- 为 Bash/File 等高危工具增加人工审批、白名单与审计链路。
- 在实现管理端 UI 或 webhook 前，用 CLI 验证审批策略与注释格式。
- 尝试不同 TTL 配置或多 Session 审批流程，观察自动放行行为。

### telemetry
Telemetry Manager 示例演示如何统一注入 OTEL Tracer/Meter、记录请求/工具指标，并用 Filter 掩蔽敏感输入。

**主要特性**
- `telemetry.NewManager` 配置 service/name/version/environment，并指定 `Filter` 的掩码与正则模式。
- `mgr.MaskText`、`SanitizeAttributes` 确保类似 `customer-id`、API key 的内容不会落在日志或 Span 属性中。
- `StartSpan`/`EndSpan` 包裹一次推理流程，`RecordRequest` 记录 latency + error，`RecordToolCall` 为工具调用计数。
- 示例在退出前调用 `mgr.Shutdown`，确保 exporter flush 完成。

**运行命令**
```bash
go run ./examples/telemetry
```

**适用场景**
- 把 agentsdk 指标接入 OTEL Collector/Prometheus 前完成本地冒烟测试。
- 验证 Filter 规则是否覆盖命名模式，避免敏感信息外泄。
- 观察不同运行配置下的时延/错误基线，指导容量规划。

### model-openai
用 `openai.Provider` 替换默认 Anthropic，完整展示同步、流式与 Tool Call 的调用方式，方便多模型共存与迁移。

**主要特性**
- 通过 `OPENAI_API_KEY/OPENAI_BASE_URL` 初始化 `modelpkg.Factory`，并在 `cfg.Extra.tools` 中注册 `lookup_weather` 函数。
- `callModel` helper 重用 `model.Generate`，对比 system prompt、user prompt 与返回内容，便于迁移时查差异。
- `model.GenerateStream` 将增量 token 打印到 stdout，示范如何侦听 `Final` 事件。
- 若响应包含 `ToolCalls`，示例会输出 name/ID/args，并提示需要以 `role=tool` 回复后再调用 `Generate` 收尾。

**运行命令**
```bash
OPENAI_API_KEY=sk-your-key go run ./examples/model-openai
```

**适用场景**
- 将现有 agentsdk-go 工程从 Anthropic 切换到 OpenAI，或实现多供应商 fallback。
- 验证 Function Calling schema 与 SDK 序列化结构是否一致。
- 讲解不同 provider 在同步/流式路径下的行为差异。

### security
聚焦 `pkg/security` 的 Sandbox、PathResolver 与 Validator，不依赖模型即可演练最小安全基线。

**主要特性**
- `security.NewSandbox` 绑定临时工作目录，所有路径/命令校验都会打印 PASS/BLOCKED，方便肉眼核对。
- `PathResolver` 对比合法路径与 `../` 越权路径，展示其如何在入 sandbox 前即阻断逃逸。
- `security.NewValidator` 以及 `Sandbox.ValidateCommand` 对危险命令（如 `rm --no-preserve-root /`）立即拒绝。
- `Sandbox.Allow` 动态加入额外前缀（如共享缓存目录），并再次验证通过，示范可扩展的 allow list。

**运行命令**
```bash
go run ./examples/security
```

**适用场景**
- 为 Bash/File/Glob 工具设置路径白名单、命令黑名单前的快速演练。
- 评估 CI/Runner 或多租户环境中的文件隔离策略。
- 搭建安全审计或管理员 UI 之前的概念验证。

### wal
演示 `pkg/wal` 的 Write-Ahead Log 生命周期：追加、fsync、重启恢复与截断，帮助理解 SDK 的持久化基线。

**主要特性**
- `wal.Open` 配合 `wal.WithSegmentBytes(512)` 强制频繁滚动 segment，更容易观察文件切换。
- 循环 `Append` 业务事件后立即 `Sync`，记录 `wal.Position` 用于恢复校验。
- 关闭后重新 `Open` 并执行 `Replay`，逐条比对 payload，确保崩溃恢复可行。
- `Truncate` 剪裁旧记录，再 `Replay` + `printSegments` 查看压缩后的 segment 列表与大小。

**运行命令**
```bash
go run ./examples/wal
```

**适用场景**
- 理解 session.FileSession、审批队列等组件背后的 WAL 工作方式。
- 调参与评估不同 segment 大小带来的 IO/磁盘成本。
- 构建自定义事件溯源或持久化层前的实验平台。

### stream (Deprecated)
历史遗留示例，同时包含 streaming + HTTP server。代码仍可运行，但推荐迁移到上方拆分后的专用示例。

**主要特性**
- 仍注册 Bash/File/Glob/Grep 并靠 `RunStream` 演示事件流。
- 同进程启动 HTTP server（`POST /run`, `GET /run/stream`, `/health`）。
- 启动时打印显式的弃用提示，帮助定位遗留脚本。

**运行命令**
```bash
go run ./examples/stream &
curl -s -X POST http://localhost:8080/run -d '{"input":"demo"}'
curl -N http://localhost:8080/run/stream?input=hello
```

**适用场景**
- 协助迁移旧脚本：对照输出，拆分到 `http-*` 或 `tool-*` 示例。
- 快速回归已有的集成测试，直到完成迁移。

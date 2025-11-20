中文 | [English](README.md)

# agentsdk-go Examples

以下示例均可在仓库根目录运行。

**修复说明**
- Sandbox 资源限制（CPU/内存/磁盘）默认生效，避免工具过度消耗。
- historyStore 使用 LRU，`MaxSessions` 默认 500，防止长时间运行造成内存泄漏。

## settings.json
所有示例改用官方 `.claude/settings.json`。最小示例如下（仓库已放置副本于 `examples/middleware/.claude/settings.json`）：
```json
{
  "permissions": {
    "allow": [
      "Bash(ls:*)",
      "Bash(pwd:*)",
      "Read(**/*.go)"
    ]
  },
  "env": {
    "EXAMPLE_VAR": "value"
  },
  "sandbox": {
    "enabled": false
  }
}
```

## cli
最小化 CLI 运行流程，直接调用 Anthropic Provider。SDK 现改用 `.claude/settings.json`（官方格式）；请将 `AGENTSDK_PROJECT_ROOT` 指向已包含该文件的目录，或按 `examples/middleware/.claude/settings.json` 的示例创建一个最小配置。

```bash
export ANTHROPIC_API_KEY=sk-...
go run ./examples/cli
```

环境变量：
- 必需：`ANTHROPIC_API_KEY`
- 可选：`AGENTSDK_PROJECT_ROOT`（指向已有 `.claude` 的目录时跳过临时配置）、`ANTHROPIC_BASE_URL`（代理/镜像）、`HTTP_PROXY`/`HTTPS_PROXY`

## http
标准库实现的 HTTP API（/v1/run、/v1/run/stream、/v1/tools/execute），默认依赖 Sandbox 与 LRU Session 限制。

```bash
export ANTHROPIC_API_KEY=sk-...
export AGENTSDK_HTTP_ADDR=":8080"           # 可选
export AGENTSDK_MAX_SESSIONS=500            # LRU 上限，防止内存泄漏
curl -s http://localhost:8080/health || true
```

核心环境变量：
- 模型：`ANTHROPIC_API_KEY`（必需），`ANTHROPIC_BASE_URL`（代理/镜像可选）
- 基础：`AGENTSDK_HTTP_ADDR`，`AGENTSDK_PROJECT_ROOT`，`AGENTSDK_SANDBOX_ROOT`，`AGENTSDK_MODEL`（目标目录需已有 `.claude/settings.json`）
- 网络：`AGENTSDK_NETWORK_ALLOW`（逗号分隔白名单，默认 `api.anthropic.com`）
- 超时：`AGENTSDK_DEFAULT_TIMEOUT`，`AGENTSDK_MAX_TIMEOUT`
- 资源：`AGENTSDK_RESOURCE_CPU_PERCENT`，`AGENTSDK_RESOURCE_MEMORY_MB`，`AGENTSDK_RESOURCE_DISK_MB`，`AGENTSDK_MAX_BODY_BYTES`，`AGENTSDK_MAX_SESSIONS`

## mcp
演示通过 stdio 连接 `mcp-server-time` 并调用 MCP 工具。

```bash
uv tool install mcp-server-time  # 如未安装
uvx mcp-server-time --help       # 验证可用
go run ./examples/mcp
```

环境要求：`uv`/`uvx` 在 PATH 中，无需 API 密钥。

## hooks
纯本地演示 hooks 执行器的 7 个生命周期回调及中间件日志，含去重与超时控制，无需 API Key。

```bash
go run ./examples/hooks -prompt "自检沙箱配置" -session hooks-demo -tool log_scan
```

可调参数：`-hook-timeout`、`-dedup-window`、`-tool-latency`。

## sandbox
接线文件系统/网络/资源三重 sandbox 策略，先跑通过路径再触发超限，默认创建临时工作区，无需 API Key。

```bash
go run ./examples/sandbox
```

可选标志：`--root`、`--allow-host`/`--deny-host`、`--cpu-limit`/`--mem-mb`/`--disk-mb`。

## skills
演示 runtime/skills 的注册、自动激活（匹配器+优先级/互斥组）与手动执行。

```bash
go run ./examples/skills \
  -prompt "分析生产日志发现异常 SSH 尝试" \
  -env prod -severity high \
  -channels cli,slack -traits sre,security
```

关键标志：`-manual-skill`、`-timeout`。

## subagents
runtime/subagents 最小用法：根据 matcher 自动挑选子代理，或强制派发目标，无需 API Key。

```bash
go run ./examples/subagents                      # 自动选择
go run ./examples/subagents -target plan         # 强制派发
go run ./examples/subagents -prompt "scan logs"  # 触发 explore 路径
```

## commands
解析/执行斜杠命令的示例，支持引号、转义、`--flag` 与 `--flag=value`，输入脚本内置，无需 API Key。

```bash
go run ./examples/commands
```

## plugins
展示 pkg/plugins 的 TrustStore、清单加载与目录扫描，校验签名后输出插件信息。

```bash
go run ./examples/plugins                 # 默认校验签名
go run ./examples/plugins -allow-unsigned # 允许未签名清单
```

默认扫描 `examples/plugins`，无需 API Key。

# 从旧版本升级到 v2：不兼容变更清单

> Ground truth: `docs/refactor/PRD.md` / `docs/refactor/ARCHITECTURE-v2.md`。  
> 本文面向“已有集成代码”的升级场景：你把依赖版本（或分支）切到 v2 重构后，哪些点会直接编译/运行失败，以及应该怎么改。

## TL;DR（先跑起来）

1. **配置目录只认 `.agents/`**：把项目内的配置/规则/技能迁到 `<project>/.agents/`（无 `.claude/` fallback）。见 `pkg/config/settings_loader.go:65`、`pkg/config/rules.go:41`、`pkg/runtime/skills/loader.go:160`。
2. **没有 offline mode**：examples 和线上模型调用必须提供 API key；缺 key 会直接报错，不再静默回退。见 `docs/refactor/PRD.md:57`、`examples/01-basic/main.go:64`。
3. **权限/审批工作流从 core 移除**：v2 是 YOLO-default + safety hook + sandbox；不实现 ask/approval queue。见 `docs/security.md:26`、`pkg/hooks/safety.go`。
4. **大量包/工具被删除**：ACP、tasks、webfetch/websearch、todo_write、slashcommand 等不再存在（需要你自己以 custom tool / 外部服务补回来）。见 `docs/refactor/PRD.md:279`。
5. **核心 API 面向 `pkg/api` + 单一 `model.Model`**：模型接口是 `Complete/CompleteStream`。见 `pkg/model/interface.go:93`。

## 破坏性变更（按“会怎么炸”排序）

### 1) 配置目录：`.claude/` ➜ `.agents/`（无回退）

**现象**
- 你原来按文档/习惯把配置放在 `.claude/`，升级后“看起来加载了但不生效”或直接找不到配置。

**v2 规则**
- Settings：`<project>/.agents/settings.json` 与 `<project>/.agents/settings.local.json`（以及 `~/.agents/...`）见 `pkg/config/settings_loader.go:65`。
- Rules：`<project>/.agents/rules/*.md` 见 `pkg/config/rules.go:41`。
- Skills：`<project>/.agents/skills/<name>/SKILL.md` 见 `pkg/runtime/skills/loader.go:160`。

**你需要做的**
- 把旧目录整体迁移（示例）：
  - `.claude/settings.json` ➜ `.agents/settings.json`
  - `.claude/settings.local.json` ➜ `.agents/settings.local.json`
  - `.claude/rules/` ➜ `.agents/rules/`
  - `.claude/skills/` ➜ `.agents/skills/`

### 2) Offline mode 移除：examples/模型调用必须有 key（不再静默回退）

**现象**
- 你以前可以“不配 key 跑 demo / 跑 tests”，升级后 examples 直接退出并报 `... API_KEY ... is required`。

**v2 规则**
- PRD 明确：examples 必须需要 API keys（无 offline mode / 无 silent fallback）。见 `docs/refactor/PRD.md:57`。
- 示例代码会显式检查环境变量并报错：见 `examples/01-basic/main.go:64`、`examples/03-http/main.go:159`、`examples/10-hooks/main.go:58`。

**你需要做的**
- CI/本地运行 examples：提供 `ANTHROPIC_API_KEY`（或旧的 `ANTHROPIC_AUTH_TOKEN`），以及 `examples/11-reasoning` 需要 `DEEPSEEK_API_KEY`（见 `examples/11-reasoning/main.go:22`）。
- 如果你需要“离线可测试”：在你自己的测试里注入 fake `model.Model`（参考 `examples/internal/demomodel/EchoModel` 和各 `*_test.go`）。

### 3) 权限/审批（ask/approval queue）不再是 core 能力

**现象**
- 你依赖“工具调用前询问/审批/记录”的内建机制，升级后找不到对应 API / 配置项不再生效。

**v2 规则**
- v2 只提供：
  - **Sandbox**：隔离（文件系统/网络/资源），不做 approval workflow。见 `docs/security.md:11`、`pkg/sandbox/`。
  - **Safety hook**：Go-native `PreToolUse` 兜底拦截灾难性 bash。见 `docs/security.md:5`、`pkg/hooks/safety.go`。
- `.agents/settings.json` 仍接受 `permissions` 字段是为了兼容，但 **不实现 ask/approval 工作流**。见 `docs/security.md:26`。
- `permissions.additionalDirectories` 仍会影响 sandbox roots。见 `pkg/api/sandbox_bridge.go:66`。

**你需要做的**
- 把审批/交互确认逻辑迁到你自己的基础设施或 hook（`hooks.ShellHook` + 外部决策服务）。
- 纯“禁用某些工具”：用 `disallowedTools`（settings 或 `api.Options`）。见 `pkg/api/tool_registration.go`、`pkg/config/settings_types.go:18`。

### 4) 内置工具裁剪：只保留 7 个 core tools

**现象**
- 你原来依赖 `webfetch/websearch/task*/todo_write/...`，升级后工具不存在或不会注册。

**v2 规则**
- core tools：`bash/read/write/edit/glob/grep/skill`（目录见 `pkg/tool/builtin/`）。
- 其它工具（tasks/web/commands/approval 等）从 core 移除（PRD）。见 `docs/refactor/PRD.md:279`。

**你需要做的**
- 需要 web 能力：自己注册 custom tool（`api.Options.CustomTools` 或直接 `api.Options.Tools`），或在你的宿主应用层做网络代理。
- 需要任务系统/ACP：在你自己的产品层实现（v2 core 不提供）。

### 5) 包与 API 面：v2 是 big-bang rewrite（不保证旧包名/旧类型仍存在）

**现象**
- `go build` 直接报 import error：某些 `pkg/*` 子包不见了，或类型/函数签名变化。

**v2 规则**
- 主要入口是 `pkg/api`（运行时/agent loop）+ `pkg/model`（单一 `model.Model`）。见 `pkg/model/interface.go:93`。
- 已删除（示例）：`pkg/acp`、`pkg/runtime/tasks`、`todo_write` 等（PRD）。见 `docs/refactor/PRD.md:382`。

**你需要做的**
- 先做“最小编译通过”的切换：把旧 import 对应到 v2 的包图（见 `docs/refactor/ARCHITECTURE-v2.md` 的 package graph）。
- 对外扩展点优先用：`api.Options`、`middleware.Middleware`（4 个 stage）、`tool.Tool`、`hooks.ShellHook`。

### 6) CLI 行为与 flags 变化（如果你 embed 了 CLI）

**现象**
- 你包装/调用 `cmd/cli` 的参数集，升级后 flag 名称/语义对不上。

**v2 CLI（当前）**
- `--project`：project root（默认 `.`）见 `cmd/cli/main.go:37`。
- `--agents`：可选，显式指定 `.agents/` 目录路径（会拼 `settings.json`）见 `cmd/cli/main.go:44`。
- `--mcp`：重复注册 MCP server（依旧支持）。见 `cmd/cli/main.go:53`。

## 升级后的“回归验证”建议（可复制粘贴）

```bash
go test -count=1 ./...
go test -race -count=1 ./...
golangci-lint run

# examples：无 key 应该明确失败（符合 no-offline）
env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN go run ./examples/01-basic
```


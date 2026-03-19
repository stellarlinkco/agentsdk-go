# Changelog

> NOTE (v2): This repository has undergone a v2 simplification refactor. Entries below may refer to removed v1 packages/tools (commands/tasks/security and legacy tool names like `file_read`). For v2 ground truth, see `docs/refactor/PRD.md` and `docs/refactor/ARCHITECTURE-v2.md`.

All notable changes to agentsdk-go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.2] - 2025-12-26

### Fixed
- **Environment Variables Override APIKey (#30)**: Fixed critical bug where `ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_API_KEY` environment variables would override explicitly configured `APIKey` in `AnthropicConfig`
  - Unified priority across `Provider` and direct construction: configured APIKey → `ANTHROPIC_AUTH_TOKEN` → `ANTHROPIC_API_KEY`
  - Removed duplicate auth injection following KISS principle (`pkg/model/anthropic.go`, `pkg/model/provider.go`)
  - Enhanced test coverage for APIKey priority and TrimSpace behavior (`pkg/model/anthropic_test.go`)
- **CI Stability**: Downgraded `golangci-lint-action` from v6 to v4 to avoid schema validation timeout in CI environment

### Changed
- Single source of truth for auth injection via `requestOptions()` method
- Consistent behavior across all code paths

### Testing
- All tests passing ✅
- Coverage: 92.6% ✅
- 34 test cases including new priority validation tests
- Backward compatible for users relying on environment variables only

---

## [0.5.1] - 2025-12-25

### Breaking Changes
- **Skill Name Validation**: Now strictly enforces lowercase alphanumeric characters and hyphens only
  - Removed support for underscores in skill names (e.g., `log_summary` → `log-summary`)
  - Directory scanning limited to one level: `skills/<name>/SKILL.md`
- **Support File Structure**: Changed from `templates/` to `assets/`, added `references/` directory
- **Progressive Disclosure**: Support files now return index only, not full content

### Added
- **Extended SkillMetadata**: Added `license`, `compatibility`, and `metadata` fields
- **ToolList Type**: Supports both YAML string and sequence formats for flexible tool definitions
- **Comprehensive Validation**: Full validation for all Agent Skills Specification requirements
- **100% Specification Compliance**: Fully compliant with Claude Agent Skills Specification

### Improved
- Test coverage: 90.7% for `pkg/runtime/skills`
- 49 tests all passing
- 6 new test cases covering specification edge cases
- Enhanced error messages with specification guidance

---

## [0.5.0] - 2025-12-24

### Changed
- **Context Compression**: Updated to use Claude Code official context compression prompt for improved quality and consistency (`pkg/api/compact.go`)

---

## [0.4.0] - 2025-12-12

### Added
- **Hooks System Extension (#11)**: Added 4 new hook event types - `PermissionRequest`, `SessionStart/End`, `SubagentStart/Stop`, and enhanced `PreToolUse` with input modification support (`pkg/hooks/`)
- **Multi-model Support (#12)**: Implemented subagent-level model binding via `api.ModelFactory` interface, allowing different models for different subagents (`pkg/api/`, `pkg/model/`)
- **Token Statistics (#13)**: Added comprehensive token usage tracking with `TokenStats` struct, automatic accumulation across turns (`pkg/model/`, `pkg/api/`)
- **Auto Compact (#14)**: Implemented automatic context compression when token threshold reached, using configurable compactor model (`pkg/api/compact.go`)
- **Async Bash (#15)**: Added `background` parameter to bash tool for non-blocking command execution (`pkg/tool/builtin/bash.go`)
- **DisallowedTools (#15)**: New `DisallowedTools` configuration to block specific tools at runtime (`pkg/config/`, `pkg/api/`)
- **Rules Configuration (#16)**: Support for `.agents/rules/` directory with markdown rules loading (`pkg/config/rules.go`)
- **OpenTelemetry Integration (#17)**: Added distributed tracing support with span propagation (`pkg/api/otel.go`)
- **UUID Tracking (#17)**: Request-level UUID tracking for observability (`pkg/api/`)

### Changed
- **BREAKING**: `api.Options.ModelFactory` is now a function `func(ctx context.Context) model.Model` instead of direct model instance
- Hooks now run as shell commands (stdin JSON payload) instead of in-process interfaces

### Documentation
- Added `docs/trace-system.md` for OpenTelemetry setup guide
- Updated AGENTS.md with new feature documentation

---

## [0.3.0] - 2025-01-26

### Added
- **Custom tool registration**: New `api.WithCustomTools()` option allows registering custom tools at runtime without modifying built-in tool registry (`pkg/api/agent.go`)
- Example 05: Custom tools demo showcasing registration and execution (`examples/05-custom-tools/`)

### Changed
- **BREAKING**: Configuration lives under `.agents/` (global: `~/.agents/`, project: `<project>/.agents/`) (`pkg/config/`)
- Documentation: Translated core docs (API reference, getting started, security) to English

### Fixed
- File permission warnings: Changed default file mode from `0o644` to `0o600` for security compliance
- Removed unused `taskTool` variable in tests

### Documentation
- Updated README files to reflect custom tool registration feature
- Consolidated custom tools guide with reduced redundancy
- Added comprehensive examples for tool registration patterns

---

## [0.3.0] - 2025-01-26

### 新增
- **自定义工具注册**：新增 `api.WithCustomTools()` 选项，支持运行时注册自定义工具，无需修改内置工具注册表（`pkg/api/agent.go`）
- 示例 05：自定义工具演示，展示工具注册与执行（`examples/05-custom-tools/`）

### 变更
- **破坏性变更**：配置目录为 `.agents/`（全局：`~/.agents/`，项目：`<project>/.agents/`）（`pkg/config/`）
- 文档：核心文档（API 参考、入门指南、安全指南）翻译为英文

### 修复
- 文件权限警告：将默认文件权限从 `0o644` 改为 `0o600` 以符合安全规范
- 移除测试中未使用的 `taskTool` 变量

### 文档
- 更新 README 以反映自定义工具注册功能
- 精简自定义工具指南，减少冗余内容
- 添加工具注册模式的完整示例

---

## [0.2.0] - 2025-01-XX

### Added
- Initial public release
- Core agent loop with 6-point middleware system
- Built-in tools: bash, file_read, file_write, grep, glob
- MCP integration support
- Configuration hot-reload
- Sandbox security layer

### 新增
- 首次公开发布
- 核心 Agent 循环与 6 点中间件系统
- 内置工具：bash、file_read、file_write、grep、glob
- MCP 集成支持
- 配置热重载
- 沙箱安全层

---

## [0.1.0] - 2025-01-XX

### Added
- Project initialization
- Basic agent architecture

### 新增
- 项目初始化
- 基础 Agent 架构

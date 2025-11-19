# 安全指南

本文档说明 agentsdk-go 的安全机制、配置方法和最佳实践。

## 安全架构

SDK 采用三层防御架构：

1. **沙箱（Sandbox）** - 文件系统和网络访问控制
2. **验证器（Validator）** - 命令和参数验证
3. **审批队列（Approval Queue）** - 人工审批机制

这三层防御与 6 个 Middleware 拦截点配合，在请求处理的关键阶段执行安全检查。

## 沙箱隔离

### 功能

沙箱提供以下隔离能力：

- 文件系统访问控制（路径白名单）
- 符号链接解析（防止路径遍历）
- 网络访问控制（域名白名单）

### 实现位置

- `pkg/sandbox/` - 沙箱管理器
- `pkg/security/sandbox.go` - 沙箱核心实现
- `pkg/security/resolver.go` - 路径解析器

### 配置示例

在 `.claude/config.yaml` 中配置沙箱策略：

```yaml
sandbox:
  enabled: true
  allowed_paths:
    - "/tmp"
    - "./workspace"
    - "/var/lib/agent/data"
  network_allow:
    - "*.anthropic.com"
    - "api.example.com"
```

### 代码示例

```go
import (
    "github.com/cexll/agentsdk-go/pkg/security"
)

// 创建沙箱实例
sandbox := security.NewSandbox(workDir)

// 添加允许的路径
sandbox.Allow("/var/lib/agent/runtime")
sandbox.Allow(filepath.Join(workDir, ".cache"))

// 验证路径访问
if err := sandbox.ValidatePath(targetPath); err != nil {
    return fmt.Errorf("路径访问被拒绝: %w", err)
}

// 验证命令执行
if err := sandbox.ValidateCommand(command); err != nil {
    return fmt.Errorf("命令执行被拒绝: %w", err)
}
```

### 最佳实践

1. 在配置文件中声明所有允许的路径，不要在运行时动态添加
2. 使用绝对路径，避免相对路径带来的歧义
3. 定期审查沙箱配置，移除不再需要的路径
4. 为每个工具执行都调用 `ValidatePath`，不要仅在启动时验证

## 命令验证

### 功能

验证器在命令执行前检查以下内容：

- 危险命令（如 `dd`、`mkfs`、`fdisk`、`shutdown`）
- 危险参数（如 `--no-preserve-root`）
- 危险模式（如 `rm -rf`、`rm -r`）
- Shell 元字符（在 Platform 模式下）
- 命令长度限制

### 实现位置

- `pkg/security/validator.go` - 命令验证器
- `pkg/security/validator_full_test.go` - 验证器测试

### 默认阻止的操作

#### 破坏性命令

- `dd` - 原始磁盘写入
- `mkfs`、`mkfs.ext4` - 文件系统格式化
- `fdisk`、`parted` - 分区编辑
- `shutdown`、`reboot`、`halt`、`poweroff` - 系统电源管理
- `mount` - 挂载文件系统

#### 危险删除模式

- `rm -rf` / `rm -fr` - 递归强制删除
- `rm -r` / `rm --recursive` - 递归删除
- `rmdir -p` - 递归删除目录

#### Shell 元字符（Platform 模式）

- `|`、`;`、`&` - 命令链接
- `>`、`<` - 重定向
- `` ` `` - 命令替换

### 代码示例

```go
import (
    "github.com/cexll/agentsdk-go/pkg/security"
)

// 创建验证器
validator := security.NewValidator()

// 验证命令
if err := validator.Validate(command); err != nil {
    log.Printf("命令被阻止: %v", err)
    return err
}

// 允许 Shell 元字符（仅 CLI 模式）
validator.AllowShellMeta(true)
```

### 自定义验证规则

```go
// 添加自定义禁止命令
validator.BanCommand("kubectl", "集群操作需要审批")
validator.BanCommand("helm", "Helm 操作需要审批")

// 添加自定义禁止参数
validator.BanArgument("--force")
validator.BanArgument("--insecure")

// 添加自定义禁止片段
validator.BanFragment("sudo rm")
```

### 最佳实践

1. 结合 JSON Schema 验证工具参数
2. 在 `BeforeTool` Middleware 中执行命令验证
3. 为被阻止的命令记录审计日志
4. 定期与业务黑名单同步
5. 为高危命令实施强制审批

## 审批队列

### 功能

审批队列提供人工审批机制，支持：

- 审批请求创建和管理
- 会话级白名单（带 TTL）
- 审批决策记录
- 审批事件通知

### 实现位置

- `pkg/security/approval.go` - 审批队列实现
- `pkg/security/approval_test.go` - 审批队列测试

### 代码示例

```go
import (
    "github.com/cexll/agentsdk-go/pkg/security"
)

// 创建审批队列
queue, err := security.NewApprovalQueue("/var/lib/agent/approvals")
if err != nil {
    return err
}

// 创建审批请求
request, err := queue.Request(sessionID, command, []string{path})
if err != nil {
    return err
}

// 检查白名单
if queue.IsWhitelisted(sessionID) {
    // 允许执行
    return executeCommand(command)
}

// 等待审批
return fmt.Errorf("等待审批: %s", request.ID)
```

### 审批决策

```go
// 批准请求（带白名单 TTL）
err := queue.Approve(requestID, approverID, 3600) // 1 小时白名单
if err != nil {
    return err
}

// 拒绝请求
err := queue.Deny(requestID, approverID, "不符合安全策略")
if err != nil {
    return err
}
```

### 最佳实践

1. 审批记录目录需在部署前创建并纳入备份
2. 为审批操作设置最短 TTL，避免永久绕过
3. 审批事件必须写入审计日志
4. 实施审批超时机制，自动拒绝过期请求
5. 限制白名单 TTL 上限，定期重新审批

## Middleware 安全拦截

### 拦截点概览

SDK 提供 6 个拦截点执行安全检查：

1. `BeforeAgent` - 请求验证、限流、黑名单过滤
2. `BeforeModel` - Prompt 注入检测、敏感词过滤
3. `AfterModel` - 输出审查、敏感数据脱敏
4. `BeforeTool` - 工具权限检查、参数验证
5. `AfterTool` - 结果审查、错误日志
6. `AfterAgent` - 审计日志、合规性检查

### BeforeAgent：请求验证

威胁场景：

- 滥用者反复创建会话导致资源耗尽
- 超长 Prompt 触发拒绝服务
- 已知恶意 IP 重放攻击

防护实现：

```go
beforeAgentGuard := middleware.Middleware{
    BeforeAgent: func(ctx context.Context, req *middleware.AgentRequest) (*middleware.AgentRequest, error) {
        // IP 黑名单检查
        if blacklist.Contains(req.RemoteAddr) {
            return nil, fmt.Errorf("IP 地址被阻止: %s", req.RemoteAddr)
        }

        // 限流检查
        if !rateLimiter.Allow(req.SessionID) {
            return nil, fmt.Errorf("请求过于频繁")
        }

        // 输入长度检查
        if len(req.Input) > maxInputLength {
            return nil, fmt.Errorf("输入长度超过限制")
        }

        return req, nil
    },
}
```

### BeforeModel：Prompt 安全

威胁场景：

- Prompt 注入攻击
- 敏感信息泄露
- 控制字符注入

防护实现：

```go
beforeModelScan := middleware.Middleware{
    BeforeModel: func(ctx context.Context, msgs []message.Message) ([]message.Message, error) {
        for _, msg := range msgs {
            content := msg.Content

            // Prompt 注入检测
            if containsInjection(content) {
                audit.Log(ctx, "prompt_injection_detected", content)
                return nil, fmt.Errorf("检测到 Prompt 注入")
            }

            // 敏感信息检测
            if secrets := detectSecrets(content); len(secrets) > 0 {
                audit.Log(ctx, "secrets_in_prompt", secrets)
                return nil, fmt.Errorf("输入包含敏感信息")
            }

            // 敏感词过滤
            msg.Content = filterSensitiveWords(content)
        }

        return msgs, nil
    },
}
```

### AfterModel：输出审查

威胁场景：

- 模型生成危险命令
- 输出包含敏感数据
- 生成恶意 URL

防护实现：

```go
afterModelReview := middleware.Middleware{
    AfterModel: func(ctx context.Context, output *agent.ModelOutput) (*agent.ModelOutput, error) {
        content := output.Content

        // 危险命令检测
        if dangerous := detectDangerousCommand(content); dangerous != "" {
            approvalQueue.Request(sessionID, dangerous, nil)
            return nil, fmt.Errorf("模型建议执行危险命令: %s", dangerous)
        }

        // 敏感数据脱敏
        cleaned := redactSecrets(content)
        if cleaned != content {
            audit.Log(ctx, "model_output_redacted", "secrets_found")
            output.Content = cleaned
        }

        return output, nil
    },
}
```

### BeforeTool：权限检查

威胁场景：

- 未授权工具调用
- 越权参数注入
- 递归调用绕过审批

防护实现：

```go
beforeToolGuard := middleware.Middleware{
    BeforeTool: func(ctx context.Context, call *middleware.ToolCall) (*middleware.ToolCall, error) {
        // 工具存在性检查
        if !toolRegistry.Exists(call.Name) {
            return nil, fmt.Errorf("未知工具: %s", call.Name)
        }

        // 权限检查
        if !rbac.CanInvoke(identity, call.Name) {
            audit.Log(ctx, "unauthorized_tool_call", call.Name)
            return nil, fmt.Errorf("无权限调用工具: %s", call.Name)
        }

        // 参数验证
        if err := validateParams(call); err != nil {
            return nil, fmt.Errorf("参数验证失败: %w", err)
        }

        // 路径验证
        if path, ok := call.Params["path"].(string); ok {
            if err := sandbox.ValidatePath(path); err != nil {
                return nil, fmt.Errorf("路径访问被拒绝: %w", err)
            }
        }

        return call, nil
    },
}
```

### AfterTool：结果审查

威胁场景：

- 工具输出包含敏感信息
- 错误信息泄露内部结构
- 超大输出拖垮系统

防护实现：

```go
afterToolReview := middleware.Middleware{
    AfterTool: func(ctx context.Context, result *middleware.ToolResult) (*middleware.ToolResult, error) {
        // 敏感信息检测
        if secrets := detectSecrets(result.Output); len(secrets) > 0 {
            result.Output = redactSecrets(result.Output)
            audit.Log(ctx, "tool_output_redacted", "secrets_found")
        }

        // 错误信息清理
        if result.Error != nil {
            logSecurityError(ctx, result.Error)
            result.Error = errors.New("工具执行失败")
        }

        // 输出长度限制
        if len(result.Output) > maxOutputLength {
            result.Output = result.Output[:maxOutputLength] + "...(truncated)"
        }

        return result, nil
    },
}
```

### AfterAgent：审计日志

威胁场景：

- 缺少操作审计
- 合规性违规
- 无法追溯安全事件

防护实现：

```go
afterAgentAudit := middleware.Middleware{
    AfterAgent: func(ctx context.Context, resp *middleware.AgentResponse) (*middleware.AgentResponse, error) {
        // 创建审计记录
        record := audit.Entry{
            Timestamp: time.Now().UTC(),
            SessionID: resp.SessionID,
            Input:     resp.Input,
            Output:    resp.Output,
            ToolCalls: resp.ToolCalls,
            Approved:  approvalQueue.IsWhitelisted(resp.SessionID),
            UserID:    getUserID(ctx),
        }

        // 写入审计日志
        if err := audit.Store(record); err != nil {
            log.Printf("审计日志写入失败: %v", err)
            return nil, fmt.Errorf("审计日志记录失败")
        }

        // 合规性检查
        if err := compliance.Check(resp); err != nil {
            audit.Log(ctx, "compliance_violation", err.Error())
            return nil, fmt.Errorf("合规性检查失败: %w", err)
        }

        return resp, nil
    },
}
```

## 部署清单

### 配置检查

部署前必须检查：

- [ ] 沙箱已配置所有必需的允许路径
- [ ] 命令验证器已启用并配置
- [ ] 审批队列存储路径已创建并设置权限
- [ ] 所有 Middleware 拦截点已注册安全处理器
- [ ] Middleware 超时设置小于请求超时
- [ ] 审计日志路径已配置并可写入
- [ ] 网络白名单已配置

### 测试验证

运行安全测试：

```bash
# 安全模块测试
go test ./pkg/security/... -v

# Middleware 安全测试
go test ./pkg/middleware/... -v

# 集成测试
go test ./test/integration/security/... -v
```

### 监控配置

配置以下监控指标：

- `middleware_stage_rejections_total{stage="before_agent"}` - 请求拒绝计数
- `middleware_stage_rejections_total{stage="before_tool"}` - 工具调用拒绝计数
- `approval_queue_pending_total` - 待审批请求数量
- `sandbox_violations_total{type="path"}` - 路径违规计数
- `sandbox_violations_total{type="command"}` - 命令违规计数
- `audit_log_failures_total` - 审计日志写入失败计数

设置告警规则：

- 拒绝率超过阈值
- 审批队列堆积
- 审计日志写入失败
- 沙箱违规激增

## 常见漏洞防护

### 路径遍历

防护方法：

1. 所有路径参数调用 `Sandbox.ValidatePath`
2. 在 `BeforeTool` 阶段重复验证
3. 使用绝对路径，解析符号链接
4. 限制允许的路径前缀

测试验证：

```bash
# 测试路径遍历防护
go test ./pkg/security -run TestSandbox_PathTraversal
```

### Prompt 注入

防护方法：

1. 在 `BeforeModel` 检测注入模式
2. 维护注入特征词表
3. 记录疑似注入到审计日志
4. 对高风险输入启用审批

检测示例：

```go
func containsInjection(input string) bool {
    patterns := []string{
        "ignore previous instructions",
        "ignore above",
        "disregard all",
        "system prompt",
    }

    lower := strings.ToLower(input)
    for _, pattern := range patterns {
        if strings.Contains(lower, pattern) {
            return true
        }
    }
    return false
}
```

### 敏感信息泄露

防护方法：

1. 在 `BeforeModel` 和 `AfterModel` 检测敏感信息
2. 在 `AfterTool` 清理工具输出
3. 使用正则表达式匹配常见模式
4. 保留脱敏前的数据到加密存储

检测模式：

```go
var secretPatterns = []*regexp.Regexp{
    regexp.MustCompile(`sk-[a-zA-Z0-9]{48}`),              // API Keys
    regexp.MustCompile(`[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9]{4}`), // Credit Cards
    regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),             // GitHub Tokens
    regexp.MustCompile(`xox[baprs]-[a-zA-Z0-9-]+`),       // Slack Tokens
}
```

### 命令注入

防护方法：

1. 使用 `Validator.Validate` 验证所有命令
2. 禁止 Shell 元字符（Platform 模式）
3. 使用参数化执行而非字符串拼接
4. 限制命令长度

### 权限提升

防护方法：

1. 在 `BeforeTool` 实施 RBAC 检查
2. 审批高权限操作
3. 限制递归调用深度
4. 记录所有权限检查决策

## 安全事件响应

### 检测

Middleware 返回错误时触发告警：

```go
if err != nil {
    alert.Send(alert.SecurityEvent{
        Stage:     "before_tool",
        Error:     err.Error(),
        SessionID: sessionID,
        Timestamp: time.Now(),
    })
}
```

### 控制

启用全局审批要求：

```go
// 撤销所有白名单
approvalQueue.RevokeAll()

// 启用强制审批
approvalQueue.SetGlobalApprovalRequired(true)
```

### 分析

导出审计日志进行分析：

```bash
# 导出最近 1 小时的审计日志
audit-export --since 1h --output /tmp/audit.json

# 查找异常模式
audit-analyze /tmp/audit.json --detect-anomalies
```

### 恢复

1. 修补安全检测逻辑
2. 运行回归测试
3. 逐步恢复服务
4. 监控异常指标

### 复盘

1. 记录事件时间线
2. 分析根本原因
3. 更新安全配置
4. 完善检测规则
5. 更新文档

## 最佳实践

### 开发阶段

1. 默认启用所有安全检查
2. 为每个工具定义 JSON Schema
3. 在单元测试中覆盖安全场景
4. 使用静态分析工具检查代码

### 部署阶段

1. 使用配置文件管理安全策略
2. 启用所有监控指标
3. 配置告警规则
4. 准备安全事件响应流程

### 运营阶段

1. 定期审查审计日志
2. 更新黑名单和验证规则
3. 进行红蓝对抗演练
4. 保持安全补丁更新

### 审计阶段

1. 审计日志使用 append-only 存储
2. 审计记录关联审批决策
3. 定期备份审计数据
4. 实施审计日志完整性校验

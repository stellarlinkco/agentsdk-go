# Approval Queue V2 - 审批队列新逻辑

## 概述

Approval Queue V2 提供了更灵活、更安全的审批机制，支持两种级别的审批控制：

1. **Session 级别白名单** - 一旦 session 被加入白名单，该 session 下的所有命令都自动通过
2. **命令级别审批记录** - 针对特定命令的审批，每个 session + command 组合独立管理

## 核心逻辑

### Request 流程

当调用 `Request(sessionID, command, paths)` 时，按以下顺序检查：

```
1. 检查 session 是否在 whitelist 中
   ↓ 是 → 自动批准，创建带有 AutoApproved=true 的记录
   ↓ 否 → 继续

2. 在 records 中查找 session_id + command 组合
   ↓ 找到 approved 且未过期 → 返回已批准记录
   ↓ 找到 denied → 返回错误，拒绝执行
   ↓ 找到 pending → 返回现有 pending 记录
   ↓ 未找到 → 创建新的 pending 记录
```

### Approve 流程

当调用 `Approve(recordID, approver, ttl)` 时：

```
1. 验证记录存在且未被 denied
2. 更新记录状态为 approved
3. 设置 ExpiresAt（如果 ttl > 0）
4. 注意：不会自动将 session 加入 whitelist
```

### Session 白名单管理

新增方法用于管理 session 级别的白名单：

- `AddSessionToWhitelist(sessionID, ttl)` - 将 session 加入白名单
- `RemoveSessionFromWhitelist(sessionID)` - 从白名单移除 session
- `IsWhitelisted(sessionID)` - 检查 session 是否在白名单中
- `GetSessionWhitelistExpiry(sessionID)` - 获取白名单过期时间

## API 变更

### 新增方法

```go
// AddSessionToWhitelist 将 session 添加到白名单
// ttl: 0 表示永久有效
func (q *ApprovalQueue) AddSessionToWhitelist(sessionID string, ttl time.Duration) error

// RemoveSessionFromWhitelist 从白名单移除 session
func (q *ApprovalQueue) RemoveSessionFromWhitelist(sessionID string) error

// GetSessionWhitelistExpiry 获取 session 白名单过期时间
func (q *ApprovalQueue) GetSessionWhitelistExpiry(sessionID string) (time.Time, bool)

// IsCommandApproved 检查特定命令是否已批准
func (q *ApprovalQueue) IsCommandApproved(sessionID, command string) (*ApprovalRecord, bool)
```

### 修改的方法

```go
// Approve 现在只更新记录状态，不会自动将 session 加入白名单
// ttl 参数设置的是该命令批准的过期时间，不是白名单过期时间
func (q *ApprovalQueue) Approve(id, approver string, ttl time.Duration) (*ApprovalRecord, error)
```

## 使用示例

### 场景 1：命令级别审批

```go
q, _ := security.NewApprovalQueue("/path/to/approvals.json")

// 第一次请求命令
rec, _ := q.Request("session-1", "ls -la", nil)
// rec.State == ApprovalPending

// 审批该命令
q.Approve(rec.ID, "admin", time.Hour)

// 同一命令再次请求 - 自动批准
rec2, _ := q.Request("session-1", "ls -la", nil)
// rec2.State == ApprovalApproved (从记录中复用)

// 不同命令 - 需要新的审批
rec3, _ := q.Request("session-1", "cat /etc/passwd", nil)
// rec3.State == ApprovalPending
```

### 场景 2：Session 级别白名单

```go
q, _ := security.NewApprovalQueue("/path/to/approvals.json")

// 将 session 加入白名单（1小时有效）
q.AddSessionToWhitelist("session-1", time.Hour)

// 该 session 的所有命令自动批准
rec, _ := q.Request("session-1", "any-command", nil)
// rec.State == ApprovalApproved
// rec.AutoApproved == true
// rec.Reason == "session whitelisted"
```

### 场景 3：拒绝的命令

```go
q, _ := security.NewApprovalQueue("/path/to/approvals.json")

// 请求危险命令
rec, _ := q.Request("session-1", "rm -rf /", nil)

// 拒绝该命令
q.Deny(rec.ID, "admin", "too dangerous")

// 再次请求相同命令 - 直接拒绝
_, err := q.Request("session-1", "rm -rf /", nil)
// err != nil: "command 'rm -rf /' was previously denied"
```

### 场景 4：Pending 状态复用

```go
q, _ := security.NewApprovalQueue("/path/to/approvals.json")

// 第一次请求
rec1, _ := q.Request("session-1", "deploy", nil)
// rec1.State == ApprovalPending

// 在审批前再次请求相同命令
rec2, _ := q.Request("session-1", "deploy", nil)
// rec2.ID == rec1.ID (返回相同的 pending 记录)
```

## 数据持久化

审批数据持久化到 JSON 文件，包含两个主要部分：

```json
{
  "records": [
    {
      "id": "abc123",
      "session_id": "session-1",
      "command": "ls -la",
      "state": "approved",
      "requested_at": "2024-01-01T00:00:00Z",
      "approved_at": "2024-01-01T00:01:00Z",
      "approver": "admin",
      "expires_at": "2024-01-01T01:00:00Z"
    }
  ],
  "whitelist": {
    "session-1": "2024-01-01T01:00:00Z"
  }
}
```

## 过期策略

### Session 白名单过期

- `AddSessionToWhitelist(sessionID, ttl)` 设置白名单过期时间
- `ttl = 0` 表示永久有效（实际使用 100 年后的时间戳）
- 过期后自动从 whitelist 中移除

### 命令批准过期

- `Approve(recordID, approver, ttl)` 设置命令批准的过期时间
- `ttl = 0` 表示永久有效
- 过期后，再次请求相同命令会创建新的 pending 记录

## 与 V1 的区别

| 特性 | V1 (旧版) | V2 (新版) |
|------|-----------|-----------|
| Approve 行为 | 自动将 session 加入白名单 | 只更新记录状态 |
| 白名单控制 | 自动管理 | 显式管理 (`AddSessionToWhitelist`) |
| 命令级别 | 不支持 | 支持 (records 中查找) |
| Pending 复用 | 创建新记录 | 返回现有记录 |
| 拒绝处理 | 无 | 再次请求直接拒绝 |

## 迁移指南

从 V1 迁移到 V2：

1. 如果之前依赖 `Approve()` 自动将 session 加入白名单，需要显式调用 `AddSessionToWhitelist()`
2. 如果需要命令级别审批，不再需要自定义实现，SDK 已内置支持
3. 检查是否有代码依赖 `Approve()` 的 whitelist 清理行为

```go
// V1 代码
q.Approve(rec.ID, "admin", time.Hour) // 同时更新记录和 whitelist

// V2 代码 - 等效行为
q.Approve(rec.ID, "admin", time.Hour) // 只更新记录
q.AddSessionToWhitelist(rec.SessionID, time.Hour) // 显式添加白名单
```

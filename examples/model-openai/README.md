# OpenAI Model Example

演示如何在 agentsdk-go 中使用 OpenAI SDK。

## 前置要求

- 设置 `OPENAI_API_KEY` 环境变量

## 使用方法

### 方式 1: 使用官方 OpenAI API（推荐）

```bash
# .env 文件
OPENAI_API_KEY="sk-..."

# 运行示例
go run ./examples/model-openai/
```

### 方式 2: 使用本地代理

**重要**: 当前 OpenAI SDK 使用标准路径 `/v1/chat/completions`，如果你的代理使用不同的路径（如 `/v1/responses`），需要配置路由重写。

#### 已知问题

如果你看到 `405 Method Not Allowed` 错误：

```
POST "http://localhost:23000/chat/completions": 405 Method Not Allowed
```

**原因**: OpenAI SDK 默认请求 `/v1/chat/completions`，但你的代理可能期望：
- `/v1/responses` (Response API 格式)
- 或其他自定义路径

#### 解决方案

**方案 A: 代理配置路由重写（推荐）**

在你的代理服务器中配置路由规则：

```nginx
# Nginx 示例
location /v1/chat/completions {
    rewrite ^/v1/chat/completions /v1/responses break;
    proxy_pass http://backend;
}
```

```yaml
# Traefik 示例
http:
  routers:
    openai-proxy:
      rule: "PathPrefix(`/v1/chat/completions`)"
      middlewares:
        - rewrite-responses
  middlewares:
    rewrite-responses:
      replacePath:
        path: "/v1/responses"
```

**方案 B: 修改 base URL（临时方案）**

如果代理支持 OpenAI 标准格式，确保 base URL 正确：

```bash
# .env
OPENAI_BASE_URL="http://localhost:23000"  # SDK 会拼接 /v1/chat/completions
```

**方案 C: 使用官方 API**

移除 `OPENAI_BASE_URL` 配置，直接使用 OpenAI 官方服务：

```bash
# .env
OPENAI_API_KEY="sk-..."
# OPENAI_BASE_URL=""  # 注释掉或删除
```

## 代理 API 格式

你的代理返回的错误信息：
```json
{
  "error": {
    "message": "Invalid request: either \"messages\" (OpenAI format) or \"input\" (Response API format) is required",
    "type": "invalid_request_error",
    "code": "missing_required_fields"
  }
}
```

说明代理支持两种格式：

1. **OpenAI 格式** (标准): `{"messages": [...]}`
2. **Response API 格式** (自定义): `{"input": "..."}`

当前 agentsdk-go 使用标准 OpenAI 格式，确保你的代理正确处理 `/v1/chat/completions` 路径。

## 测试代理连接

```bash
# 测试 /v1/responses 端点
curl -X POST http://localhost:23000/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{"input": "test message"}'

# 测试 /v1/chat/completions 端点（OpenAI 标准）
curl -X POST http://localhost:23000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "test"}]}'
```

## 功能演示

本示例演示：

1. **同步生成**: 基本的 `Generate()` 调用
2. **流式生成**: `GenerateStream()` 逐步输出
3. **工具调用**: 使用 `GenerateWithTools()` 进行函数调用

## 预期输出

```
2025/11/17 14:00:00 OpenAI model (SDK): gpt-4.1-mini via api.openai.com
2025/11/17 14:00:00 OpenAI model ready: *openai.SDKModel (gpt-4.1-mini)
2025/11/17 14:00:00 ---- Generate (sync) ---- prompt="Explain how this OpenAI sample differs from examples/basic."
2025/11/17 14:00:02 assistant(assistant): This example demonstrates OpenAI-specific features...
2025/11/17 14:00:02 ---- GenerateStream ---- prompt="Stream three numbered steps for calling OpenAI via agentsdk-go."
1. Import the OpenAI model package...
2. Create an SDK model instance...
3. Call Generate or GenerateStream...
2025/11/17 14:00:04 stream finished
2025/11/17 14:00:04 tool request -> name=lookup_weather id=call_abc123 args=map[city:Tokyo]
```

## 相关文档

- [OpenAI SDK](https://github.com/openai/openai-go)
- [agentsdk-go Model 接口](../../pkg/model/model.go)
- [基础示例](../basic/)

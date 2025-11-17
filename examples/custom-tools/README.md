# 自定义 Tools 和 System Prompt 完整指南

本示例展示如何在 agentsdk-go 中自定义工具和系统提示词。

## 📦 核心概念

### 1. Tool 接口

所有自定义工具必须实现 `tool.Tool` 接口：

```go
type Tool interface {
    Name() string                    // 工具唯一标识
    Description() string             // 工具描述（LLM 可见）
    Schema() *JSONSchema            // 参数定义（JSON Schema）
    Execute(ctx, params) (*ToolResult, error)  // 执行逻辑
}
```

### 2. ToolResult 结构

```go
type ToolResult struct {
    Success bool        // 执行是否成功
    Output  string      // 文本输出
    Data    interface{} // 结构化数据
    Error   error       // 错误信息（error 类型）
}
```

### 3. System Prompt

通过 `SDKModel.SetSystem()` 方法设置自定义系统提示词。

---

## 🛠️ 自定义工具示例

### 示例 1：计算器工具

```go
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string {
    return "calculator"
}

func (t *CalculatorTool) Description() string {
    return "Perform basic arithmetic operations"
}

func (t *CalculatorTool) Schema() *tool.JSONSchema {
    return &tool.JSONSchema{
        Type: "object",
        Properties: map[string]interface{}{
            "operation": map[string]interface{}{
                "type": "string",
                "enum": []string{"add", "subtract", "multiply", "divide"},
            },
            "a": map[string]interface{}{"type": "number"},
            "b": map[string]interface{}{"type": "number"},
        },
        Required: []string{"operation", "a", "b"},
    }
}

func (t *CalculatorTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
    op := params["operation"].(string)
    a := params["a"].(float64)
    b := params["b"].(float64)

    var result float64
    switch op {
    case "add":
        result = a + b
    case "multiply":
        result = a * b
    // ... 其他操作
    }

    return &tool.ToolResult{
        Success: true,
        Data:    map[string]interface{}{"result": result},
    }, nil
}
```

### 示例 2：时间查询工具

```go
type TimeTool struct{}

func (t *TimeTool) Schema() *tool.JSONSchema {
    return &tool.JSONSchema{
        Type: "object",
        Properties: map[string]interface{}{
            "format": map[string]interface{}{
                "type": "string",
                "enum": []string{"rfc3339", "unix", "human"},
            },
            "timezone": map[string]interface{}{
                "type": "string",
                "description": "如 UTC, Asia/Shanghai",
            },
        },
        Required: []string{"format"},
    }
}

func (t *TimeTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
    format := params["format"].(string)
    timezone, _ := params["timezone"].(string)
    if timezone == "" {
        timezone = "UTC"
    }

    loc, _ := time.LoadLocation(timezone)
    now := time.Now().In(loc)

    var timeStr string
    switch format {
    case "rfc3339":
        timeStr = now.Format(time.RFC3339)
    case "unix":
        timeStr = fmt.Sprintf("%d", now.Unix())
    case "human":
        timeStr = now.Format("2006-01-02 15:04:05 MST")
    }

    return &tool.ToolResult{
        Success: true,
        Data: map[string]interface{}{
            "time": timeStr,
            "timezone": timezone,
        },
    }, nil
}
```

---

## 🎯 使用方法

### 1. 创建模型并设置 System Prompt

```go
// 方式 A：使用默认 Anthropic API
model := anthropic.NewSDKModel(apiKey, "claude-3-5-sonnet-20241022", 2048)

// 方式 B：使用自定义 baseURL（如 Kimi API）
model := anthropic.NewSDKModelWithBaseURL(
    apiKey,
    "claude-3-5-sonnet-20241022",
    "https://api.kimi.com/coding",
    2048,
)

// 设置自定义 System Prompt
model.SetSystem(`你是一个专业的数学助手。
- 使用 calculator 工具进行计算
- 使用 get_current_time 获取时间
- 提供清晰的步骤说明`)
```

### 2. 创建 Session 和 Agent

```go
// 创建内存 Session（必需）
sess, _ := session.NewMemorySession("my-session-id")

// 创建 Agent
ag, _ := agent.New(agent.Config{
    Name:        "my-assistant",
    Description: "Specialized agent with custom tools",
},
    agent.WithModel(model),   // 设置模型
    agent.WithSession(sess),  // 设置 session
)
```

### 3. 注册工具

```go
ag, _ := agent.New(agent.Config{
    Name:        "my-assistant",
    Description: "Specialized agent with custom tools",
})

// 注册自定义工具
ag.AddTool(&CalculatorTool{})
ag.AddTool(&TimeTool{})

// 也可以使用内置工具
ag.AddTool(toolbuiltin.NewBashTool())
ag.AddTool(toolbuiltin.NewFileTool())
```

### 3. 运行 Agent

```go
result, err := ag.Run(ctx, "请计算 (123 + 456) * 789")
fmt.Println(result.Output)
fmt.Printf("工具调用次数: %d\n", len(result.ToolCalls))
```

**⚠️ 重要提示**：
- 必须通过 `agent.WithModel(model)` 设置模型
- 必须通过 `agent.WithSession(sess)` 设置 session
- System prompt 通过 `model.SetSystem()` 设置

---

## 🚀 运行示例

```bash
# 设置环境变量
export ANTHROPIC_API_KEY="your-api-key"

# 可选：使用自定义 API（如 Kimi）
export ANTHROPIC_BASE_URL="https://api.kimi.com/coding"

# 编译并运行
cd examples/custom-tools
go run main.go
```

### 预期输出

```
=== 示例 1: 数学计算 ===
Output: **计算结果：**
**步骤展示：**
1. 123 + 456 = **579**
2. 579 × 789 = **456,831**

Tool Calls: 3

=== 示例 2: 时间查询 ===
Output: 当前上海时间是：2025-11-17 12:10:00 CST

Tool Calls: 1

=== Token Usage ===
Total tokens used: 458
```

---

## 📚 高级技巧

### 1. 工具参数验证

```go
func (t *MyTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
    // 类型断言 + 验证
    value, ok := params["field"].(string)
    if !ok || value == "" {
        return &tool.ToolResult{
            Success: false,
            Error:   fmt.Errorf("invalid field parameter"),
        }, nil
    }
    // ... 执行逻辑
}
```

### 2. 工具链式调用

LLM 会自动进行多轮工具调用，例如：

```
用户: "获取当前时间戳并计算距离 2025 年还有多少秒"
→ LLM 调用 get_current_time(format="unix")
→ 获得时间戳 1731842000
→ LLM 调用 calculator(subtract, 1735689600, 1731842000)
→ 得到结果 3847600 秒
```

### 3. System Prompt 最佳实践

```go
const systemPrompt = `你是 [角色定位]。

核心能力：
- [能力 1]：使用 [工具名] 实现 [功能]
- [能力 2]：...

行为准则：
- 始终使用工具而不是凭记忆
- 提供清晰的推理步骤
- 结果要包含单位和说明

限制：
- 不要执行危险命令
- 不要访问敏感文件`
```

### 4. 错误处理

```go
func (t *MyTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
    // 业务错误：返回 ToolResult.Error
    if businessError {
        return &tool.ToolResult{
            Success: false,
            Error:   fmt.Errorf("业务错误: %v", err),
        }, nil
    }

    // 系统错误：返回 error
    if systemError {
        return nil, fmt.Errorf("系统错误: %v", err)
    }

    return &tool.ToolResult{Success: true, Data: result}, nil
}
```

---

## 📖 内置工具参考

agentsdk-go 提供了两个内置工具：

### BashTool
```go
import "github.com/cexll/agentsdk-go/pkg/tool/builtin"

ag.AddTool(toolbuiltin.NewBashTool())
// 允许 Agent 执行 bash 命令
```

### FileTool
```go
ag.AddTool(toolbuiltin.NewFileTool())
// 允许 Agent 读写文件
```

---

## 🔗 相关文档

- [Tool 接口定义](../../pkg/tool/tool.go)
- [内置工具实现](../../pkg/tool/builtin/)
- [Agent 配置文档](../../pkg/agent/)
- [Model 接口文档](../../pkg/model/)

---

## ❓ 常见问题

**Q: 如何限制工具执行时间？**
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
result, err := ag.Run(ctx, input)
```

**Q: 如何在 tool 中访问文件系统？**
使用 `security.Sandbox` 包裹文件操作，参考 `FileTool` 实现。

**Q: 如何调试工具调用？**
查看 `result.ToolCalls` 获取所有工具调用记录：
```go
for _, call := range result.ToolCalls {
    log.Printf("Tool: %s, Params: %v, Output: %v",
        call.Name, call.Params, call.Output)
}
```

**Q: 工具 Schema 支持哪些类型？**
支持 JSON Schema 标准类型：`string`, `number`, `integer`, `boolean`, `object`, `array`, `enum`

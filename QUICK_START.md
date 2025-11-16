# agentsdk-go 快速开始指南

## 环境准备

### 1. 安装依赖

```bash
# Go 1.23+ 环境
go version  # 确保 >= 1.23

# 克隆或进入项目目录
cd agentsdk-go
```

### 2. 配置 API 密钥

agentsdk-go 支持多种 LLM 提供商，通过环境变量配置：

#### Kimi API（推荐）

```bash
export ANTHROPIC_BASE_URL="https://api.kimi.com/coding"
export ANTHROPIC_API_KEY="sk-kimi-V32mKLtdl5lVL24DejzJXEGkZMxXwcNdGSpW08Qfr7eDKIVliaefYycvReeJ8DGe"
```

#### Anthropic Claude

```bash
export ANTHROPIC_API_KEY="sk-ant-xxxx"
# 不设置 ANTHROPIC_BASE_URL 则使用默认 https://api.anthropic.com
```

#### OpenAI

```bash
export OPENAI_API_KEY="sk-xxxx"
export OPENAI_BASE_URL="https://api.openai.com/v1"  # 可选
```

---

## 单元测试验证

### 运行所有测试

```bash
go test ./pkg/... -cover
```

**预期输出**:
```
ok  	pkg/agent       0.611s	coverage: 65.1%
ok  	pkg/event       0.703s	coverage: 85.0%
ok  	pkg/mcp         1.166s	coverage: 76.9%
ok  	pkg/model/openai 1.136s	coverage: 48.5%
ok  	pkg/security    1.919s	coverage: 24.3%
ok  	pkg/server      1.460s	coverage: 58.8%
ok  	pkg/session     1.922s	coverage: 70.5%
ok  	pkg/tool        2.056s	coverage: 49.4%
ok  	pkg/wal         2.366s	coverage: 73.0%
```

### 运行特定模块测试

```bash
# 测试 WAL 持久化
go test ./pkg/wal -v

# 测试 MCP 集成
go test ./pkg/mcp -v

# 测试事件系统
go test ./pkg/event -v
```

---

## 快速示例

### 示例 1: 直接工具调用（Echo 模式）

当前 v0.1 MVP 为 **Echo 模式**，使用特殊格式 `tool:<name> {json}` 直接调用工具。

```bash
cat > /tmp/test_tool.go << 'EOF'
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cexll/agentsdk-go/pkg/agent"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

func main() {
	ctx := context.Background()

	ag, err := agent.New(agent.Config{
		Name:        "tool-test-agent",
		Description: "Test tool execution",
		DefaultContext: agent.RunContext{
			SessionID:     "test-session",
			WorkDir:       ".",
			MaxIterations: 1,
		},
	})
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	if err := ag.AddTool(toolbuiltin.NewBashTool()); err != nil {
		log.Fatalf("add bash tool: %v", err)
	}

	// 使用工具格式调用
	input := `tool:bash_execute {"command":"echo 'Hello from agentsdk-go'"}`
	result, err := ag.Run(ctx, input)
	if err != nil {
		log.Fatalf("agent run: %v", err)
	}

	fmt.Println("---- Tool Execution Result ----")
	fmt.Println(result.Output)
	fmt.Printf("Stop Reason: %s\n", result.StopReason)
	fmt.Printf("Tool Calls: %d\n", len(result.ToolCalls))
	if len(result.ToolCalls) > 0 {
		call := result.ToolCalls[0]
		fmt.Printf("  Name: %s\n", call.Name)
		fmt.Printf("  Output: %s\n", call.Output)
		fmt.Printf("  Duration: %v\n", call.Duration)
	}
}
EOF

go run /tmp/test_tool.go
```

**预期输出**:
```
---- Tool Execution Result ----
{"Success":true,"Output":"Hello from agentsdk-go","Data":{...},"Error":null}
Stop Reason: tool_call
Tool Calls: 1
  Name: bash_execute
  Output: &{true Hello from agentsdk-go map[...] <nil>}
  Duration: 2.490625ms
```

### 示例 2: Basic 示例（API 连通性验证）

```bash
# 设置 Kimi API
export ANTHROPIC_BASE_URL="https://api.kimi.com/coding"
export ANTHROPIC_API_KEY="sk-kimi-V32mKLtdl5lVL24DejzJXEGkZMxXwcNdGSpW08Qfr7eDKIVliaefYycvReeJ8DGe"

# 运行示例
go run examples/basic/main.go
```

**预期输出**:
```
Anthropic base URL: https://api.kimi.com/coding
Anthropic model: claude-3-5-sonnet-20241022
Anthropic model ready: *anthropic.AnthropicModel (claude-3-5-sonnet-20241022)
---- Agent Output ----
session basic-example-session: 请执行命令 'echo Hello from agentsdk-go' 并返回结果
---- Token Usage ----
input=41 output=72 total=113 cache=0
```

**说明**: 当前为 Echo 模式，Agent 会返回输入内容的回显。真实 LLM 推理集成计划在 v0.3。

### 示例 3: 流式输出（SSE）

```bash
# 启动 SSE 服务器
export ANTHROPIC_BASE_URL="https://api.kimi.com/coding"
export ANTHROPIC_API_KEY="sk-kimi-V32mKLtdl5lVL24DejzJXEGkZMxXwcNdGSpW08Qfr7eDKIVliaefYycvReeJ8DGe"

go run examples/stream/main.go
```

**另一个终端验证**:
```bash
# 单次执行
curl -X POST http://localhost:8080/run \
  -H 'Content-Type: application/json' \
  -d '{"input":"tool:bash_execute {\"command\":\"date\"}"}'

# 流式输出
curl -N http://localhost:8080/run/stream?input=hello
```

### 示例 4: agentctl CLI 工具

```bash
# 构建 CLI
make agentctl

# 或直接运行
cd cmd/agentctl

# 初始化配置
go run . config init

# 设置 API 密钥
go run . config set api_key "sk-kimi-V32mKLtdl5lVL24DejzJXEGkZMxXwcNdGSpW08Qfr7eDKIVliaefYycvReeJ8DGe"
go run . config set base_url "https://api.kimi.com/coding"

# 运行任务
go run . run --tool bash "tool:bash_execute {\"command\":\"uname -a\"}"

# 启动 HTTP 服务器
go run . serve --port 8080
```

---

## 工具系统测试

### Bash 工具

```go
import toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"

ag.AddTool(toolbuiltin.NewBashTool())

// 调用格式
input := `tool:bash_execute {"command":"ls -la"}`
```

### File 工具

```go
ag.AddTool(toolbuiltin.NewFileTool())

// 读取文件
input := `tool:file_read {"path":"/tmp/test.txt"}`

// 写入文件
input := `tool:file_write {"path":"/tmp/test.txt","content":"Hello World"}`

// 删除文件
input := `tool:file_delete {"path":"/tmp/test.txt"}`
```

---

## 持久化测试

### FileSession + WAL

```go
import (
	"github.com/cexll/agentsdk-go/pkg/session"
	"github.com/cexll/agentsdk-go/pkg/wal"
)

// 创建 WAL
w, err := wal.Open("/tmp/agent-wal")
if err != nil {
	log.Fatal(err)
}
defer w.Close()

// 创建 FileSession
sess, err := session.NewFileSession("session-001", w)
if err != nil {
	log.Fatal(err)
}

// 追加消息
sess.Append(session.Message{
	Role:    "user",
	Content: "Hello",
})

// 创建 Checkpoint
cpID, err := sess.Checkpoint()
if err != nil {
	log.Fatal(err)
}

// 恢复会话
newSess, err := session.NewFileSession("session-001", w)
if err != nil {
	log.Fatal(err)
}
err = newSess.Resume(cpID)
```

---

## MCP 集成测试

### 注册 MCP Server

```go
import "github.com/cexll/agentsdk-go/pkg/tool"

registry := tool.NewRegistry()

// stdio 协议
err := registry.RegisterMCPServer("stdio://./bin/mcp-server")

// SSE 协议
err := registry.RegisterMCPServer("http://localhost:8080")

// 执行远程工具
result, err := registry.Execute(ctx, "echo", map[string]interface{}{
	"text": "hello",
})
```

---

## 多模型支持测试

### OpenAI

```go
import (
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/openai"
)

factory := model.NewFactory(openai.NewProvider(nil))
cfg := model.ModelConfig{
	Provider: "openai",
	Model:    "gpt-4o",
	APIKey:   os.Getenv("OPENAI_API_KEY"),
	BaseURL:  "https://api.openai.com/v1",
}

m, err := factory.NewModel(ctx, cfg)
```

### Anthropic

```go
import (
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
)

factory := model.NewFactory(anthropic.NewProvider(nil))
cfg := model.ModelConfig{
	Provider: "anthropic",
	Model:    "claude-3-5-sonnet-20241022",
	APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
	BaseURL:  os.Getenv("ANTHROPIC_BASE_URL"),
}

m, err := factory.NewModel(ctx, cfg)
```

---

## 性能测试

### 并发工具调用

```bash
# 安装 hey（HTTP 负载测试工具）
go install github.com/rakyll/hey@latest

# 启动服务器
go run examples/stream/main.go &

# 并发测试
hey -n 100 -c 10 -m POST \
  -H "Content-Type: application/json" \
  -d '{"input":"tool:bash_execute {\"command\":\"echo test\"}"}' \
  http://localhost:8080/run
```

---

## 故障排查

### 常见问题

1. **API 连接失败**
   ```
   错误: failed to create anthropic model
   ```
   - 检查 `ANTHROPIC_API_KEY` 和 `ANTHROPIC_BASE_URL` 是否正确设置
   - 验证网络连接：`curl https://api.kimi.com/coding/v1/messages`

2. **工具执行失败**
   ```
   错误: security: command validation failed
   ```
   - 检查命令是否包含危险字符（管道、重定向等）
   - 参考 `pkg/security/validator.go` 了解限制规则

3. **测试失败**
   ```
   错误: package github.com/cexll/agentsdk-go/pkg/xxx: cannot find package
   ```
   - 运行 `go mod tidy` 更新依赖
   - 确保在项目根目录执行命令

4. **WAL 文件损坏**
   ```
   错误: WAL replay failed
   ```
   - 删除 WAL 目录重新初始化：`rm -rf /tmp/agent-wal`
   - 检查磁盘空间是否充足

---

## 下一步

- **v0.3 路线图**: 查看 `PROJECT_STATS.md` 了解企业级特性计划
- **架构设计**: 阅读 `agentsdk-go-architecture.md` 了解设计思路
- **v0.2 完成报告**: 查看 `V02_COMPLETION_REPORT.md` 了解新增功能细节
- **贡献指南**: 提交 PR 到 https://github.com/cexll/agentsdk-go

---

**文档版本**: v0.2
**最后更新**: 2025-11-16
**维护者**: agentsdk-go team

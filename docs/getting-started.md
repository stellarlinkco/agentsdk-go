# Getting Started

This guide walks through the basic usage of agentsdk-go, including environment setup, core concepts, and common code examples.

## Environment Requirements

### Required

- Go 1.24 or later
- Git (to clone the repo)
 
### Required (API calls / examples)

- Anthropic API Key (`ANTHROPIC_API_KEY`)

### Verify

```bash
go version  # should show go1.24 or later
```

## Installation

### Get the Source

```bash
git clone https://github.com/stellarlinkco/agentsdk-go.git
cd agentsdk-go
```

### Build Check

```bash
# Build the project
make build

# Run core module tests
go test ./...
```

### Configure API Key

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

## Basic Examples

### Minimal Runnable Example

Create `main.go`:

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/stellarlinkco/agentsdk-go/pkg/api"
    "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func main() {
    ctx := context.Background()

    // 创建模型提供者
    provider := &model.AnthropicProvider{
        APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
        ModelName: "claude-sonnet-4-5",
    }

    // 初始化运行时
    runtime, err := api.New(ctx, api.Options{
        ProjectRoot:   ".",
        ModelFactory:  provider,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer runtime.Close()

    // 执行任务
    result, err := runtime.Run(ctx, api.Request{
        Prompt:    "列出当前目录下的文件",
        SessionID: "demo",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("输出: %s", result.Result.Output)
}
```

Run:

```bash
go run main.go
```

### Using Middleware

```go
package main

import (
    "context"
    "log"
    "os"
    "time"

    "github.com/stellarlinkco/agentsdk-go/pkg/api"
    "github.com/stellarlinkco/agentsdk-go/pkg/middleware"
    "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func main() {
    ctx := context.Background()

    provider := &model.AnthropicProvider{
        APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
        ModelName: "claude-sonnet-4-5",
    }

    // 定义日志 Middleware（Middleware 是接口，使用 Funcs 辅助结构体）
    loggingMiddleware := middleware.Funcs{
        Identifier: "logging",
        OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
            log.Printf("[请求开始]")
            st.Values["start_time"] = time.Now()
            return nil
        },
        OnAfterAgent: func(ctx context.Context, st *middleware.State) error {
            duration := time.Since(st.Values["start_time"].(time.Time))
            log.Printf("[响应] 耗时: %v", duration)
            return nil
        },
    }

    // 注入 Middleware
    runtime, err := api.New(ctx, api.Options{
        ProjectRoot:   ".",
        ModelFactory:  provider,
        Middleware:    []middleware.Middleware{loggingMiddleware},
    })
    if err != nil {
        log.Fatal(err)
    }
    defer runtime.Close()

    result, err := runtime.Run(ctx, api.Request{
        Prompt:    "计算 1+1",
        SessionID: "math",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("结果: %s", result.Result.Output)
}
```

### Streaming Output

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/stellarlinkco/agentsdk-go/pkg/api"
    "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func main() {
    ctx := context.Background()

    provider := &model.AnthropicProvider{
        APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
        ModelName: "claude-sonnet-4-5",
    }

    runtime, err := api.New(ctx, api.Options{
        ProjectRoot:   ".",
        ModelFactory:  provider,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer runtime.Close()

    // 使用流式 API
    events, err := runtime.RunStream(ctx, api.Request{
        Prompt:    "分析当前项目结构",
        SessionID: "stream-demo",
    })
    if err != nil {
        log.Fatal(err)
    }

    for event := range events {
        switch event.Type {
        case "content_block_delta":
            if event.Delta != nil {
                fmt.Print(event.Delta.Text)
            }
        case "tool_execution_start":
            fmt.Printf("\n[执行工具] %s\n", event.Name)
        case "tool_execution_result":
            fmt.Printf("[工具输出] %s\n", event.Output)
        case "message_stop":
            fmt.Println("\n[完成]")
        }
    }
}
```

## Core Concepts

### Runtime

The Runtime (in `pkg/api/`) orchestrates model calls and tool execution.

Key method:

- `Run(ctx context.Context, req api.Request) (*api.Response, error)` — blocking run
- `RunStream(ctx context.Context, req api.Request) (<-chan api.StreamEvent, error)` — streaming run

Key traits:

- Supports multi-iteration (model → tools → model)
- `MaxIterations` limits loop count
- Middleware executes at 4 hook points

### Model

The Model interface defines provider behavior (`pkg/model/interface.go`):

```go
type Model interface {
    Complete(ctx context.Context, req Request) (*Response, error)
    CompleteStream(ctx context.Context, req Request, cb StreamHandler) error
}
```

Currently supported provider:

- Anthropic Claude (via `AnthropicProvider`)

### Tool

Tools are external functions the Agent can invoke (`pkg/tool/tool.go`):

```go
type Tool interface {
    Name() string
    Description() string
    Schema() *JSONSchema
    Execute(ctx context.Context, params map[string]any) (*ToolResult, error)
}
```

Built-ins (`pkg/tool/builtin/`):

- `bash` — execute shell commands
- `read` — read files
- `write` — write files
- `edit` — edit files (string replacement)
- `grep` — content search
- `glob` — file globbing
- `skill` — execute `.agents/skills/`

### Middleware

Middleware offers 4 interception points to inject custom logic (`pkg/middleware/`):

1. `BeforeAgent` — before Agent runs
2. `BeforeTool` — before tool execution
3. `AfterTool` — after tool execution
4. `AfterAgent` — after Agent finishes

## Configuration

### Directory Layout

Configuration lives under `.agents/`:

```
.agents/
├── settings.json         # main config
├── settings.local.json   # local overrides (gitignored)
├── rules/                # rules (markdown)
├── skills/               # skill definitions
└── agents/               # subagent definitions
```

### Precedence (high → low)

1. Runtime overrides (CLI flags / API options)
2. `<project>/.agents/settings.local.json`
3. `<project>/.agents/settings.json`
4. `~/.agents/settings.local.json`
5. `~/.agents/settings.json`
6. SDK defaults

Global settings under `~/.agents/` are optional and act as a low-priority baseline; keep project config under `<project>/.agents/`.

### settings.json Example

```json
{
  "permissions": {
    "additionalDirectories": []
  },
  "disallowedTools": ["bash"],
  "env": {
    "MY_VAR": "value"
  },
  "sandbox": {
    "enabled": false
  }
}
```

### Load Config

```go
import "github.com/stellarlinkco/agentsdk-go/pkg/config"

loader := &config.SettingsLoader{ProjectRoot: "."}

settings, err := loader.Load()
if err != nil {
    log.Fatal(err)
}
```

## Middleware Development

### Basic Middleware

```go
loggingMiddleware := middleware.Funcs{
    Identifier: "logging",
    OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
        log.Printf("收到请求")
        return nil
    },
    OnAfterAgent: func(ctx context.Context, st *middleware.State) error {
        log.Printf("返回响应")
        return nil
    },
}
```

### Sharing State

Use `State.Values` to share data across hooks:

```go
timingMiddleware := middleware.Funcs{
    Identifier: "timing",
    OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
        st.Values["start_time"] = time.Now()
        return nil
    },
    OnAfterAgent: func(ctx context.Context, st *middleware.State) error {
        startTime := st.Values["start_time"].(time.Time)
        duration := time.Since(startTime)
        log.Printf("执行时间: %v", duration)
        return nil
    },
}
```

### Error Handling

Returning an error stops the chain:

```go
validationMiddleware := middleware.Funcs{
    Identifier: "validation",
    OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
        // Returning an error stops the middleware chain
        return errors.New("输入不能为空")
    },
}
```

### Complex Example: Rate Limiting + Monitoring

```go
package main

import (
    "context"
    "errors"
    "log"
    "time"

    "github.com/stellarlinkco/agentsdk-go/pkg/middleware"
)

// 令牌桶限流器
type rateLimiter struct {
    tokens    int
    maxTokens int
    lastTime  time.Time
}

func (r *rateLimiter) allow() bool {
    now := time.Now()
    elapsed := now.Sub(r.lastTime).Seconds()
    r.tokens = min(r.maxTokens, r.tokens+int(elapsed*5)) // 每秒补充 5 个令牌
    r.lastTime = now

    if r.tokens > 0 {
        r.tokens--
        return true
    }
    return false
}

func createRateLimitMiddleware(maxTokens int) middleware.Middleware {
    limiter := &rateLimiter{
        tokens:    maxTokens,
        maxTokens: maxTokens,
        lastTime:  time.Now(),
    }

    return middleware.Funcs{
        Identifier: "rate-limit",
        OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
            if !limiter.allow() {
                return errors.New("请求过于频繁，请稍后再试")
            }
            return nil
        },
    }
}

func createMonitoringMiddleware() middleware.Middleware {
    return middleware.Funcs{
        Identifier: "monitoring",
        OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
            log.Printf("[monitor] agent request start")
            return nil
        },
        OnAfterAgent: func(ctx context.Context, st *middleware.State) error {
            log.Printf("[monitor] agent request end")
            return nil
        },
        OnBeforeTool: func(ctx context.Context, st *middleware.State) error {
            // 记录工具调用
            log.Printf("[监控] 执行工具")
            return nil
        },
        OnAfterTool: func(ctx context.Context, st *middleware.State) error {
            // 记录工具结果
            log.Printf("[监控] 工具执行完成")
            return nil
        },
    }
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

## Running Examples

### CLI

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/02-cli --session-id demo --prompt "你好"
```

Flags:

- `--session-id` — session ID (defaults to `SESSION_ID` env or `demo-session`)
- `--project-root` — project root directory (defaults to `.`; config lives under `<project>/.agents/`)

### HTTP Server

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cd examples/03-http
go run .
```

Endpoints:

- Health: `http://localhost:8080/health`
- Sync run: `POST /v1/run`
- Streaming: `POST /v1/run/stream`

### MCP Client

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cd examples/mcp
go run .
```

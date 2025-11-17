package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	"github.com/cexll/agentsdk-go/pkg/session"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

// =========================================
// 1. 自定义 Tool：计算器
// =========================================

type CalculatorTool struct{}

func (t *CalculatorTool) Name() string {
	return "calculator"
}

func (t *CalculatorTool) Description() string {
	return "Perform basic arithmetic operations: add, subtract, multiply, divide"
}

func (t *CalculatorTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"description": "The operation to perform",
				"enum":        []string{"add", "subtract", "multiply", "divide"},
			},
			"a": map[string]interface{}{
				"type":        "number",
				"description": "First number",
			},
			"b": map[string]interface{}{
				"type":        "number",
				"description": "Second number",
			},
		},
		Required: []string{"operation", "a", "b"},
	}
}

func (t *CalculatorTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	operation, _ := params["operation"].(string)
	a, _ := params["a"].(float64)
	b, _ := params["b"].(float64)

	var result float64
	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return &tool.ToolResult{
				Success: false,
				Error:   fmt.Errorf("division by zero"),
			}, nil
		}
		result = a / b
	default:
		return &tool.ToolResult{
			Success: false,
			Error:   fmt.Errorf("unknown operation: %s", operation),
		}, nil
	}

	return &tool.ToolResult{
		Success: true,
		Data:    map[string]interface{}{"result": result},
	}, nil
}

// =========================================
// 2. 自定义 Tool：获取当前时间
// =========================================

type TimeTool struct{}

func (t *TimeTool) Name() string {
	return "get_current_time"
}

func (t *TimeTool) Description() string {
	return "Get the current date and time in various formats"
}

func (t *TimeTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"format": map[string]interface{}{
				"type":        "string",
				"description": "Time format: rfc3339, unix, human",
				"enum":        []string{"rfc3339", "unix", "human"},
			},
			"timezone": map[string]interface{}{
				"type":        "string",
				"description": "Timezone (e.g., UTC, Asia/Shanghai), defaults to UTC",
			},
		},
		Required: []string{"format"},
	}
}

func (t *TimeTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	format, _ := params["format"].(string)
	timezone, _ := params["timezone"].(string)
	if timezone == "" {
		timezone = "UTC"
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return &tool.ToolResult{
			Success: false,
			Error:   fmt.Errorf("invalid timezone: %s", timezone),
		}, nil
	}

	now := time.Now().In(loc)
	var timeStr string

	switch format {
	case "rfc3339":
		timeStr = now.Format(time.RFC3339)
	case "unix":
		timeStr = fmt.Sprintf("%d", now.Unix())
	case "human":
		timeStr = now.Format("2006-01-02 15:04:05 MST")
	default:
		return &tool.ToolResult{
			Success: false,
			Error:   fmt.Errorf("unknown format: %s", format),
		}, nil
	}

	return &tool.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"time":     timeStr,
			"timezone": timezone,
		},
	}, nil
}

// =========================================
// 3. 自定义 System Prompt
// =========================================

const customSystemPrompt = `你是一个专业的数学助手和时间管理专家。

核心能力：
1. 使用 calculator 工具进行精确的数学计算
2. 使用 get_current_time 工具获取准确的时间信息

行为准则：
- 始终使用工具进行计算，不要凭记忆计算
- 提供清晰的步骤说明
- 结果要包含单位和说明
- 遇到复杂问题时，分步骤解决

示例对话：
用户："现在几点？"
你：[调用 get_current_time] → "现在是 2025-11-17 12:00:00 UTC"

用户："23 * 45 等于多少？"
你：[调用 calculator(multiply, 23, 45)] → "23 乘以 45 等于 1035"
`

// =========================================
// 4. Main 函数
// =========================================

func main() {
	ctx := context.Background()

	// 获取 API Key
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}

	// 创建模型（支持自定义 baseURL）
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	var model modelpkg.Model
	if baseURL != "" {
		model = anthropic.NewSDKModelWithBaseURL(apiKey, "claude-3-5-sonnet-20241022", baseURL, 2048)
		log.Printf("Using custom base URL: %s", baseURL)
	} else {
		model = anthropic.NewSDKModel(apiKey, "claude-3-5-sonnet-20241022", 2048)
	}

	// 设置 System Prompt（如果模型支持）
	if sdkModel, ok := model.(*anthropic.SDKModel); ok {
		sdkModel.SetSystem(customSystemPrompt)
		log.Println("Custom system prompt configured")
	}

	// 创建内存 Session
	sess, err := session.NewMemorySession("custom-tools-session")
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	// 创建 Agent
	ag, err := agent.New(agent.Config{
		Name:        "math-time-assistant",
		Description: "A specialized assistant with calculator and time tools",
		DefaultContext: agent.RunContext{
			SessionID:     "custom-tools-session",
			WorkDir:       ".",
			MaxIterations: 10,
		},
	},
		agent.WithModel(model),   // 使用 WithModel 选项设置模型
		agent.WithSession(sess),  // 使用创建的 session
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 注册自定义工具
	if err := ag.AddTool(&CalculatorTool{}); err != nil {
		log.Fatalf("Failed to add calculator tool: %v", err)
	}
	if err := ag.AddTool(&TimeTool{}); err != nil {
		log.Fatalf("Failed to add time tool: %v", err)
	}

	log.Println("Agent ready with custom tools and system prompt")
	log.Println("Tools registered: calculator, get_current_time")
	log.Println()

	// 示例 1：数学计算
	fmt.Println("=== 示例 1: 数学计算 ===")
	result1, err := ag.Run(ctx, "请计算 (123 + 456) * 789，分步骤展示")
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Output: %s\n", result1.Output)
		fmt.Printf("Tool Calls: %d\n", len(result1.ToolCalls))
		fmt.Println()
	}

	// 示例 2：时间查询
	fmt.Println("=== 示例 2: 时间查询 ===")
	result2, err := ag.Run(ctx, "现在上海时间是几点？请以人类可读格式显示")
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Output: %s\n", result2.Output)
		fmt.Printf("Tool Calls: %d\n", len(result2.ToolCalls))
		fmt.Println()
	}

	// 示例 3：组合使用
	fmt.Println("=== 示例 3: 组合使用 ===")
	result3, err := ag.Run(ctx, "距离 2025 年还有多少秒？先获取当前 Unix 时间戳，然后计算")
	if err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Output: %s\n", result3.Output)
		fmt.Printf("Tool Calls: %d\n", len(result3.ToolCalls))
	}

	// Token 使用统计
	fmt.Println("\n=== Token Usage ===")
	fmt.Printf("Total tokens used: %d\n", result1.Usage.TotalTokens+result2.Usage.TotalTokens+result3.Usage.TotalTokens)
}

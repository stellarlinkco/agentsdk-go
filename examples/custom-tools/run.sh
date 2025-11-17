#!/bin/bash
# 自定义工具示例运行脚本

cd "$(dirname "$0")"

# 加载环境变量
export ANTHROPIC_API_KEY="sk-kimi-YMaf5ozXXeHhucpuns8Rnzl700NaNWZG70njIqKiHGGBGtobZ1y4FCIkFtv73w97"
export ANTHROPIC_BASE_URL="https://api.kimi.com/coding"

echo "==================================="
echo "  自定义工具示例"
echo "==================================="
echo "API Key: ${ANTHROPIC_API_KEY:0:20}..."
echo "Base URL: $ANTHROPIC_BASE_URL"
echo ""
echo "注册工具: calculator, get_current_time"
echo ""

# 运行示例
go run main.go

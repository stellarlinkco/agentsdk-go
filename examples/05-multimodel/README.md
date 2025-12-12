# Multi-Model Example

This example demonstrates multi-model support, allowing you to configure
different models for different tools to optimize costs.

## Features

- **Model Pool**: Configure multiple models indexed by tier (`low`/`mid`/`high`).
- **Tool-Model Mapping**: Bind specific tools to specific model tiers.
- **Automatic Fallback**: Tools not in the mapping use the default model.

## Configuration

```go
haikuProvider := &model.AnthropicProvider{ModelName: "claude-3-5-haiku-20241022"}
sonnetProvider := &model.AnthropicProvider{ModelName: "claude-sonnet-4-20250514"}

haiku, _ := haikuProvider.Model(ctx)
sonnet, _ := sonnetProvider.Model(ctx)

rt, _ := api.New(ctx, api.Options{
    Model: sonnet, // default model

    ModelPool: map[string]model.Model{
        string(api.ModelTierLow):  haiku,
        string(api.ModelTierMid):  sonnet,
        string(api.ModelTierHigh): sonnet, // placeholder for opus
    },

    ToolModelMapping: map[string]string{
        "Grep": string(api.ModelTierLow),  // use Haiku for grep
        "Task": string(api.ModelTierHigh), // use Opus/high for task
    },
})
```

## Running

```bash
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/05-multimodel
```

## Cost Optimization Strategy

| Tool Type | Recommended Tier | Rationale |
|-----------|------------------|-----------|
| Grep, Glob | low | Simple pattern matching |
| Read | low | Just reading content |
| Bash | mid | May need understanding |
| Write, Edit | mid | Needs accuracy |
| Task (subagent) | high | Complex reasoning |


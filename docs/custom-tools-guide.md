# Custom Tools and Built-in Selection Guide

This guide explains how to selectively enable built-in tools and register custom tools in agentsdk-go, while keeping backward compatibility and simplicity.

## Tool Interface

Every custom tool must implement `tool.Tool`:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() *JSONSchema
    Execute(ctx context.Context, params map[string]any) (*ToolResult, error)
}
```

## Option Fields and Priority

- `Options.Tools []tool.Tool`  
  Legacy field. **When non-empty it fully overrides the tool set**, ignoring other tool options (backward compatible).
- `Options.EnabledBuiltinTools []string`  
  Controls the built-in whitelist (case-insensitive):  
  - `nil` (default): register all built-ins  
  - empty slice: disable all built-ins  
  - non-empty: enable only the listed built-ins  
  Available names (lowercase with underscores): `bash`, `file_read`, `file_write`, `file_edit`, `grep`, `glob`, `web_fetch`, `web_search`, `bash_output`, `task_create`, `skill`, `slash_command`, `task` (Task is only auto-enabled in CLI/Platform entrypoints).
- `Options.CustomTools []tool.Tool`  
  Appends custom tools when `Tools` is empty (nil entries are skipped).

Priority: `Tools` > (`EnabledBuiltinTools` filtering + `CustomTools` append).

## Built-in Whitelist Example

```go
opts := api.Options{
    ProjectRoot:         ".",
    ModelFactory:        provider,
    EnabledBuiltinTools: []string{"bash", "grep", "file_read"}, // enable only these
}
rt, _ := api.New(context.Background(), opts)
```

## Disable All Built-ins

```go
opts := api.Options{
    ProjectRoot:         ".",
    ModelFactory:        provider,
    EnabledBuiltinTools: []string{},               // register no built-ins
    CustomTools:         []tool.Tool{&EchoTool{}}, // custom only
}
```

## Append Custom Tools

```go
opts := api.Options{
    ProjectRoot:  ".",
    ModelFactory: provider,
    // nil means all built-ins stay enabled
    CustomTools: []tool.Tool{&EchoTool{}},
}
```

## Mix (Partial Built-ins + Custom)

```go
opts := api.Options{
    ProjectRoot:         ".",
    ModelFactory:        provider,
    EnabledBuiltinTools: []string{"bash", "file_read"},
    CustomTools:         []tool.Tool{&CalculatorTool{}},
}
```

## Custom Tool Example: Echo

```go
type EchoTool struct{}

func (t *EchoTool) Name() string        { return "echo" }
func (t *EchoTool) Description() string { return "returns the input text" }
func (t *EchoTool) Schema() *tool.JSONSchema {
    return &tool.JSONSchema{
        Type: "object",
        Properties: map[string]any{
            "text": map[string]any{"type": "string", "description": "text to echo"},
        },
        Required: []string{"text"},
    }
}
func (t *EchoTool) Execute(ctx context.Context, params map[string]any) (*tool.ToolResult, error) {
    return &tool.ToolResult{Output: fmt.Sprint(params["text"])}, nil
}
```

## Notes

- Name matching is case-insensitive; `-` or spaces are treated as `_`. Prefer the listed lowercase underscore forms.
- Task tool auto-attaches only in CLI/Platform entrypoints; CI entrypoint skips it.
- MCP tool registration is independent of these options and always registers all MCP tools.
- When names collide, the first tool wins and a warning is logged to highlight the conflict.

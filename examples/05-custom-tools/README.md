# 05-custom-tools: Selective built-ins + custom tool

This example shows how to:
- Enable only a subset of built-in tools via `EnabledBuiltinTools`
- Append a custom `EchoTool` via `CustomTools`
- Keep legacy `Tools` override semantics unchanged (not used here)

## Run
```bash
go run ./examples/05-custom-tools
# Online mode (requires ANTHROPIC_API_KEY):
# go run ./examples/05-custom-tools --online
```

## What happens
- Registers built-ins `bash` and `read` (because `EnabledBuiltinTools` lists them)
- Skips other built-ins (empty list would disable all)
- Appends a custom `echo` tool
- Sends a prompt instructing the model to call `echo`

Adjust the options in `main.go` to:
- Enable all built-ins: set `EnabledBuiltinTools: nil`
- Disable all built-ins: set `EnabledBuiltinTools: []string{}`
- Add more custom tools: append to `CustomTools`

package agent

import "time"

// Context carries runtime state for a single agent execution.
type Context struct {
	Iteration       int
	StartedAt       time.Time
	Values          map[string]any
	ToolResults     []ToolResult
	LastModelOutput *ModelOutput
}

func NewContext() *Context {
	return &Context{
		StartedAt: time.Now(),
		Values:    map[string]any{},
	}
}

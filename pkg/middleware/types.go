package middleware

import "context"

// Stage enumerates the interception points supported by the chain.
type Stage int

const (
	StageBeforeAgent Stage = iota
	StageBeforeTool
	StageAfterTool
	StageAfterAgent
)

// State carries mutable execution data shared across middleware invocations.
// The concrete types stored in these fields are left to callers; middleware
// should type-assert to what it expects.
type State struct {
	Iteration   int
	Agent       any
	ModelInput  any
	ModelOutput any
	ToolCall    any
	ToolResult  any
	Values      map[string]any
}

// Middleware defines all four interception points. Implementations may
// no-op individual methods when the hook is not needed.
type Middleware interface {
	Name() string
	BeforeAgent(ctx context.Context, st *State) error
	BeforeTool(ctx context.Context, st *State) error
	AfterTool(ctx context.Context, st *State) error
	AfterAgent(ctx context.Context, st *State) error
}

// Funcs is a helper that turns a set of function pointers into a Middleware.
// Unspecified hooks default to no-ops.
type Funcs struct {
	Identifier string

	OnBeforeAgent func(ctx context.Context, st *State) error
	OnBeforeTool  func(ctx context.Context, st *State) error
	OnAfterTool   func(ctx context.Context, st *State) error
	OnAfterAgent  func(ctx context.Context, st *State) error
}

func (f Funcs) Name() string {
	if f.Identifier != "" {
		return f.Identifier
	}
	return "middleware"
}

func (f Funcs) BeforeAgent(ctx context.Context, st *State) error {
	if f.OnBeforeAgent == nil {
		return nil
	}
	return f.OnBeforeAgent(ctx, st)
}

func (f Funcs) BeforeTool(ctx context.Context, st *State) error {
	if f.OnBeforeTool == nil {
		return nil
	}
	return f.OnBeforeTool(ctx, st)
}

func (f Funcs) AfterTool(ctx context.Context, st *State) error {
	if f.OnAfterTool == nil {
		return nil
	}
	return f.OnAfterTool(ctx, st)
}

func (f Funcs) AfterAgent(ctx context.Context, st *State) error {
	if f.OnAfterAgent == nil {
		return nil
	}
	return f.OnAfterAgent(ctx, st)
}

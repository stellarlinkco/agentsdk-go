package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
)

var (
	ErrDuplicateCommand = errors.New("commands: duplicate registration")
	ErrUnknownCommand   = errors.New("commands: unknown command")
)

// Definition describes a slash command handler.
type Definition struct {
	Name        string
	Description string
	Priority    int
	MutexKey    string
}

// Validate ensures the definition is sound.
func (d Definition) Validate() error {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return errors.New("commands: name is required")
	}
	if !validName(strings.ToLower(name)) {
		return fmt.Errorf("commands: invalid name %q", d.Name)
	}
	return nil
}

// Handler executes a parsed invocation.
type Handler interface {
	Handle(context.Context, Invocation) (Result, error)
}

// HandlerFunc turns a function into a Handler.
type HandlerFunc func(context.Context, Invocation) (Result, error)

// Handle implements Handler.
func (fn HandlerFunc) Handle(ctx context.Context, inv Invocation) (Result, error) {
	if fn == nil {
		return Result{}, errors.New("commands: handler func is nil")
	}
	return fn(ctx, inv)
}

// Result captures handler output.
type Result struct {
	Command  string
	Output   any
	Metadata map[string]any
	Error    string
}

func (r Result) clone() Result {
	if len(r.Metadata) > 0 {
		r.Metadata = maps.Clone(r.Metadata)
	}
	return r
}

// Executor routes parsed slash commands to registered handlers.
type Executor struct {
	mu       sync.RWMutex
	commands map[string]*registeredCommand
}

// NewExecutor creates a new command executor.
func NewExecutor() *Executor {
	return &Executor{commands: map[string]*registeredCommand{}}
}

// Register adds a command definition + handler pair.
func (e *Executor) Register(def Definition, handler Handler) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if handler == nil {
		return errors.New("commands: handler is nil")
	}
	normalized := registeredCommand{
		definition: Definition{
			Name:        strings.ToLower(strings.TrimSpace(def.Name)),
			Description: strings.TrimSpace(def.Description),
			Priority:    max(def.Priority, 0),
			MutexKey:    strings.ToLower(strings.TrimSpace(def.MutexKey)),
		},
		handler: handler,
	}
	key := normalized.definition.Name

	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.commands[key]; exists {
		return ErrDuplicateCommand
	}
	e.commands[key] = &normalized
	return nil
}

// Run parses text and executes slash commands sequentially.
func (e *Executor) Run(ctx context.Context, text string) ([]Result, error) {
	invocations, err := Parse(text)
	if err != nil {
		return nil, err
	}
	return e.Execute(ctx, invocations)
}

// Execute runs already parsed invocations.
func (e *Executor) Execute(ctx context.Context, invocations []Invocation) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(invocations) == 0 {
		return nil, nil
	}
	pending := make([]plannedExecution, 0, len(invocations))

	e.mu.RLock()
	for idx, inv := range invocations {
		cmd, ok := e.commands[inv.Name]
		if !ok {
			e.mu.RUnlock()
			return nil, ErrUnknownCommand
		}
		pending = append(pending, plannedExecution{
			order:      idx,
			invocation: inv,
			command:    cmd,
		})
	}
	e.mu.RUnlock()

	filtered := applyMutex(pending)
	results := make([]Result, 0, len(filtered))
	for _, exec := range filtered {
		res, err := exec.command.handler.Handle(ctx, exec.invocation)
		res.Command = exec.command.definition.Name
		res = res.clone()
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// List returns registered command definitions sorted by priority + name.
func (e *Executor) List() []Definition {
	e.mu.RLock()
	defs := make([]Definition, 0, len(e.commands))
	for _, cmd := range e.commands {
		defs = append(defs, cmd.definition)
	}
	e.mu.RUnlock()
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Priority != defs[j].Priority {
			return defs[i].Priority > defs[j].Priority
		}
		return defs[i].Name < defs[j].Name
	})
	return defs
}

func applyMutex(pending []plannedExecution) []plannedExecution {
	if len(pending) == 0 {
		return nil
	}
	best := map[string]int{}
	for idx, exec := range pending {
		key := exec.command.definition.MutexKey
		if key == "" {
			continue
		}
		if prevIdx, ok := best[key]; ok {
			prev := pending[prevIdx]
			if exec.command.definition.Priority > prev.command.definition.Priority {
				pending[prevIdx].skip = true
				best[key] = idx
			} else {
				pending[idx].skip = true
			}
			continue
		}
		best[key] = idx
	}
	filtered := make([]plannedExecution, 0, len(pending))
	for _, exec := range pending {
		if exec.skip {
			continue
		}
		filtered = append(filtered, exec)
	}
	return filtered
}

type plannedExecution struct {
	order      int
	invocation Invocation
	command    *registeredCommand
	skip       bool
}

type registeredCommand struct {
	definition Definition
	handler    Handler
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

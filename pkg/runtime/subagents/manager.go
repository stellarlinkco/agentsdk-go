package subagents

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
)

var (
	ErrDuplicateSubagent  = errors.New("subagents: duplicate registration")
	ErrUnknownSubagent    = errors.New("subagents: unknown target")
	ErrNoMatchingSubagent = errors.New("subagents: no matching subagent")
	ErrEmptyInstruction   = errors.New("subagents: instruction is empty")
)

// Definition describes a single subagent.
type Definition struct {
	Name        string
	Description string
	Priority    int
	MutexKey    string
	BaseContext Context
	Matchers    []skills.Matcher
}

// Validate ensures the definition is safe to register.
func (d Definition) Validate() error {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return errors.New("subagents: name is required")
	}
	for _, r := range name {
		if !(r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("subagents: invalid name %q", d.Name)
		}
	}
	return nil
}

// Handler executes a subagent request.
type Handler interface {
	Handle(context.Context, Context, Request) (Result, error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(context.Context, Context, Request) (Result, error)

// Handle implements Handler.
func (fn HandlerFunc) Handle(ctx context.Context, subCtx Context, req Request) (Result, error) {
	if fn == nil {
		return Result{}, errors.New("subagents: handler func is nil")
	}
	return fn(ctx, subCtx, req)
}

// Request carries execution parameters for a subagent run.
type Request struct {
	Target        string
	Instruction   string
	Activation    skills.ActivationContext
	ToolWhitelist []string
	Metadata      map[string]any
}

// Result captures handler output.
type Result struct {
	Subagent string
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

type Manager struct {
	mu        sync.RWMutex
	subagents map[string]*registeredSubagent
}

// NewManager builds a new manager.
func NewManager() *Manager {
	return &Manager{subagents: map[string]*registeredSubagent{}}
}

// Register installs a subagent definition + handler.
func (m *Manager) Register(def Definition, handler Handler) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if handler == nil {
		return errors.New("subagents: handler is nil")
	}
	normalized := registeredSubagent{
		definition: Definition{
			Name:        strings.ToLower(strings.TrimSpace(def.Name)),
			Description: strings.TrimSpace(def.Description),
			Priority:    max(def.Priority, 0),
			MutexKey:    strings.ToLower(strings.TrimSpace(def.MutexKey)),
			BaseContext: def.BaseContext.Clone(),
			Matchers:    append([]skills.Matcher(nil), def.Matchers...),
		},
		handler: handler,
	}
	key := normalized.definition.Name
	if key == "" {
		key = strings.TrimSpace(def.Name)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.subagents[key]; exists {
		return ErrDuplicateSubagent
	}
	m.subagents[key] = &normalized
	return nil
}

// List returns registered subagent definitions sorted by priority + name.
func (m *Manager) List() []Definition {
	m.mu.RLock()
	defs := make([]Definition, 0, len(m.subagents))
	for _, sub := range m.subagents {
		defs = append(defs, cloneDefinition(sub.definition))
	}
	m.mu.RUnlock()
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Priority != defs[j].Priority {
			return defs[i].Priority > defs[j].Priority
		}
		return defs[i].Name < defs[j].Name
	})
	return defs
}

// Dispatch selects and executes a subagent. When Target is empty, automatic
// matchers choose the best candidate subject to priority/mutex ordering.
func (m *Manager) Dispatch(ctx context.Context, req Request) (Result, error) {
	instruction := strings.TrimSpace(req.Instruction)
	if instruction == "" {
		return Result{}, ErrEmptyInstruction
	}
	target, err := m.selectTarget(req)
	if err != nil {
		return Result{}, err
	}
	runCtx := target.definition.BaseContext.Clone()
	if len(req.Metadata) > 0 {
		runCtx = runCtx.WithMetadata(req.Metadata)
	}
	if len(req.ToolWhitelist) > 0 {
		runCtx = runCtx.RestrictTools(req.ToolWhitelist...)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	result, execErr := target.handler.Handle(ctx, runCtx, req)
	result.Subagent = target.definition.Name
	result = result.clone()
	if execErr != nil {
		result.Error = execErr.Error()
		return result, execErr
	}
	return result, nil
}

func (m *Manager) selectTarget(req Request) (*registeredSubagent, error) {
	if target := strings.TrimSpace(req.Target); target != "" {
		m.mu.RLock()
		sub, ok := m.subagents[strings.ToLower(target)]
		m.mu.RUnlock()
		if !ok {
			return nil, ErrUnknownSubagent
		}
		return sub, nil
	}
	matches := m.matching(req.Activation)
	if len(matches) == 0 {
		return nil, ErrNoMatchingSubagent
	}
	return matches[0], nil
}

func (m *Manager) matching(ctx skills.ActivationContext) []*registeredSubagent {
	m.mu.RLock()
	snapshot := make([]*registeredSubagent, 0, len(m.subagents))
	for _, sub := range m.subagents {
		snapshot = append(snapshot, sub)
	}
	m.mu.RUnlock()

	type candidate struct {
		sub   *registeredSubagent
		score float64
	}
	var candidates []candidate
	for _, sub := range snapshot {
		if len(sub.definition.Matchers) == 0 {
			candidates = append(candidates, candidate{sub, 0.5})
			continue
		}
		var best skills.MatchResult
		matched := false
		for _, matcher := range sub.definition.Matchers {
			if matcher == nil {
				continue
			}
			result := matcher.Match(ctx)
			if !result.Matched {
				continue
			}
			if !matched || result.BetterThan(best) {
				best = result
				matched = true
			}
		}
		if !matched {
			continue
		}
		candidates = append(candidates, candidate{sub: sub, score: best.Score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		di := candidates[i].sub.definition
		dj := candidates[j].sub.definition
		if di.Priority != dj.Priority {
			return di.Priority > dj.Priority
		}
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return di.Name < dj.Name
	})

	seen := map[string]struct{}{}
	filtered := make([]*registeredSubagent, 0, len(candidates))
	for _, cand := range candidates {
		key := cand.sub.definition.MutexKey
		if key == "" {
			filtered = append(filtered, cand.sub)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, cand.sub)
	}
	return filtered
}

func cloneDefinition(def Definition) Definition {
	cloned := Definition{
		Name:        def.Name,
		Description: def.Description,
		Priority:    def.Priority,
		MutexKey:    def.MutexKey,
		BaseContext: def.BaseContext.Clone(),
		Matchers:    append([]skills.Matcher(nil), def.Matchers...),
	}
	return cloned
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type registeredSubagent struct {
	definition Definition
	handler    Handler
}

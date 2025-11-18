package skills

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
	// ErrDuplicateSkill indicates an attempt to register the same name twice.
	ErrDuplicateSkill = errors.New("skills: duplicate registration")
	// ErrUnknownSkill is returned by Execute/Get when a skill is missing.
	ErrUnknownSkill = errors.New("skills: unknown skill")
)

// Definition describes a declarative skill registration entry.
type Definition struct {
	Name        string
	Description string
	Priority    int
	MutexKey    string
	// DisableAutoActivation keeps the skill available for manual invocation
	// while excluding it from automatic activation matching.
	DisableAutoActivation bool
	Metadata              map[string]string
	Matchers              []Matcher
}

// Validate performs cheap sanity checks before accepting a definition.
func (d Definition) Validate() error {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return errors.New("skills: name is required")
	}
	for _, r := range name {
		if !(r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("skills: invalid name %q", d.Name)
		}
	}
	return nil
}

// Handler executes a skill.
type Handler interface {
	Execute(context.Context, ActivationContext) (Result, error)
}

// HandlerFunc adapts ordinary functions to Handler.
type HandlerFunc func(context.Context, ActivationContext) (Result, error)

// Execute implements Handler.
func (fn HandlerFunc) Execute(ctx context.Context, ac ActivationContext) (Result, error) {
	if fn == nil {
		return Result{}, errors.New("skills: handler func is nil")
	}
	return fn(ctx, ac)
}

// Result captures the output from a skill execution.
type Result struct {
	Skill    string
	Output   any
	Metadata map[string]any
}

// clone ensures internal metadata never leaks shared references.
func (r Result) clone() Result {
	if len(r.Metadata) > 0 {
		r.Metadata = maps.Clone(r.Metadata)
	}
	return r
}

// Skill represents a single registered skill.
type Skill struct {
	definition Definition
	handler    Handler
}

// Definition returns an immutable copy of the skill metadata.
func (s *Skill) Definition() Definition {
	if s == nil {
		return Definition{}
	}
	def := s.definition
	if len(def.Metadata) > 0 {
		def.Metadata = maps.Clone(def.Metadata)
	}
	def.Matchers = append([]Matcher(nil), def.Matchers...)
	return def
}

// Execute runs the skill handler.
func (s *Skill) Execute(ctx context.Context, ac ActivationContext) (Result, error) {
	if s == nil || s.handler == nil {
		return Result{}, errors.New("skills: skill is nil")
	}
	res, err := s.handler.Execute(ctx, ac)
	if err != nil {
		return Result{}, err
	}
	if res.Skill == "" {
		res.Skill = s.definition.Name
	}
	return res.clone(), nil
}

// Registry coordinates skill registration and activation.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{skills: map[string]*Skill{}}
}

// Register adds a skill definition + handler pair.
func (r *Registry) Register(def Definition, handler Handler) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if handler == nil {
		return errors.New("skills: handler is nil")
	}
	normalized := normalizeDefinition(def)
	key := normalized.Name

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[key]; exists {
		return ErrDuplicateSkill
	}
	r.skills[key] = &Skill{definition: normalized, handler: handler}
	return nil
}

// Get fetches a skill by name.
func (r *Registry) Get(name string) (*Skill, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	r.mu.RLock()
	defer r.mu.RUnlock()
	skill, ok := r.skills[key]
	return skill, ok
}

// Execute invokes a named skill.
func (r *Registry) Execute(ctx context.Context, name string, ac ActivationContext) (Result, error) {
	skill, ok := r.Get(name)
	if !ok {
		return Result{}, ErrUnknownSkill
	}
	return skill.Execute(ctx, ac)
}

// Activation is a resolved auto-activation candidate.
type Activation struct {
	Skill  *Skill
	Score  float64
	Reason string
}

// Definition returns metadata for the activation.
func (a Activation) Definition() Definition {
	if a.Skill == nil {
		return Definition{}
	}
	return a.Skill.Definition()
}

// Match evaluates all auto-activating skills against the provided context while
// enforcing priority ordering and mutex groups.
func (r *Registry) Match(ctx ActivationContext) []Activation {
	snapshot := r.snapshot()
	var matches []Activation
	for _, skill := range snapshot {
		def := skill.definition
		if def.DisableAutoActivation {
			continue
		}
		result, ok := evaluate(skill, ctx)
		if !ok {
			continue
		}
		matches = append(matches, Activation{Skill: skill, Score: result.Score, Reason: result.Reason})
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		di := matches[i].Skill.definition
		dj := matches[j].Skill.definition
		if di.Priority != dj.Priority {
			return di.Priority > dj.Priority
		}
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return di.Name < dj.Name
	})

	selected := matches[:0]
	seen := map[string]struct{}{}
	for _, activation := range matches {
		key := activation.Skill.definition.MutexKey
		if key == "" {
			selected = append(selected, activation)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, activation)
	}
	return selected
}

// List returns the registered skill definitions sorted by priority + name.
func (r *Registry) List() []Definition {
	snapshot := r.snapshot()
	defs := make([]Definition, 0, len(snapshot))
	for _, skill := range snapshot {
		defs = append(defs, skill.Definition())
	}
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Priority != defs[j].Priority {
			return defs[i].Priority > defs[j].Priority
		}
		return defs[i].Name < defs[j].Name
	})
	return defs
}

func (r *Registry) snapshot() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		out = append(out, skill)
	}
	return out
}

func evaluate(skill *Skill, ctx ActivationContext) (MatchResult, bool) {
	if len(skill.definition.Matchers) == 0 {
		return MatchResult{Matched: true, Score: 0.5, Reason: "always"}, true
	}
	var best MatchResult
	matched := false
	for _, matcher := range skill.definition.Matchers {
		if matcher == nil {
			continue
		}
		res := matcher.Match(ctx)
		if !res.Matched {
			continue
		}
		if !matched || res.BetterThan(best) {
			best = res
			matched = true
		}
	}
	return best, matched
}

func normalizeDefinition(def Definition) Definition {
	normalized := Definition{
		Name:                  strings.ToLower(strings.TrimSpace(def.Name)),
		Description:           strings.TrimSpace(def.Description),
		Priority:              def.Priority,
		MutexKey:              strings.ToLower(strings.TrimSpace(def.MutexKey)),
		DisableAutoActivation: def.DisableAutoActivation,
	}
	if normalized.Name == "" {
		normalized.Name = strings.TrimSpace(def.Name)
	}
	if normalized.Priority < 0 {
		normalized.Priority = 0
	}
	if len(def.Metadata) > 0 {
		normalized.Metadata = maps.Clone(def.Metadata)
	}
	if len(def.Matchers) > 0 {
		normalized.Matchers = append([]Matcher(nil), def.Matchers...)
	}
	return normalized
}

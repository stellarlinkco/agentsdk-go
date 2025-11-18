package subagents

import (
	"context"
	"maps"
	"slices"
	"strings"
)

type contextKey struct{}

// Context stores execution metadata for an individual subagent run.
type Context struct {
	SessionID     string
	Metadata      map[string]any
	ToolWhitelist []string
}

// Clone produces a deep copy to maintain isolation between runs.
func (c Context) Clone() Context {
	cloned := Context{SessionID: c.SessionID}
	if len(c.Metadata) > 0 {
		cloned.Metadata = maps.Clone(c.Metadata)
	}
	if len(c.ToolWhitelist) > 0 {
		cloned.ToolWhitelist = append([]string(nil), c.ToolWhitelist...)
	}
	return cloned
}

// WithMetadata merges metadata into the context.
func (c Context) WithMetadata(meta map[string]any) Context {
	if len(meta) == 0 {
		return c
	}
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	for k, v := range meta {
		c.Metadata[k] = v
	}
	return c
}

// RestrictTools narrows the tool whitelist to the provided names.
func (c Context) RestrictTools(tools ...string) Context {
	cleaned := normalizeTools(tools)
	if len(cleaned) == 0 {
		return c
	}
	if len(c.ToolWhitelist) == 0 {
		c.ToolWhitelist = cleaned
		return c
	}
	base := toToolSet(c.ToolWhitelist)
	restricted := make([]string, 0, len(cleaned))
	for _, tool := range cleaned {
		if _, ok := base[tool]; ok {
			restricted = append(restricted, tool)
		}
	}
	c.ToolWhitelist = restricted
	return c
}

// Allows reports whether the tool may be used under this context. Empty
// whitelists imply full access for backward compatibility with legacy agents.
func (c Context) Allows(tool string) bool {
	if len(c.ToolWhitelist) == 0 {
		return true
	}
	_, ok := toToolSet(c.ToolWhitelist)[normalizeTool(tool)]
	return ok
}

// ToolList returns the current whitelist (sorted, deduplicated) for inspection.
func (c Context) ToolList() []string {
	if len(c.ToolWhitelist) == 0 {
		return nil
	}
	list := normalizeTools(c.ToolWhitelist)
	list = slices.Compact(list)
	slices.Sort(list)
	return list
}

// WithContext stores the runtime Context inside ctx for downstream consumers.
func WithContext(ctx context.Context, subCtx Context) context.Context {
	return context.WithValue(ctx, contextKey{}, subCtx.Clone())
}

// FromContext retrieves a Context previously injected with WithContext.
func FromContext(ctx context.Context) (Context, bool) {
	if ctx == nil {
		return Context{}, false
	}
	if value, ok := ctx.Value(contextKey{}).(Context); ok {
		return value.Clone(), true
	}
	return Context{}, false
}

func normalizeTools(tools []string) []string {
	result := make([]string, 0, len(tools))
	seen := map[string]struct{}{}
	for _, tool := range tools {
		norm := normalizeTool(tool)
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		result = append(result, norm)
	}
	slices.Sort(result)
	return result
}

func normalizeTool(tool string) string {
	return strings.ToLower(strings.TrimSpace(tool))
}

func toToolSet(tools []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		norm := normalizeTool(tool)
		if norm == "" {
			continue
		}
		set[norm] = struct{}{}
	}
	return set
}

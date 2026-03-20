package toolbuiltin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

const skillToolDescriptionHeader = `Execute a skill.

<skills_instructions>
Call this tool with {"command":"<skill-name>"} (no arguments).
Only use skills listed in <available_skills>. Do not invoke a skill that is already running.
</skills_instructions>

<available_skills>
`

var skillSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"command": map[string]interface{}{
			"type":        "string",
			"description": "The skill name (no arguments). E.g., \"pdf\" or \"xlsx\"",
		},
	},
	Required: []string{"command"},
}

// ActivationContextProvider resolves the activation context for manual skill calls.
type ActivationContextProvider func(context.Context) skills.ActivationContext

// SkillTool adapts the runtime skills registry into a tool.
type SkillTool struct {
	registry *skills.Registry
	provider ActivationContextProvider
}

// NewSkillTool wires the registry with an optional activation provider.
func NewSkillTool(reg *skills.Registry, provider ActivationContextProvider) *SkillTool {
	if provider == nil {
		provider = defaultActivationProvider
	}
	return &SkillTool{registry: reg, provider: provider}
}

func (s *SkillTool) Name() string { return "skill" }

func (s *SkillTool) Description() string {
	var defs []skills.Definition
	if s != nil && s.registry != nil {
		defs = s.registry.List()
	}
	return buildSkillDescription(defs)
}

func (s *SkillTool) Schema() *tool.JSONSchema { return skillSchema }

func buildSkillDescription(defs []skills.Definition) string {
	var b strings.Builder
	b.WriteString(skillToolDescriptionHeader)
	if len(defs) == 0 {
		b.WriteString("</available_skills>\n")
		return b.String()
	}
	for i, def := range defs {
		writeSkillDefinition(&b, def)
		if i < len(defs)-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("</available_skills>\n")
	return b.String()
}

func writeSkillDefinition(b *strings.Builder, def skills.Definition) {
	name := strings.TrimSpace(def.Name)
	if name == "" {
		name = "unknown"
	}
	description := strings.TrimSpace(def.Description)
	if description == "" {
		description = "No description provided."
	}
	location := strings.TrimSpace(skillLocation(def))
	if location == "" {
		location = "unspecified"
	}

	fmt.Fprintf(b, `<skill>
<name>
%s
</name>
<description>
%s
</description>
<location>
%s
</location>
</skill>
`, escapeXML(name), escapeXML(description), escapeXML(location))
}

func (s *SkillTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if s == nil || s.registry == nil {
		return nil, errors.New("skill registry is not initialised")
	}
	name, err := parseSkillName(params)
	if err != nil {
		return nil, err
	}
	act := s.provider(ctx)
	result, err := s.registry.Execute(ctx, name, act)
	if err != nil {
		return nil, err
	}
	output := formatSkillOutput(result)
	data := map[string]interface{}{
		"skill":    result.Skill,
		"output":   result.Output,
		"metadata": result.Metadata,
	}
	return &tool.ToolResult{
		Success: true,
		Output:  output,
		Data:    data,
	}, nil
}

func parseSkillName(params map[string]interface{}) (string, error) {
	if params == nil {
		return "", errors.New("params is nil")
	}
	raw, ok := params["command"]
	if !ok {
		return "", errors.New("command is required")
	}
	name, err := coerceString(raw)
	if err != nil {
		return "", fmt.Errorf("command must be string: %w", err)
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", errors.New("command cannot be empty")
	}
	return name, nil
}

func formatSkillOutput(result skills.Result) string {
	switch v := result.Output.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return v
		}
	case fmt.Stringer:
		if text := strings.TrimSpace(v.String()); text != "" {
			return text
		}
	case nil:
	default:
		if data, err := json.Marshal(v); err == nil {
			text := strings.TrimSpace(string(data))
			if text != "" && text != "null" {
				return text
			}
		}
	}
	if result.Skill == "" {
		return "skill executed"
	}
	return fmt.Sprintf("skill %s executed", result.Skill)
}

type activationContextKey struct{}

// WithSkillActivationContext attaches a skills.ActivationContext to the context.
func WithSkillActivationContext(ctx context.Context, ac skills.ActivationContext) context.Context {
	return context.WithValue(ctx, activationContextKey{}, ac.Clone())
}

// SkillActivationContextFromContext extracts an activation context if present.
func SkillActivationContextFromContext(ctx context.Context) (skills.ActivationContext, bool) {
	if ctx == nil {
		return skills.ActivationContext{}, false
	}
	ac, ok := ctx.Value(activationContextKey{}).(skills.ActivationContext)
	if !ok {
		return skills.ActivationContext{}, false
	}
	return ac, true
}

func defaultActivationProvider(ctx context.Context) skills.ActivationContext {
	if ac, ok := SkillActivationContextFromContext(ctx); ok {
		return ac
	}
	return skills.ActivationContext{}
}

func skillLocation(def skills.Definition) string {
	if len(def.Metadata) == 0 {
		return ""
	}
	for _, key := range []string{"location", "source", "origin"} {
		if value := strings.TrimSpace(def.Metadata[key]); value != "" {
			return value
		}
	}
	return ""
}

var skillDescriptionEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

func escapeXML(value string) string {
	if value == "" {
		return ""
	}
	return skillDescriptionEscaper.Replace(value)
}

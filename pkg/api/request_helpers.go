package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
	"github.com/cexll/agentsdk-go/pkg/runtime/subagents"
)

func removeCommandLines(prompt string, invs []commands.Invocation) string {
	if len(invs) == 0 {
		return prompt
	}
	mask := map[int]struct{}{}
	for _, inv := range invs {
		pos := inv.Position - 1
		if pos >= 0 {
			mask[pos] = struct{}{}
		}
	}
	lines := strings.Split(prompt, "\n")
	kept := make([]string, 0, len(lines))
	for idx, line := range lines {
		if _, drop := mask[idx]; drop {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func applyPromptMetadata(prompt string, meta map[string]any) string {
	if len(meta) == 0 {
		return prompt
	}
	if text, ok := anyToString(meta["api.prompt_override"]); ok {
		prompt = text
	}
	if text, ok := anyToString(meta["api.prepend_prompt"]); ok {
		prompt = strings.TrimSpace(text) + "\n" + prompt
	}
	if text, ok := anyToString(meta["api.append_prompt"]); ok {
		prompt = prompt + "\n" + strings.TrimSpace(text)
	}
	return strings.TrimSpace(prompt)
}

func mergeTags(req *Request, meta map[string]any) {
	if req == nil || len(meta) == 0 {
		return
	}
	if req.Tags == nil {
		req.Tags = map[string]string{}
	}
	if tags, ok := meta["api.tags"].(map[string]string); ok {
		for k, v := range tags {
			req.Tags[k] = v
		}
		return
	}
	if raw, ok := meta["api.tags"].(map[string]any); ok {
		for k, v := range raw {
			req.Tags[k] = fmt.Sprint(v)
		}
	}
}

func applyCommandMetadata(req *Request, meta map[string]any) {
	if req == nil || len(meta) == 0 {
		return
	}
	if target, ok := anyToString(meta["api.target_subagent"]); ok {
		req.TargetSubagent = target
	}
	if wl := stringSlice(meta["api.tool_whitelist"]); len(wl) > 0 {
		req.ToolWhitelist = wl
	}
}

func applySubagentTarget(req *Request) (subagents.Definition, bool) {
	if req == nil {
		return subagents.Definition{}, false
	}
	target := strings.TrimSpace(req.TargetSubagent)
	if target == "" {
		req.TargetSubagent = ""
		return subagents.Definition{}, false
	}
	if def, ok := subagents.BuiltinDefinition(target); ok {
		req.TargetSubagent = def.Name
		return def, true
	}
	req.TargetSubagent = canonicalToolName(target)
	return subagents.Definition{}, false
}

func buildSubagentContext(req Request, def subagents.Definition, matched bool) (subagents.Context, bool) {
	var subCtx subagents.Context
	if matched {
		subCtx = def.BaseContext.Clone()
	}
	if session := strings.TrimSpace(req.SessionID); session != "" {
		subCtx.SessionID = session
	}
	if desc := metadataString(req.Metadata, "task.description"); desc != "" {
		if subCtx.Metadata == nil {
			subCtx.Metadata = map[string]any{}
		}
		subCtx.Metadata["task.description"] = desc
	}
	if model := strings.ToLower(metadataString(req.Metadata, "task.model")); model != "" {
		if subCtx.Metadata == nil {
			subCtx.Metadata = map[string]any{}
		}
		subCtx.Metadata["task.model"] = model
		if strings.TrimSpace(subCtx.Model) == "" {
			subCtx.Model = model
		}
	}
	if subCtx.SessionID == "" && len(subCtx.Metadata) == 0 && len(subCtx.ToolWhitelist) == 0 && strings.TrimSpace(subCtx.Model) == "" {
		return subagents.Context{}, false
	}
	return subCtx, true
}

func metadataString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	if val, ok := anyToString(meta[key]); ok {
		return val
	}
	return ""
}

func canonicalToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func toLowerSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if key := canonicalToolName(value); key != "" {
			set[key] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func combineToolWhitelists(requested []string, subagent []string) map[string]struct{} {
	reqSet := toLowerSet(requested)
	subSet := toLowerSet(subagent)
	switch {
	case len(reqSet) == 0 && len(subSet) == 0:
		return nil
	case len(reqSet) == 0:
		return subSet
	case len(subSet) == 0:
		return reqSet
	default:
		intersection := make(map[string]struct{}, len(subSet))
		for name := range subSet {
			if _, ok := reqSet[name]; ok {
				intersection[name] = struct{}{}
			}
		}
		return intersection
	}
}

func orderedForcedSkills(reg *skills.Registry, names []string) []skills.Activation {
	if reg == nil || len(names) == 0 {
		return nil
	}
	var activations []skills.Activation
	for _, name := range names {
		skill, ok := reg.Get(name)
		if !ok {
			continue
		}
		activations = append(activations, skills.Activation{Skill: skill})
	}
	return activations
}

func combinePrompt(current string, output any) string {
	text, ok := anyToString(output)
	if !ok || strings.TrimSpace(text) == "" {
		return current
	}
	if current == "" {
		return strings.TrimSpace(text)
	}
	return current + "\n" + strings.TrimSpace(text)
}

func prependPrompt(prompt, prefix string) string {
	if strings.TrimSpace(prefix) == "" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return strings.TrimSpace(prefix)
	}
	return strings.TrimSpace(prefix) + "\n\n" + strings.TrimSpace(prompt)
}

func mergeMetadata(dst, src map[string]any) map[string]any {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func anyToString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), true
	case fmt.Stringer:
		return strings.TrimSpace(v.String()), true
	}
	if value == nil {
		return "", false
	}
	return strings.TrimSpace(fmt.Sprint(value)), true
}

func stringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		out := append([]string(nil), v...)
		sort.Strings(out)
		return out
	case []any:
		var out []string
		for _, entry := range v {
			if text, ok := anyToString(entry); ok && text != "" {
				out = append(out, text)
			}
		}
		sort.Strings(out)
		return out
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		return []string{text}
	default:
		return nil
	}
}

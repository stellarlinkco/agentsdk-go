package skills

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ActivationContext captures conversational state used for auto-activation.
type ActivationContext struct {
	Prompt   string
	Channels []string
	Tags     map[string]string
	Traits   []string
	Metadata map[string]any
}

// Clone produces an isolated copy of the activation context.
func (c ActivationContext) Clone() ActivationContext {
	cloned := ActivationContext{Prompt: c.Prompt}
	if len(c.Channels) > 0 {
		cloned.Channels = append([]string(nil), c.Channels...)
	}
	if len(c.Traits) > 0 {
		cloned.Traits = append([]string(nil), c.Traits...)
	}
	if len(c.Tags) > 0 {
		cloned.Tags = maps.Clone(c.Tags)
	}
	if len(c.Metadata) > 0 {
		cloned.Metadata = maps.Clone(c.Metadata)
	}
	return cloned
}

// MatchResult represents the outcome of a matcher evaluation.
type MatchResult struct {
	Matched bool
	Score   float64
	Reason  string
}

// BetterThan orders two match results. Unmatched entries always lose, then
// higher scores are preferred. When scores tie reasons act as a tiebreaker
// to keep deterministic ordering.
func (r MatchResult) BetterThan(other MatchResult) bool {
	if !r.Matched {
		return false
	}
	if !other.Matched {
		return true
	}
	if r.Score != other.Score {
		return r.Score > other.Score
	}
	if r.Reason == other.Reason || r.Reason == "" {
		return false
	}
	if other.Reason == "" {
		return true
	}
	return r.Reason < other.Reason
}

// Matcher evaluates whether the activation context should trigger a skill.
type Matcher interface {
	Match(ActivationContext) MatchResult
}

// MatcherFunc adapts a function to Matcher.
type MatcherFunc func(ActivationContext) MatchResult

// Match implements Matcher.
func (fn MatcherFunc) Match(ctx ActivationContext) MatchResult {
	if fn == nil {
		return MatchResult{}
	}
	return fn(ctx)
}

// KeywordMatcher inspects prompt text for keywords.
type KeywordMatcher struct {
	All []string
	Any []string
}

// Match implements Matcher.
func (m KeywordMatcher) Match(ctx ActivationContext) MatchResult {
	all := normalizeTokens(m.All)
	any := normalizeTokens(m.Any)
	if len(all) == 0 && len(any) == 0 {
		return MatchResult{}
	}
	prompt := strings.ToLower(ctx.Prompt)
	if strings.TrimSpace(prompt) == "" {
		return MatchResult{}
	}
	for _, token := range all {
		if !strings.Contains(prompt, token) {
			return MatchResult{}
		}
	}

	anyMatched := len(any) == 0
	matchedToken := ""
	for _, token := range any {
		if strings.Contains(prompt, token) {
			anyMatched = true
			matchedToken = token
			break
		}
	}
	if !anyMatched {
		return MatchResult{}
	}

	score := clampScore(0.55 + 0.2*boolToFloat(len(all) > 0) + 0.15*boolToFloat(matchedToken != ""))
	reasonParts := []string{"keywords"}
	if len(all) > 0 {
		reasonParts = append(reasonParts, "all="+strconv.Itoa(len(all)))
	}
	if matchedToken != "" {
		reasonParts = append(reasonParts, "hit="+matchedToken)
	} else if len(any) > 0 {
		reasonParts = append(reasonParts, "any="+strconv.Itoa(len(any)))
	}
	return MatchResult{Matched: true, Score: score, Reason: strings.Join(reasonParts, "|")}
}

// TagMatcher enforces metadata tag requirements/exclusions.
type TagMatcher struct {
	Require map[string]string
	Exclude map[string]string
}

// Match implements Matcher.
func (m TagMatcher) Match(ctx ActivationContext) MatchResult {
	req := normalizeTagMap(m.Require)
	excl := normalizeTagMap(m.Exclude)
	if len(req) == 0 && len(excl) == 0 {
		return MatchResult{}
	}
	tags := normalizeTagMap(ctx.Tags)
	for key, val := range excl {
		current, ok := tags[key]
		if !ok {
			continue
		}
		if val == "" || current == val {
			return MatchResult{}
		}
	}
	matchCount := 0
	for key, val := range req {
		current, ok := tags[key]
		if !ok {
			return MatchResult{}
		}
		if val != "" && val != current {
			return MatchResult{}
		}
		matchCount++
	}
	score := clampScore(0.55 + 0.35*ratio(matchCount, len(req)))
	reasonParts := []string{"tags"}
	if len(req) > 0 {
		reasonParts = append(reasonParts, "require="+strconv.Itoa(len(req)))
	}
	if len(excl) > 0 {
		reasonParts = append(reasonParts, "exclude="+strconv.Itoa(len(excl)))
	}
	return MatchResult{Matched: true, Score: score, Reason: strings.Join(reasonParts, "|")}
}

// TraitMatcher matches against declared user/system traits.
type TraitMatcher struct {
	Traits []string
}

// Match implements Matcher.
func (m TraitMatcher) Match(ctx ActivationContext) MatchResult {
	target := normalizeTokens(m.Traits)
	if len(target) == 0 {
		return MatchResult{}
	}
	have := tokenSet(ctx.Traits)
	if len(have) == 0 {
		return MatchResult{}
	}
	matches := 0
	for _, trait := range target {
		if _, ok := have[trait]; ok {
			matches++
		}
	}
	if matches == 0 {
		return MatchResult{}
	}
	score := clampScore(0.55 + 0.4*ratio(matches, len(target)))
	reason := "traits:" + strconv.Itoa(matches) + "/" + strconv.Itoa(len(target))
	return MatchResult{Matched: true, Score: score, Reason: reason}
}

func normalizeTokens(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := set[normalized]; ok {
			continue
		}
		set[normalized] = struct{}{}
		result = append(result, normalized)
	}
	slices.Sort(result)
	return result
}

func normalizeTagMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		normKey := strings.ToLower(strings.TrimSpace(key))
		if normKey == "" {
			continue
		}
		out[normKey] = strings.ToLower(strings.TrimSpace(value))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tokenSet(values []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, value := range values {
		norm := strings.ToLower(strings.TrimSpace(value))
		if norm == "" {
			continue
		}
		set[norm] = struct{}{}
	}
	return set
}

func clampScore(score float64) float64 {
	switch {
	case score < 0:
		return 0
	case score > 0.99:
		return 0.99
	default:
		return score
	}
}

func ratio(count, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) / float64(total)
}

func boolToFloat(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}

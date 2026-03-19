package middleware

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

const (
	traceAgentStartKey = "trace.agent.start"
	traceToolStartKey  = "trace.tool.start"
)

func ensureStateValues(st *State) {
	if st != nil && st.Values == nil {
		st.Values = map[string]any{}
	}
}

func (m *TraceMiddleware) trackDuration(stage Stage, st *State, now time.Time) int64 {
	if st == nil {
		return 0
	}
	switch stage {
	case StageBeforeAgent:
		st.Values[traceAgentStartKey] = now
	case StageBeforeTool:
		st.Values[traceToolStartKey] = now
	case StageAfterTool:
		return durationSince(st.Values, traceToolStartKey, now)
	case StageAfterAgent:
		return durationSince(st.Values, traceAgentStartKey, now)
	}
	return 0
}

func durationSince(values map[string]any, key string, now time.Time) int64 {
	if values == nil {
		return 0
	}
	start, ok := values[key].(time.Time)
	if !ok || start.IsZero() {
		return 0
	}
	delete(values, key)
	return now.Sub(start).Milliseconds()
}

func aggregateStats(events []TraceEvent) (int, int64) {
	var tokens int
	var duration int64
	for _, evt := range events {
		if evt.DurationMS > 0 {
			duration += evt.DurationMS
		}
		tokens += usageTotal(evt.ModelResponse)
	}
	return tokens, duration
}

func usageTotal(resp map[string]any) int {
	if len(resp) == 0 {
		return 0
	}
	raw, ok := resp["usage"]
	if !ok {
		return 0
	}
	switch val := raw.(type) {
	case model.Usage:
		return val.TotalTokens
	case *model.Usage:
		if val != nil {
			return val.TotalTokens
		}
	case map[string]any:
		return toInt(val["total_tokens"])
	default:
		return toInt(val)
	}
	return 0
}

func toInt(v any) int {
	switch num := v.(type) {
	case int:
		return num
	case int64:
		return int(num)
	case int32:
		return int(num)
	case float64:
		return int(num)
	case float32:
		return int(num)
	case json.Number:
		if i, err := num.Int64(); err == nil {
			return int(i)
		}
		return 0
	case string:
		if strings.TrimSpace(num) == "" {
			return 0
		}
		if i, err := strconv.Atoi(strings.TrimSpace(num)); err == nil {
			return i
		}
	}
	return 0
}

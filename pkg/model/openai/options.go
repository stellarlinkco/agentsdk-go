package openai

import (
	"encoding/json"
	"strconv"
	"strings"
)

type modelOptions struct {
	MaxTokens        int
	Temperature      *float64
	TopP             *float64
	PresencePenalty  *float64
	FrequencyPenalty *float64
	Stop             []string
	Tools            []ToolDefinition
	ToolChoice       json.RawMessage
	ResponseFormat   json.RawMessage
	Seed             *int
}

func parseModelOptions(extra map[string]any) modelOptions {
	opts := modelOptions{}
	if len(extra) == 0 {
		return opts
	}
	for key, val := range extra {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "max_tokens":
			if v, ok := toInt(val); ok && v > 0 {
				opts.MaxTokens = v
			}
		case "temperature":
			if v, ok := toFloat(val); ok {
				opts.Temperature = &v
			}
		case "top_p":
			if v, ok := toFloat(val); ok {
				opts.TopP = &v
			}
		case "presence_penalty":
			if v, ok := toFloat(val); ok {
				opts.PresencePenalty = &v
			}
		case "frequency_penalty":
			if v, ok := toFloat(val); ok {
				opts.FrequencyPenalty = &v
			}
		case "stop":
			opts.Stop = parseStop(val)
		case "tools":
			opts.Tools = parseTools(val)
		case "tool_choice":
			if data, err := json.Marshal(val); err == nil {
				opts.ToolChoice = data
			}
		case "response_format":
			if data, err := json.Marshal(val); err == nil {
				opts.ResponseFormat = data
			}
		case "seed":
			if v, ok := toInt(val); ok {
				opts.Seed = &v
			}
		}
	}
	return opts
}

func parseStop(val any) []string {
	switch v := val.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func parseTools(val any) []ToolDefinition {
	if val == nil {
		return nil
	}
	data, err := json.Marshal(val)
	if err != nil {
		return nil
	}
	var defs []ToolDefinition
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil
	}
	out := make([]ToolDefinition, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Function.Name)
		if name == "" {
			continue
		}
		if def.Type == "" {
			def.Type = "function"
		}
		if def.Function.Parameters != nil {
			def.Function.Parameters = cloneMap(def.Function.Parameters)
		}
		out = append(out, def)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneTools(in []ToolDefinition) []ToolDefinition {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolDefinition, len(in))
	for i, tool := range in {
		out[i] = tool
		if tool.Function.Parameters != nil {
			out[i].Function.Parameters = cloneMap(tool.Function.Parameters)
		}
	}
	return out
}

func toInt(val any) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		return i, err == nil
	default:
		return 0, false
	}
}

func toFloat(val any) (float64, bool) {
	switch v := val.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

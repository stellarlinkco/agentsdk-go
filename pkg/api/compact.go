package api

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/stellarlinkco/agentsdk-go/pkg/message"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

// CompactConfig controls automatic context compaction.
type CompactConfig struct {
	Enabled       bool    `json:"enabled"`
	Threshold     float64 `json:"threshold"`      // trigger ratio (default 0.8)
	PreserveCount int     `json:"preserve_count"` // keep latest N messages (default 5)
}

const (
	defaultCompactThreshold   = 0.8
	defaultCompactPreserve    = 5
	defaultClaudeContextLimit = 200000
)

func (c CompactConfig) withDefaults() CompactConfig {
	cfg := c
	if cfg.Threshold <= 0 || cfg.Threshold > 1 {
		cfg.Threshold = defaultCompactThreshold
	}
	if cfg.PreserveCount <= 0 {
		cfg.PreserveCount = defaultCompactPreserve
	}
	return cfg
}

type compactor struct {
	cfg   CompactConfig
	limit int
	mu    sync.Mutex
}

func newCompactor(cfg CompactConfig, tokenLimit int) *compactor {
	cfg = cfg.withDefaults()
	if !cfg.Enabled {
		return nil
	}
	limit := tokenLimit
	if limit <= 0 {
		limit = defaultClaudeContextLimit
	}
	return &compactor{cfg: cfg, limit: limit}
}

func (c *compactor) shouldCompact(msgCount, tokenCount int) bool {
	if c == nil || !c.cfg.Enabled {
		return false
	}
	if msgCount <= c.cfg.PreserveCount {
		return false
	}
	if tokenCount <= 0 || c.limit <= 0 {
		return false
	}
	ratio := float64(tokenCount) / float64(c.limit)
	return ratio >= c.cfg.Threshold
}

func (c *compactor) maybeCompact(ctx context.Context, hist *message.History, mdl model.Model) (bool, error) {
	if c == nil || hist == nil || !c.cfg.Enabled {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	msgCount := hist.Len()
	tokenCount := hist.TokenCount()
	if !c.shouldCompact(msgCount, tokenCount) {
		return false, nil
	}
	if mdl == nil {
		return false, errors.New("api: compaction enabled but model is nil")
	}

	snapshot := hist.All()
	preserve := c.cfg.PreserveCount
	if preserve >= len(snapshot) {
		return false, nil
	}

	cut := len(snapshot) - preserve
	for _, span := range toolTransactionSpans(snapshot) {
		if span.start < cut && cut < span.end {
			cut = span.start
			break
		}
	}
	if cut <= 0 {
		return false, nil
	}

	summary, err := compressMessages(ctx, mdl, stripToolIO(snapshot[:cut]))
	if err != nil {
		return false, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false, nil
	}

	out := make([]message.Message, 0, 1+len(snapshot[cut:]))
	out = append(out, message.Message{
		Role:    "system",
		Content: "## Summary\n\n" + summary,
	})
	out = append(out, snapshot[cut:]...)
	hist.Replace(out)
	return true, nil
}

type toolTransactionSpan struct {
	start int
	end   int // exclusive
}

func toolTransactionSpans(msgs []message.Message) []toolTransactionSpan {
	if len(msgs) == 0 {
		return nil
	}
	var spans []toolTransactionSpan
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		end := i + 1
		for end < len(msgs) && msgs[end].Role == "tool" {
			end++
		}
		spans = append(spans, toolTransactionSpan{start: i, end: end})
		i = end - 1
	}
	return spans
}

func stripToolIO(msgs []message.Message) []message.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]message.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Role == "tool" {
			continue
		}
		cloned := message.CloneMessage(msg)
		cloned.ToolCalls = nil
		cloned.ReasoningContent = ""
		if cloned.Role == "assistant" && len(msg.ToolCalls) > 0 && strings.TrimSpace(cloned.Content) == "" && len(cloned.ContentBlocks) == 0 {
			cloned.Content = "Tool call(s) omitted."
		}
		out = append(out, cloned)
	}
	return out
}

func compressMessages(ctx context.Context, mdl model.Model, msgs []message.Message) (string, error) {
	if mdl == nil {
		return "", errors.New("api: compressMessages: model is nil")
	}
	req := model.Request{
		System: "You are a prompt compression engine. Summarize the conversation for future context. Do not include any tool inputs, tool JSON, or tool outputs verbatim. Keep key facts, decisions, and constraints. Be concise.",
		Messages: append(convertMessages(msgs), model.Message{
			Role:    "user",
			Content: "Compress the above conversation into a short summary.",
		}),
		MaxTokens: 512,
	}
	resp, err := mdl.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("api: compaction model returned nil response")
	}
	return resp.Message.Content, nil
}

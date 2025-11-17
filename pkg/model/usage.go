package model

// TokenUsage 记录模型交互中的 token 统计信息。
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	CacheTokens  int `json:"cache_tokens"`
}

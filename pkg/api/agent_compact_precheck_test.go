package api

import (
	"context"
	"testing"
)

func TestRuntimePrepare_PrecheckCompactsHistory(t *testing.T) {
	auto := CompactConfig{Enabled: true, Threshold: 0.8, PreserveCount: 1}
	rt := newTestRuntime(t, staticModel{content: "ok"}, auto)

	sessionID := "sess"
	hist := rt.histories.Get(sessionID)
	for i := 0; i < 10; i++ {
		hist.Append(msgWithTokens("user", 20))
	}

	_, err := rt.Run(context.Background(), Request{Prompt: "hello", SessionID: sessionID})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	got := hist.All()
	if len(got) != 3 {
		t.Fatalf("expected compacted history len=3, got len=%d msgs=%+v", len(got), got)
	}
	if got[0].Role != "system" {
		t.Fatalf("expected summary message first, got %+v", got[0])
	}
	if got[1].Role != "user" || got[1].Content != "hello" {
		t.Fatalf("expected preserved prompt, got %+v", got[1])
	}
	if got[2].Role != "assistant" || got[2].Content != "ok" {
		t.Fatalf("expected model response appended after compaction, got %+v", got[2])
	}
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/event"
)

func main() {
	ctx := context.Background()
	storePath := filepath.Join(os.TempDir(), "agentsdk_events.jsonl")
	store, err := event.NewFileEventStore(storePath)
	if err != nil {
		log.Fatalf("create store: %v", err)
	}
	defer func() {
		_ = store.Close()
		_ = os.Remove(storePath)
	}()

	cfg := agent.Config{
		Name:           "bookmark-demo",
		DefaultContext: agent.RunContext{SessionID: fmt.Sprintf("bookmark-%d", time.Now().UnixMilli())},
		EventStore:     store,
	}

	ag, err := agent.New(cfg)
	if err != nil {
		log.Fatalf("new agent: %v", err)
	}

	firstRun, err := ag.Run(ctx, "第一次运行，描述 Bookmark 的价值")
	if err != nil {
		log.Fatalf("first run: %v", err)
	}
	fmt.Printf("首轮输出: %q\n", firstRun.Output)

	firstBookmark, err := store.LastBookmark()
	if err != nil {
		log.Fatalf("fetch bookmark: %v", err)
	}
	if firstBookmark == nil {
		log.Fatal("没有生成任何 Bookmark")
	}
	fmt.Printf("记录断点: seq=%d at %s\n", firstBookmark.Seq, firstBookmark.Timestamp.Format(time.RFC3339))

	secondRun, err := ag.Run(ctx, "第二次运行，继续阐述断点续播如何使用")
	if err != nil {
		log.Fatalf("second run: %v", err)
	}
	fmt.Printf("第二次输出: %q\n", secondRun.Output)

	replay, err := ag.Resume(ctx, firstBookmark)
	if err != nil {
		log.Fatalf("resume: %v", err)
	}
	fmt.Printf("Resume 捕获 %d 条事件，StopReason=%s\n", len(replay.Events), replay.StopReason)
}

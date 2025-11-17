package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/anthropic"
	"github.com/cexll/agentsdk-go/pkg/session"
)

const (
	defaultModel     = "claude-3-5-sonnet-20241022"
	memoryCheckpoint = "baseline-turn"
	fileCheckpoint   = "file-baseline"
)

func main() {
	ctx := context.Background()
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}

	// Reuse the Anthropic bootstrap logic from the basic example so that both
	// demos share consistent model initialization semantics.
	claudeModel, err := newAnthropicModel(ctx, apiKey)
	if err != nil {
		log.Fatalf("create anthropic model: %v", err)
	}
	fmt.Printf("Anthropic model ready: %T (%s)\n", claudeModel, defaultModel)

	if err := runSessionDemo(); err != nil {
		log.Fatalf("session demo failed: %v", err)
	}
}

// runSessionDemo wires together MemorySession + FileSession flows to showcase
// the core APIs: Append, List, Checkpoint, Resume, and Fork.
func runSessionDemo() error {
	memory, err := session.NewMemorySession("checkpoint-memory")
	if err != nil {
		return err
	}
	defer memory.Close()

	log.Println("== MemorySession scenario ==")

	baseTranscript := []session.Message{
		{Role: "user", Content: "你好 agentsdk, 让我们开始。"},
		{Role: "assistant", Content: "你好! 这是一段演示会话。"},
		{Role: "user", Content: "请记录此上下文并继续。"},
	}

	for _, msg := range baseTranscript {
		if err := appendMessage(memory, msg); err != nil {
			return err
		}
	}

	// Persist the current transcript so that we can roll back to it later.
	if err := memory.Checkpoint(memoryCheckpoint); err != nil {
		return err
	}
	log.Printf("checkpoint %q stored for session %s", memoryCheckpoint, memory.ID())

	// Continue the primary branch before branching so we see divergence.
	if err := appendMessage(memory, session.Message{Role: "assistant", Content: "主线继续推进。"}); err != nil {
		return err
	}

	// Fork a child branch and explore an alternative answer stream.
	child, err := memory.Fork("checkpoint-memory-child")
	if err != nil {
		return err
	}
	defer child.Close()

	if err := appendMessage(child, session.Message{Role: "assistant", Content: "子分支提供不同的答复。"}); err != nil {
		return err
	}

	// List the full transcripts for both branches.
	if err := printTranscript(memory, "memory main history", session.Filter{}); err != nil {
		return err
	}
	if err := printTranscript(child, "memory child history", session.Filter{}); err != nil {
		return err
	}

	// Demonstrate filtered queries by fetching only the user turns.
	if err := printTranscript(memory, "memory user-only view", session.Filter{Role: "user"}); err != nil {
		return err
	}

	// Restore the pre-branch checkpoint and show the rollback effect.
	if err := memory.Resume(memoryCheckpoint); err != nil {
		return err
	}
	log.Printf("session %s resumed from checkpoint %q", memory.ID(), memoryCheckpoint)
	if err := printTranscript(memory, "memory after resume", session.Filter{}); err != nil {
		return err
	}

	// FileSession uses the same APIs but persists state via a WAL on disk.
	root := filepath.Join(os.TempDir(), "agentsdk-checkpoint-demo")
	fileSession, err := session.NewFileSession("checkpoint-file", root)
	if err != nil {
		return err
	}
	defer fileSession.Close()
	log.Printf("FileSession WAL directory: %s", filepath.Join(root, "checkpoint-file"))

	// Copy the restored memory transcript into the durable file session.
	if err := replayTranscript(memory, fileSession); err != nil {
		return err
	}

	if err := fileSession.Checkpoint(fileCheckpoint); err != nil {
		return err
	}
	log.Printf("checkpoint %q stored for file-backed session", fileCheckpoint)

	if err := appendMessage(fileSession, session.Message{Role: "assistant", Content: "文件会话新增一条回复。"}); err != nil {
		return err
	}
	if err := printTranscript(fileSession, "file before resume", session.Filter{}); err != nil {
		return err
	}

	if err := fileSession.Resume(fileCheckpoint); err != nil {
		return err
	}
	log.Printf("file session resumed from checkpoint %q", fileCheckpoint)
	if err := printTranscript(fileSession, "file after resume", session.Filter{}); err != nil {
		return err
	}

	forkedFile, err := fileSession.Fork("checkpoint-file-child")
	if err != nil {
		return err
	}
	defer forkedFile.Close()

	if err := appendMessage(forkedFile, session.Message{Role: "assistant", Content: "文件子会话拓展自己的结论。"}); err != nil {
		return err
	}
	if err := printTranscript(forkedFile, "file child history", session.Filter{}); err != nil {
		return err
	}

	return nil
}

// appendMessage centralizes logging so every Append call shows up in stdout.
func appendMessage(sess session.Session, msg session.Message) error {
	log.Printf("[%s] append %s: %s", sess.ID(), msg.Role, msg.Content)
	return sess.Append(msg)
}

// printTranscript fetches and logs session messages honoring the provided filter.
func printTranscript(sess session.Session, title string, filter session.Filter) error {
	msgs, err := sess.List(filter)
	if err != nil {
		return err
	}
	log.Printf("---- %s (%d messages) ----", title, len(msgs))
	for idx, msg := range msgs {
		log.Printf("%02d | %s | %s | %s", idx+1, msg.Timestamp.Format(time.RFC3339), msg.Role, msg.Content)
	}
	return nil
}

// replayTranscript copies the entire source transcript into another session.
func replayTranscript(src session.Session, dst session.Session) error {
	msgs, err := src.List(session.Filter{})
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		if err := dst.Append(msg); err != nil {
			return err
		}
	}
	return nil
}

// newAnthropicModel now reuses the official SDK wrapper to ensure parity with other demos.
func newAnthropicModel(_ context.Context, apiKey string) (modelpkg.Model, error) {
	log.Printf("Anthropic model (SDK): %s", defaultModel)
	// 使用官方 SDK 封装
	return anthropic.NewSDKModel(apiKey, defaultModel, 1024), nil
}
